package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zsrv/goscape-singleplayer/internal/inproc"
)

func TestNewConfigWiresLoopbackStack(t *testing.T) {
	cfg, err := NewConfig(Options{
		DataDir:      "sp-data",
		CacheDir:     "sp-pack",
		WorldPort:    40594,
		OndemandPort: 40080,
		LoginPort:    42004,
		FriendsPort:  42005,
	})
	if err != nil {
		t.Fatalf("NewConfig: %v", err)
	}

	if cfg.Target != "all" {
		t.Errorf("Target = %q, want \"all\"", cfg.Target)
	}
	if !cfg.World.Enable || !cfg.Login.Enable || !cfg.Friends.Enable || !cfg.OnDemand.Enable {
		t.Errorf("all modules must be enabled: world=%v login=%v friends=%v ondemand=%v",
			cfg.World.Enable, cfg.Login.Enable, cfg.Friends.Enable, cfg.OnDemand.Enable)
	}

	// Ports propagate, and the world's gRPC client addresses point at the
	// loopback ports the login/friends servers listen on.
	if cfg.World.TCPListenPort != 40594 {
		t.Errorf("World.TCPListenPort = %d", cfg.World.TCPListenPort)
	}
	if cfg.OnDemand.Server.HTTPListenPort != 40080 {
		t.Errorf("OnDemand HTTPListenPort = %d", cfg.OnDemand.Server.HTTPListenPort)
	}
	if cfg.Login.GRPCListenPort != 42004 {
		t.Errorf("Login.GRPCListenPort = %d", cfg.Login.GRPCListenPort)
	}
	if cfg.Friends.GRPCListenPort != 42005 {
		t.Errorf("Friends.GRPCListenPort = %d", cfg.Friends.GRPCListenPort)
	}
	if cfg.World.LoginServerAddress != "127.0.0.1:42004" {
		t.Errorf("World.LoginServerAddress = %q", cfg.World.LoginServerAddress)
	}
	if cfg.World.FriendsServerAddress != "127.0.0.1:42005" {
		t.Errorf("World.FriendsServerAddress = %q", cfg.World.FriendsServerAddress)
	}
	if !cfg.World.FriendsServerEnabled {
		t.Error("World.FriendsServerEnabled = false, want true")
	}

	// /rs2.cgi must advertise the real game port (app.CheckConfig invariant).
	if cfg.OnDemand.Port != cfg.World.TCPListenPort {
		t.Errorf("OnDemand.Port = %d, want %d", cfg.OnDemand.Port, cfg.World.TCPListenPort)
	}

	// State roots under DataDir/CacheDir, absolute (server defaults are
	// cwd-relative; the embedding binary must not depend on cwd).
	for name, p := range map[string]string{
		"sqlite DSN":         cfg.Database.SQLite.DSN,
		"login save path":    cfg.Login.SavePath,
		"world cache path":   cfg.World.CachePath,
		"ondemand cache":     cfg.OnDemand.CachePath,
		"ondemand publicdir": cfg.OnDemand.PublicDir,
	} {
		if !filepath.IsAbs(p) {
			t.Errorf("%s = %q, want absolute", name, p)
		}
	}
	if !strings.HasSuffix(cfg.Database.SQLite.DSN, filepath.Join("sp-data", "goscape.db")) {
		t.Errorf("sqlite DSN = %q", cfg.Database.SQLite.DSN)
	}
	if !strings.HasSuffix(cfg.Login.SavePath, filepath.Join("sp-data", "players")) {
		t.Errorf("Login.SavePath = %q", cfg.Login.SavePath)
	}
	if !strings.HasSuffix(cfg.World.CachePath, "sp-pack") || cfg.OnDemand.CachePath != cfg.World.CachePath {
		t.Errorf("cache paths = %q / %q", cfg.World.CachePath, cfg.OnDemand.CachePath)
	}
}

// Amendment 1: the chat word-filter jagfile path must reach world config.
// Empty Options.WordEncPath derives <cache-dir>/../raw/wordenc (goscape repo
// layout: the raw file sits next to the pack).
func TestNewConfigDerivesWordEncPathNextToCache(t *testing.T) {
	cfg, err := NewConfig(Options{
		DataDir:      "sp-data",
		CacheDir:     "sp-pack",
		WorldPort:    40594,
		OndemandPort: 40080,
		LoginPort:    42004,
		FriendsPort:  42005,
	})
	if err != nil {
		t.Fatalf("NewConfig: %v", err)
	}
	if !filepath.IsAbs(cfg.World.WordEncPath) {
		t.Errorf("World.WordEncPath = %q, want absolute", cfg.World.WordEncPath)
	}
	if !strings.HasSuffix(cfg.World.WordEncPath, filepath.Join("raw", "wordenc")) {
		t.Errorf("World.WordEncPath = %q, want …/raw/wordenc", cfg.World.WordEncPath)
	}
	// Derived path sits next to the cache dir: <cache-dir>/../raw/wordenc.
	want := filepath.Join(filepath.Dir(cfg.World.CachePath), "raw", "wordenc")
	if cfg.World.WordEncPath != want {
		t.Errorf("World.WordEncPath = %q, want %q (next to cache dir)", cfg.World.WordEncPath, want)
	}
}

func TestNewConfigWordEncPathOverride(t *testing.T) {
	cfg, err := NewConfig(Options{
		DataDir:      "sp-data",
		CacheDir:     "sp-pack",
		WordEncPath:  "custom/wordenc",
		WorldPort:    40594,
		OndemandPort: 40080,
		LoginPort:    42004,
		FriendsPort:  42005,
	})
	if err != nil {
		t.Fatalf("NewConfig: %v", err)
	}
	if !filepath.IsAbs(cfg.World.WordEncPath) {
		t.Errorf("World.WordEncPath = %q, want absolute", cfg.World.WordEncPath)
	}
	if !strings.HasSuffix(cfg.World.WordEncPath, filepath.Join("custom", "wordenc")) {
		t.Errorf("World.WordEncPath = %q, want the override to propagate", cfg.World.WordEncPath)
	}
}

func TestCheckWordEnc(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "wordenc")
	if err := CheckWordEnc(path); err == nil {
		t.Fatal("CheckWordEnc on missing file: want error, got nil")
	}
	if err := os.WriteFile(path, []byte{0}, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := CheckWordEnc(path); err != nil {
		t.Fatalf("CheckWordEnc with file present: %v", err)
	}
}

func TestCheckCache(t *testing.T) {
	dir := t.TempDir()
	if err := CheckCache(dir); err == nil {
		t.Fatal("CheckCache on empty dir: want error, got nil")
	}
	if err := os.WriteFile(filepath.Join(dir, "main_file_cache.dat"), []byte{0}, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := CheckCache(dir); err != nil {
		t.Fatalf("CheckCache with dat file: %v", err)
	}
}

func TestNewConfigWiresFabricListeners(t *testing.T) {
	f := inproc.New()
	t.Cleanup(func() { _ = f.Close() })

	cfg, err := NewConfig(Options{
		DataDir:      t.TempDir(),
		CacheDir:     t.TempDir(),
		WorldPort:    43594,
		OndemandPort: 8080,
		LoginPort:    2004,
		FriendsPort:  2005,
		Fabric:       f,
	})
	if err != nil {
		t.Fatalf("NewConfig: %v", err)
	}

	// The port fields stay assigned; the listener is what makes them moot.
	// OnDemand.Port must still track World.TCPListenPort or app.CheckConfig
	// warns and /rs2.cgi emits a wrong portoff.
	if cfg.OnDemand.Port != cfg.World.TCPListenPort {
		t.Errorf("OnDemand.Port = %d, want it to track World.TCPListenPort = %d",
			cfg.OnDemand.Port, cfg.World.TCPListenPort)
	}

	if cfg.World.Listener == nil {
		t.Error("World.Listener not wired")
	}
	if cfg.OnDemand.Server.Listener == nil {
		t.Error("OnDemand.Server.Listener not wired")
	}
	if cfg.Login.Listener == nil {
		t.Error("Login.Listener not wired")
	}
	if cfg.Friends.Listener == nil {
		t.Error("Friends.Listener not wired")
	}
	if cfg.World.LoginServerDialer == nil {
		t.Error("World.LoginServerDialer not wired")
	}
	if cfg.World.FriendsServerDialer == nil {
		t.Error("World.FriendsServerDialer not wired")
	}
	if cfg.World.LoginServerAddress != "passthrough:///login" {
		t.Errorf("LoginServerAddress = %q, want passthrough:///login", cfg.World.LoginServerAddress)
	}
	if cfg.World.FriendsServerAddress != "passthrough:///friends" {
		t.Errorf("FriendsServerAddress = %q, want passthrough:///friends", cfg.World.FriendsServerAddress)
	}
}

func TestNewConfigWithoutFabricKeepsPorts(t *testing.T) {
	cfg, err := NewConfig(Options{
		DataDir:      t.TempDir(),
		CacheDir:     t.TempDir(),
		WorldPort:    43594,
		OndemandPort: 8080,
		LoginPort:    2004,
		FriendsPort:  2005,
	})
	if err != nil {
		t.Fatalf("NewConfig: %v", err)
	}
	if cfg.World.Listener != nil {
		t.Error("World.Listener set without a fabric")
	}
	if cfg.World.TCPListenPort != 43594 {
		t.Errorf("TCPListenPort = %d, want 43594", cfg.World.TCPListenPort)
	}
	if cfg.World.LoginServerAddress != "127.0.0.1:2004" {
		t.Errorf("LoginServerAddress = %q, want 127.0.0.1:2004", cfg.World.LoginServerAddress)
	}
}
