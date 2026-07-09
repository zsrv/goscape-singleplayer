// Command goscape-singleplayer runs a complete singleplayer game: the full
// goscape server stack (world, login, friends, ondemand, sqlite) in-process
// on loopback TCP, and the goscape-client game window in the same process.
//
// Exit paths (all converge on exitOnce so the server stops exactly once):
//   - window close → clientextras.ExitFunc → graceful server Stop → exit 0
//   - SIGINT/SIGTERM → app's signal handler stops services → Done watcher exits
//   - server module failure → Done watcher logs and exits 1
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/zsrv/goscape-client/pkg/jagex2/client/clientextras"
	"github.com/zsrv/goscape-client/pkg/jagex2/launch"
	"github.com/zsrv/goscape-singleplayer/internal/server"
)

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}

func main() {
	dataDir := flag.String("data-dir", "./data", "directory for the world database and player saves")
	cacheDir := flag.String("cache-dir", "./data/pack", "packed game cache directory (goscape `make pack` output)")
	wordencPath := flag.String("wordenc-path", "", "raw wordenc jagfile for the chat word-filter (default: <cache-dir>/../raw/wordenc)")
	worldPort := flag.Int("world-port", 43594, "loopback game (world) TCP port")
	ondemandPort := flag.Int("ondemand-port", 8080, "loopback cache/OnDemand HTTP port")
	loginPort := flag.Int("login-port", 2004, "loopback login gRPC port (internal)")
	friendsPort := flag.Int("friends-port", 2005, "loopback friends gRPC port (internal)")
	mem := flag.String("mem", "high", "client memory mode: high|low")
	worldType := flag.String("world-type", "members", "world type: free|members")
	flag.Parse()

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

	if err := server.CheckCache(*cacheDir); err != nil {
		fatalf("%v", err)
	}

	cfg, err := server.NewConfig(server.Options{
		DataDir:      *dataDir,
		CacheDir:     *cacheDir,
		WordEncPath:  *wordencPath,
		WorldPort:    *worldPort,
		OndemandPort: *ondemandPort,
		LoginPort:    *loginPort,
		FriendsPort:  *friendsPort,
	})
	if err != nil {
		fatalf("server config: %v", err)
	}

	if err := server.CheckWordEnc(cfg.World.WordEncPath); err != nil {
		fatalf("%v", err)
	}

	logger, err := server.NewLogger()
	if err != nil {
		fatalf("logger: %v", err)
	}

	srv, err := server.Start(logger, cfg)
	if err != nil {
		fatalf("server start: %v", err)
	}

	readyCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	err = srv.WaitReady(readyCtx, *ondemandPort)
	cancel()
	if err != nil {
		fatalf("server failed to become ready: %v", err)
	}

	var exitOnce sync.Once

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
			os.Exit(code)
		})
		// Another goroutine is already driving the exit; park until it does.
		select {}
	}

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

	launch.Run(launch.Options{
		NodeID:          10,
		StoreID:         32,
		LowMemory:       lowMemory,
		Members:         members,
		Host:            "127.0.0.1",
		Transport:       clientextras.TransportTCP,
		WorldPort:       *worldPort,
		WSPath:          "",
		OndemandBaseURL: fmt.Sprintf("http://127.0.0.1:%d", *ondemandPort),
	})
}
