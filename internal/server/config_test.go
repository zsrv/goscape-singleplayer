package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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

// rev-225 packs a split layout (client/ + server/ under the cache root); the
// chat word-filter is loaded from inside the cache, so CheckCache probes for
// the client/config jagfile instead of a monolithic main_file_cache.dat.
func TestCheckCache(t *testing.T) {
	dir := t.TempDir()
	if err := CheckCache(dir); err == nil {
		t.Fatal("CheckCache on empty dir: want error, got nil")
	}
	if err := os.MkdirAll(filepath.Join(dir, "client"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "client", "config"), []byte{0}, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := CheckCache(dir); err != nil {
		t.Fatalf("CheckCache with client/config present: %v", err)
	}
}
