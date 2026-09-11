// Command goscape-singleplayer runs a complete singleplayer game: the full
// goscape server stack (world, login, friends, ondemand, sqlite) and the
// goscape-client game window in the same process. By default everything
// rides in-memory transports and the process opens no sockets; --expose-tcp
// restores the loopback ports for debugging (a second client, tcpdump, curl).
//
// Exit paths. stopOnce guarantees the server stops at most once and exitOnce
// that the process exits at most once; the two are separate because the panic
// path needs the stop without owning the exit:
//   - window close → clientextras.ExitFunc → graceful server Stop, then the
//     fabric closes (only after Stop returns) → exit 0
//   - SIGINT/SIGTERM → app's signal handler stops services → Done watcher exits
//   - server module failure → Done watcher logs and exits 1
//   - window creation panics (headless machine: glfw.Init, glfw.CreateWindow
//     or gl.Init) → runWindow stops the server so sqlite closes cleanly, then
//     lets the panic continue so the operator still sees why
//
// Windows output. The Windows build links as a GUI app (-H windowsgui), so
// double-clicking it opens the game window and nothing else — no console
// window alongside. Started from a terminal it still writes there, by
// attaching to the console it was launched from. Started from Explorer it has
// no standard streams at all, so the server log and any panic trace go to
// <data-dir>/logs/goscape-singleplayer.log instead, and -console asks for a
// console window as well. console_windows.go holds the detail.
package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/zsrv/goscape-client/pkg/jagex2/client/clientextras"
	"github.com/zsrv/goscape-client/pkg/jagex2/launch"
	"github.com/zsrv/goscape-singleplayer/internal/build"
	"github.com/zsrv/goscape-singleplayer/internal/content"
	"github.com/zsrv/goscape-singleplayer/internal/inproc"
	"github.com/zsrv/goscape-singleplayer/internal/server"
)

const (
	// defaultDataDir is the -data-dir default, named because the fallback log
	// has to be placed before the command line can be trusted — a usage error
	// is one of the things it exists to record — and this is the only guess
	// available at that point. Declared once so the two cannot drift.
	defaultDataDir = "./data"

	logFileName = "goscape-singleplayer.log"
)

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}

// options is the parsed command line. Separating it from main keeps flag
// parsing and validation reachable from a test — main itself cannot be
// tested, because it ends in launch.Run opening a real window.
type options struct {
	dataDir  string
	cacheDir string

	worldPort    int
	ondemandPort int
	loginPort    int
	friendsPort  int

	exposeTCP bool
	lowMemory bool
	members   bool

	// console asks the Windows build for a console window. Declared on every
	// platform so shortcuts, scripts and docs stay portable; inert everywhere
	// else, which main reports rather than ignores.
	console bool

	// explicitCacheDir records whether -cache-dir was actually passed, which
	// content.ResolveCacheDir needs to tell "user asked for this directory"
	// apart from "nobody said, so the embedded bundle may win".
	explicitCacheDir bool

	// explicit names every flag the user actually passed, so a flag that is
	// accepted but cannot take effect can be reported rather than ignored. A
	// default value cannot carry this; only flag.Visit can.
	explicit map[string]bool
}

// parseArgs parses and validates args. done is true when the command has
// already finished its work and the game must not start (--version). A
// non-nil error is a usage problem; the flag package has already reported
// syntax errors to out by then.
func parseArgs(args []string, out io.Writer) (opts *options, done bool, err error) {
	fs := flag.NewFlagSet("goscape-singleplayer", flag.ContinueOnError)
	fs.SetOutput(out)

	dataDir := fs.String("data-dir", defaultDataDir, "directory for the world database and player saves")
	cacheDir := fs.String("cache-dir", "./data/pack", "packed game cache directory (goscape `make pack` output; rev-225 split layout: client/ + server/ under this path)")
	worldPort := fs.Int("world-port", 43594, "loopback game (world) TCP port")
	ondemandPort := fs.Int("ondemand-port", 8080, "loopback cache/OnDemand HTTP port")
	loginPort := fs.Int("login-port", 2004, "loopback login gRPC port (internal)")
	friendsPort := fs.Int("friends-port", 2005, "loopback friends gRPC port (internal)")
	exposeTCP := fs.Bool("expose-tcp", false, "bind the loopback ports instead of running everything in-process (debugging: lets a second client, tcpdump or curl reach the server)")
	mem := fs.String("mem", "high", "client memory mode: high|low")
	worldType := fs.String("world-type", "members", "world type: free|members")
	showVersion := fs.Bool("version", false, "print build and content provenance, then exit")
	console := fs.Bool("console", false, "Windows only: also open a console window for log output when launched from Explorer (a console inherited from a terminal is always used, with or without this)")

	if err := fs.Parse(args); err != nil {
		return nil, false, err
	}

	if *showVersion {
		fmt.Fprintln(out, build.Info())
		fmt.Fprintln(out, content.Info())
		return nil, true, nil
	}

	o := &options{
		dataDir:      *dataDir,
		cacheDir:     *cacheDir,
		worldPort:    *worldPort,
		ondemandPort: *ondemandPort,
		loginPort:    *loginPort,
		friendsPort:  *friendsPort,
		exposeTCP:    *exposeTCP,
		console:      *console,
	}

	switch *mem {
	case "high":
		o.lowMemory = false
	case "low":
		o.lowMemory = true
	default:
		return nil, false, fmt.Errorf("invalid -mem %q (want high|low)", *mem)
	}

	switch *worldType {
	case "free":
		o.members = false
	case "members":
		o.members = true
	default:
		return nil, false, fmt.Errorf("invalid -world-type %q (want free|members)", *worldType)
	}

	o.explicit = make(map[string]bool)
	fs.Visit(func(f *flag.Flag) { o.explicit[f.Name] = true })
	o.explicitCacheDir = o.explicit["cache-dir"]

	return o, false, nil
}

// wantsConsole reports whether args ask for a console window.
//
// The console has to exist before the first write, which is earlier than the
// real parseArgs call can help with: the flag package reports usage errors to
// its output, and on a windowsgui binary launched from Explorer that output
// has nowhere to go yet. So the command line is parsed twice — here with the
// output discarded, purely to learn the intent, and then for real.
//
// Reusing parseArgs rather than scanning os.Args is what keeps the two passes
// from disagreeing about what the command line says: a hand-rolled scan has to
// know which flags take a value to tell `-console` the flag from `-console`
// the value of -cache-dir, and there is no reason to maintain that knowledge
// twice. It holds only while parseArgs stays free of side effects other than
// writing to out.
//
// -version short-circuits parseArgs before options exist, so `-console
// -version` reports false. That combination has no use — an AllocConsole
// window closes with the process — and every launch from a terminal has stdio
// without the flag anyway.
func wantsConsole(args []string) bool {
	opts, _, _ := parseArgs(args, io.Discard)
	return opts != nil && opts.console
}

// inertPortFlags names the port flags the user passed that cannot take effect
// in the selected mode, in flag-declaration order.
//
// In fabric mode each module serves an injected listener instead of binding,
// so Login.GRPCListenPort, Friends.GRPCListenPort and
// OnDemand.Server.HTTPListenPort are all overridden, and world's two bridge
// addresses are replaced with passthrough:/// targets. -world-port is NOT
// inert: the fabric routes the client's socket by it (BindPort) and
// cfg.OnDemand.Port derives portoff from it. Under --expose-tcp every port is
// bound for real, so nothing is inert.
func inertPortFlags(o *options) []string {
	if o.exposeTCP {
		return nil
	}
	var inert []string
	for _, name := range []string{"ondemand-port", "login-port", "friends-port"} {
		if o.explicit[name] {
			inert = append(inert, "-"+name)
		}
	}
	return inert
}

// consoleFlagInert reports whether -console was passed on a platform where it
// cannot do anything. Only the Windows build links as a GUI app, so only it
// has a hidden console to show.
//
// goos is a parameter rather than a read of runtime.GOOS so the answer for
// every platform is testable from any one of them.
func consoleFlagInert(o *options, goos string) bool {
	return o.console && goos != "windows"
}

// logFilePath is where output goes when there is no stream to write to.
//
// Under -data-dir rather than beside the executable: -data-dir is already the
// one directory this command requires to be writable — the sqlite database and
// the player saves live there — while the binary itself is often unpacked
// somewhere read-only, or under Program Files where a write would be
// virtualised somewhere the user will never find it.
func logFilePath(dataDir string) string {
	return filepath.Join(dataDir, "logs", logFileName)
}

// openLogFile creates the log directory and opens this run's log.
//
// It runs before server.Start has created any state directory, and on a first
// launch -data-dir does not exist yet, so the directory is created here.
//
// O_TRUNC, not O_APPEND: the question this file answers is always "why did the
// run I just did fail", so the previous run's bytes are noise, and a file
// nobody ever looks at must not grow without bound.
func openLogFile(dataDir string) (*os.File, error) {
	path := logFilePath(dataDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create log directory: %w", err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open log file: %w", err)
	}
	return f, nil
}

// ondemandBaseURL is the base URL both the readiness probe and the client's
// ported fetch sites use for the ondemand HTTP service.
//
// Under --expose-tcp the port is real, so this is the loopback address the
// module actually binds. In fabric mode nothing binds at all and the injected
// dialer ignores the address, which makes the host arbitrary — so it is
// deliberately one that cannot resolve. A loopback host would mean that a
// dialer which somehow failed to be wired up would reach whatever really owns
// that port instead, and the readiness probe would report success against a
// stranger. RFC 2606 reserves .invalid as guaranteed never to resolve, so a
// mis-wiring fails loudly.
func ondemandBaseURL(inProcess bool, port int) string {
	if inProcess {
		return "http://ondemand.invalid"
	}
	return fmt.Sprintf("http://127.0.0.1:%d", port)
}

// runWindow invokes launch, and if it panics, stops the server before letting
// the panic continue.
//
// platform.newGLFWBackend panics rather than returning an error on glfw.Init,
// glfw.CreateWindow or gl.Init failure — the usual outcome on a headless or
// forwarded-display machine. That panic unwinds through launch.Run into main,
// and every graceful exit path in this command lives inside
// clientextras.ExitFunc or the Done watcher, both of which it bypasses. The
// server would then die with the process, leaving sqlite without a clean
// close.
//
// stop is not called when launch returns normally: that path belongs to
// clientextras.ExitFunc, which the client invokes itself.
func runWindow(launch func(), stop func()) {
	defer func() {
		if r := recover(); r != nil {
			stop()
			panic(r)
		}
	}()
	launch()
}

func main() {
	// First, before anything writes anything: the Windows build is a GUI
	// binary, which Windows starts with no standard handles unless something
	// hands them over, and the flag package's usage errors are already a
	// write. stdioUsable comes back false for exactly one launch shape — that
	// binary started from Explorer without -console — and is always true
	// elsewhere.
	stdioUsable := setupConsole(wantsConsole(os.Args[1:]))

	// Buffered rather than written straight out: -data-dir decides where
	// output goes when stdio is unusable, and only parseArgs knows -data-dir.
	// Without the buffer, usage text would be written before there was
	// anywhere to put it.
	var early bytes.Buffer
	opts, done, err := parseArgs(os.Args[1:], &early)

	// -version has nothing to log and nothing to start, so it must not create
	// a log directory as a side effect of printing two lines.
	if done {
		_, _ = io.Copy(os.Stdout, &early)
		return
	}

	if !stdioUsable {
		// opts is nil when the command line did not parse, and the parse that
		// would have said where -data-dir points is the one that failed — so
		// use its default rather than drop the error on the floor. A Windows
		// shortcut with a typo'd flag is otherwise a silent exit 1.
		dataDir := defaultDataDir
		if opts != nil {
			dataDir = opts.dataDir
		}
		if f, logErr := openLogFile(dataDir); logErr == nil {
			redirectStdio(f)
		}
		// Nothing to report if that failed: the report would need the stream
		// it just failed to provide.
	}
	_, _ = io.Copy(os.Stdout, &early)

	if err != nil {
		fatalf("%v", err)
	}

	// Accepted-but-ignored flags are worth a word: without this, -login-port
	// 3000 silently changes nothing and the next hour goes on finding out why.
	if inert := inertPortFlags(opts); len(inert) > 0 {
		fmt.Fprintf(os.Stderr,
			"warning: %s ignored: those modules run in-process, so nothing binds a port (pass --expose-tcp to bind them)\n",
			strings.Join(inert, ", "))
	}
	if consoleFlagInert(opts, runtime.GOOS) {
		fmt.Fprintf(os.Stderr,
			"warning: -console ignored on %s: only the Windows build hides a console to begin with\n",
			runtime.GOOS)
	}

	bundle, haveBundle := content.Bundle()
	resolvedCacheDir, resolveErr := content.ResolveCacheDir(
		opts.explicitCacheDir, opts.cacheDir, opts.dataDir, bundle, haveBundle, content.PackDigest)
	if resolveErr != nil {
		fatalf("%v", resolveErr)
	}

	if err := server.CheckCache(resolvedCacheDir); err != nil {
		fatalf("%v", err)
	}

	var fabric *inproc.Fabric
	if !opts.exposeTCP {
		fabric = inproc.New()
	}

	cfg, err := server.NewConfig(server.Options{
		DataDir:      opts.dataDir,
		CacheDir:     resolvedCacheDir,
		WorldPort:    opts.worldPort,
		OndemandPort: opts.ondemandPort,
		LoginPort:    opts.loginPort,
		FriendsPort:  opts.friendsPort,
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

	// Two distinct clients, not one shared between readiness and asset
	// fetching: the probe needs a bounded per-attempt timeout so WaitReady's
	// deadline is actually enforceable (bufconn's DialContext blocks until
	// Accept, and Get otherwise carries context.Background() with no
	// deadline of its own); the asset fetcher must not cap large archive
	// downloads with that same short timeout.
	baseURL := ondemandBaseURL(fabric != nil, opts.ondemandPort)
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
	err = srv.WaitReady(readyCtx, probeClient, baseURL)
	cancel()
	if err != nil {
		fatalf("server failed to become ready: %v", err)
	}

	// Stops the server, then closes the fabric — in that order, at most once.
	// Player saves and the sqlite flush happen during service shutdown and
	// still need their transports, so the fabric can only close after Stop
	// returns. Separate from exitOnce because two different callers need the
	// stop without both owning the process exit: ExitFunc (which exits
	// afterwards) and runWindow (which re-panics afterwards).
	var (
		stopOnce   sync.Once
		stopFailed bool
	)
	stopServer := func() {
		stopOnce.Do(func() {
			if err := srv.Stop(10 * time.Second); err != nil {
				logger.Error("server shutdown", "err", err)
				stopFailed = true
			}
			if fabric != nil {
				_ = fabric.Close()
			}
		})
	}

	// Window close / client shutdown: stop the server first, then leave.
	clientextras.ExitFunc = func(code int) {
		exitOnce.Do(func() {
			stopServer()
			if stopFailed && code == 0 {
				code = 1
			}
			os.Exit(code)
		})
		// Another goroutine is already driving the exit; park until it does.
		select {}
	}

	runWindow(func() {
		launch.Run(launch.Options{
			NodeID:          10, // must match the server's world.node-id / ondemand.node-id defaults (both 10)
			LowMemory:       opts.lowMemory,
			Members:         opts.members,
			Host:            "127.0.0.1",
			Transport:       transport,
			WorldPort:       opts.worldPort,
			WSPath:          "",
			OndemandBaseURL: baseURL,
			DialInProc:      dialInProc,
			HTTPClient:      assetClient,
		})
	}, stopServer)
}
