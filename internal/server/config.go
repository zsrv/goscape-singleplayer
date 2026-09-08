// Package server boots the full goscape server stack (world, login, friends,
// ondemand, sqlite) inside the singleplayer process, on loopback listeners.
package server

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/zsrv/goscape-singleplayer/internal/inproc"
	"github.com/zsrv/goscape/cmd/goscape/app"
)

// Endpoint names for the in-process fabric. They are routing keys only —
// nothing here ever reaches a socket.
const (
	EndpointWorld    = "world"
	EndpointOndemand = "ondemand"
	EndpointLogin    = "login"
	EndpointFriends  = "friends"
)

// Options configures the embedded server stack. All listeners bind 127.0.0.1.
type Options struct {
	DataDir      string // world database, player saves
	CacheDir     string // packed game cache (goscape `make pack` output)
	WorldPort    int    // game TCP port the client connects to
	OndemandPort int    // cache/OnDemand HTTP port the client fetches from
	LoginPort    int    // internal login gRPC port
	FriendsPort  int    // internal friends gRPC port

	// Fabric, when non-nil, runs every module on in-memory transports and the
	// four port fields are ignored. Nil binds the loopback ports, which is
	// what --expose-tcp selects.
	Fabric *inproc.Fabric
}

// NewConfig builds the goscape app config for a self-contained singleplayer
// stack: every module enabled, defaults from app.NewDefaultConfig, all state
// rooted under DataDir. Paths are made absolute because the server defaults
// are cwd-relative and the client side of the process must not care about cwd.
func NewConfig(opts Options) (*app.Config, error) {
	dataDir, err := filepath.Abs(opts.DataDir)
	if err != nil {
		return nil, fmt.Errorf("resolve data dir: %w", err)
	}
	cacheDir, err := filepath.Abs(opts.CacheDir)
	if err != nil {
		return nil, fmt.Errorf("resolve cache dir: %w", err)
	}

	cfg := app.NewDefaultConfig()
	cfg.Target = app.SingleBinary // "all"

	cfg.Database.SQLite.DSN = filepath.Join(dataDir, "goscape.db")

	cfg.Login.Enable = true
	cfg.Login.GRPCListenPort = opts.LoginPort
	cfg.Login.SavePath = filepath.Join(dataDir, "players")

	cfg.Friends.Enable = true
	cfg.Friends.GRPCListenPort = opts.FriendsPort

	cfg.World.Enable = true
	cfg.World.CachePath = cacheDir
	cfg.World.TCPListenPort = opts.WorldPort
	cfg.World.LoginServerAddress = fmt.Sprintf("127.0.0.1:%d", opts.LoginPort)
	cfg.World.FriendsServerAddress = fmt.Sprintf("127.0.0.1:%d", opts.FriendsPort)
	cfg.World.FriendsServerEnabled = true

	cfg.OnDemand.Enable = true
	cfg.OnDemand.CachePath = cacheDir
	cfg.OnDemand.Server.HTTPListenPort = opts.OndemandPort
	// /rs2.cgi advertises the game port; keep in sync with the world listener
	// or app.CheckConfig warns and the applet bootstrap would mislead.
	cfg.OnDemand.Port = opts.WorldPort
	cfg.OnDemand.PublicDir = filepath.Join(dataDir, "public")

	// In-process transports. The port fields above stay as they are — an
	// injected listener simply overrides what each module would have bound,
	// so nothing here has to unpick them.
	if opts.Fabric != nil {
		worldEP := opts.Fabric.Endpoint(EndpointWorld, inproc.RPCBufSize)
		ondemandEP := opts.Fabric.Endpoint(EndpointOndemand, inproc.OndemandBufSize)
		loginEP := opts.Fabric.Endpoint(EndpointLogin, inproc.RPCBufSize)
		friendsEP := opts.Fabric.Endpoint(EndpointFriends, inproc.RPCBufSize)

		cfg.World.Listener = worldEP.Listener()
		cfg.OnDemand.Server.Listener = ondemandEP.Listener()
		cfg.Login.Listener = loginEP.Listener()
		cfg.Friends.Listener = friendsEP.Listener()

		// gRPC resolves the target even when a dialer is supplied, so the
		// bridge addresses become passthrough targets rather than host:port.
		cfg.World.LoginServerAddress = "passthrough:///login"
		cfg.World.FriendsServerAddress = "passthrough:///friends"
		cfg.World.LoginServerDialer = loginEP.DialGRPC
		cfg.World.FriendsServerDialer = friendsEP.DialGRPC

		// The game client asks for its socket by port, so route that port to
		// the world endpoint. Using opts.WorldPort rather than a constant
		// keeps --world-port harmless instead of silently ignored.
		opts.Fabric.BindPort(cfg.World.TCPListenPort, worldEP)
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// CheckCache fails fast with an actionable message when the packed cache is
// missing — the expected first-run mistake. The repo ships no cache; goscape's
// `make pack` produces one. rev-225 packs a split layout (client/ + server/
// under the cache root); the chat word-filter is loaded from inside the
// cache (client/wordenc), not a separate raw file.
func CheckCache(cacheDir string) error {
	if _, err := os.Stat(filepath.Join(cacheDir, "client", "config")); err != nil {
		return fmt.Errorf("no packed rev-225 game cache at %s (client/config missing): "+
			"point --cache-dir at a rev-225 split-layout pack (data/pack with client/ + server/): %w",
			cacheDir, err)
	}
	return nil
}
