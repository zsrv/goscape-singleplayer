// Command goscape-singleplayer runs a complete singleplayer game: the full
// goscape server stack (world, login, friends, ondemand, sqlite) and the
// goscape-client game window in the same process. By default everything
// rides in-memory transports and the process opens no sockets; --expose-tcp
// restores the loopback ports for debugging (a second client, tcpdump, curl).
//
// Exit paths (all converge on exitOnce so the server stops exactly once):
//   - window close → clientextras.ExitFunc → graceful server Stop, then the
//     fabric closes (only after Stop returns) → exit 0
//   - SIGINT/SIGTERM → app's signal handler stops services → Done watcher exits
//   - server module failure → Done watcher logs and exits 1
package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/zsrv/goscape-client/pkg/jagex2/client/clientextras"
	"github.com/zsrv/goscape-client/pkg/jagex2/launch"
	"github.com/zsrv/goscape-singleplayer/internal/build"
	"github.com/zsrv/goscape-singleplayer/internal/content"
	"github.com/zsrv/goscape-singleplayer/internal/inproc"
	"github.com/zsrv/goscape-singleplayer/internal/server"
)

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}

func main() {
	dataDir := flag.String("data-dir", "./data", "directory for the world database and player saves")
	cacheDir := flag.String("cache-dir", "./data/pack", "packed game cache directory (goscape `make pack` output; rev-225 split layout: client/ + server/ under this path)")
	worldPort := flag.Int("world-port", 43594, "loopback game (world) TCP port")
	ondemandPort := flag.Int("ondemand-port", 8080, "loopback cache/OnDemand HTTP port")
	loginPort := flag.Int("login-port", 2004, "loopback login gRPC port (internal)")
	friendsPort := flag.Int("friends-port", 2005, "loopback friends gRPC port (internal)")
	exposeTCP := flag.Bool("expose-tcp", false, "bind the loopback ports instead of running everything in-process (debugging: lets a second client, tcpdump or curl reach the server)")
	mem := flag.String("mem", "high", "client memory mode: high|low")
	worldType := flag.String("world-type", "members", "world type: free|members")
	showVersion := flag.Bool("version", false, "print build and content provenance, then exit")
	flag.Parse()

	if *showVersion {
		fmt.Println(build.Info())
		fmt.Println(content.Info())
		return
	}

	var lowMemory bool
	switch *mem {
	case "high":
		lowMemory = false
	case "low":
		lowMemory = true
	default:
		fatalf("invalid -mem %q (want high|low)", *mem)
	}

	var members bool
	switch *worldType {
	case "free":
		members = false
	case "members":
		members = true
	default:
		fatalf("invalid -world-type %q (want free|members)", *worldType)
	}

	explicitCacheDir := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "cache-dir" {
			explicitCacheDir = true
		}
	})

	bundle, haveBundle := content.Bundle()
	resolvedCacheDir, resolveErr := content.ResolveCacheDir(
		explicitCacheDir, *cacheDir, *dataDir, bundle, haveBundle, content.PackDigest)
	if resolveErr != nil {
		fatalf("%v", resolveErr)
	}

	if err := server.CheckCache(resolvedCacheDir); err != nil {
		fatalf("%v", err)
	}

	var fabric *inproc.Fabric
	if !*exposeTCP {
		fabric = inproc.New()
	}

	cfg, err := server.NewConfig(server.Options{
		DataDir:      *dataDir,
		CacheDir:     resolvedCacheDir,
		WorldPort:    *worldPort,
		OndemandPort: *ondemandPort,
		LoginPort:    *loginPort,
		FriendsPort:  *friendsPort,
		Fabric:       fabric,
	})
	if err != nil {
		fatalf("server config: %v", err)
	}

	// Music is optional: the client logs one line and plays silence when the
	// SoundFont is missing, so a failure to place it must not stop a server
	// that is otherwise fine. Warn and carry on.
	if err := content.EnsureSoundFont(bundle, haveBundle, cfg.OnDemand.PublicDir); err != nil {
		fmt.Fprintf(os.Stderr, "warning: %v (music will be silent)\n", err)
	}

	logger, err := server.NewLogger()
	if err != nil {
		fatalf("logger: %v", err)
	}

	srv, err := server.Start(logger, cfg)
	if err != nil {
		fatalf("server start: %v", err)
	}

	var exitOnce sync.Once

	// Spawned before the readiness wait so a signal-driven or failed boot can
	// never slip between WaitReady and this watcher and let launch.Run open a
	// window against a dead server.
	//
	// Signal-driven or failure-driven server exit: the client has no graceful
	// stop (the stock client's own exit path is os.Exit too), so leave once
	// the server is down.
	go func() {
		<-srv.Done()
		exitOnce.Do(func() {
			if err := srv.Err(); err != nil {
				logger.Error("server exited", "err", err)
				os.Exit(1)
			}
			os.Exit(0)
		})
	}()

	// The ondemand base URL is a real URL in both modes; in fabric mode the
	// dialer ignores its host:port, which keeps the client's ported fetch
	// sites unchanged.
	//
	// Two distinct clients, not one shared between readiness and asset
	// fetching: the probe needs a bounded per-attempt timeout so WaitReady's
	// deadline is actually enforceable (bufconn's DialContext blocks until
	// Accept, and Get otherwise carries context.Background() with no
	// deadline of its own); the asset fetcher must not cap large archive
	// downloads with that same short timeout.
	ondemandBaseURL := fmt.Sprintf("http://127.0.0.1:%d", *ondemandPort)
	probeClient := &http.Client{Timeout: 2 * time.Second}
	var assetClient *http.Client // nil keeps clientextras.HTTPClient at http.DefaultClient
	transport := clientextras.TransportTCP
	var dialInProc func(port int) (net.Conn, error)

	if fabric != nil {
		ondemandEP := fabric.Endpoint(server.EndpointOndemand, inproc.OndemandBufSize)
		probeClient = &http.Client{Transport: &http.Transport{DialContext: ondemandEP.DialContext}, Timeout: 2 * time.Second}
		assetClient = &http.Client{Transport: &http.Transport{DialContext: ondemandEP.DialContext}}
		transport = clientextras.TransportInProc
		dialInProc = fabric.DialPort
	}

	readyCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	err = srv.WaitReady(readyCtx, probeClient, ondemandBaseURL)
	cancel()
	if err != nil {
		fatalf("server failed to become ready: %v", err)
	}

	// Window close / client shutdown: stop the server first (player saves +
	// sqlite flush happen during service shutdown), then leave the process.
	clientextras.ExitFunc = func(code int) {
		exitOnce.Do(func() {
			if err := srv.Stop(10 * time.Second); err != nil {
				logger.Error("server shutdown", "err", err)
				if code == 0 {
					code = 1
				}
			}
			// Only after Stop returns: player saves and the sqlite flush
			// happen during service shutdown and still need their transports.
			if fabric != nil {
				_ = fabric.Close()
			}
			os.Exit(code)
		})
		// Another goroutine is already driving the exit; park until it does.
		select {}
	}

	launch.Run(launch.Options{
		NodeID:          10, // must match the server's world.node-id / ondemand.node-id defaults (both 10)
		LowMemory:       lowMemory,
		Members:         members,
		Host:            "127.0.0.1",
		Transport:       transport,
		WorldPort:       *worldPort,
		WSPath:          "",
		OndemandBaseURL: ondemandBaseURL,
		DialInProc:      dialInProc,
		HTTPClient:      assetClient,
	})
}
