package server

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"
)

// Boots the real module stack against a packed cache and verifies
// ready→stop. Needs a cache: GOSCAPE_SP_TEST_CACHE overrides; default is the
// sibling goscape-rev245.2 checkout's pack output. Skips when absent so
// `go test ./...` stays green on a fresh machine.
func TestServerBootsReadyAndStops(t *testing.T) {
	cacheDir := os.Getenv("GOSCAPE_SP_TEST_CACHE")
	if cacheDir == "" {
		cacheDir = "../../../goscape-rev245.2/data/pack"
	}
	if err := CheckCache(cacheDir); err != nil {
		t.Skipf("packed cache unavailable: %v", err)
	}

	cfg, err := NewConfig(Options{
		DataDir:      t.TempDir(),
		CacheDir:     cacheDir,
		WorldPort:    43794, // offset from defaults so a dev server can coexist
		OndemandPort: 48080,
		LoginPort:    42104,
		FriendsPort:  42105,
	})
	if err != nil {
		t.Fatalf("NewConfig: %v", err)
	}

	srv, err := Start(slog.New(slog.DiscardHandler), cfg)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := srv.WaitReady(ctx, 48080); err != nil {
		t.Fatalf("WaitReady: %v", err)
	}

	if err := srv.Stop(30 * time.Second); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	select {
	case <-srv.Done():
	default:
		t.Fatal("Done not closed after successful Stop")
	}
}
