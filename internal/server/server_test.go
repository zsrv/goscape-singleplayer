package server

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/zsrv/goscape-singleplayer/internal/inproc"
)

// Boots the real module stack against a packed cache and verifies
// ready→stop. Needs a cache: GOSCAPE_SP_TEST_CACHE overrides; default is the
// sibling goscape checkout's pack output. Skips when absent so `go test ./...`
// stays green on a fresh machine.
func TestServerBootsReadyAndStops(t *testing.T) {
	cacheDir := os.Getenv("GOSCAPE_SP_TEST_CACHE")
	if cacheDir == "" {
		cacheDir = "../../../goscape/data/pack"
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
	httpClient := &http.Client{Timeout: 2 * time.Second}
	if err := srv.WaitReady(ctx, httpClient, "http://127.0.0.1:48080"); err != nil {
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

// Boots the real module stack on in-memory transports and proves the four
// loopback listeners are gone. Needs a cache, like the TCP boot test.
func TestServerBootsInProcess(t *testing.T) {
	cacheDir := os.Getenv("GOSCAPE_SP_TEST_CACHE")
	if cacheDir == "" {
		cacheDir = "../../../goscape/data/pack"
	}
	if err := CheckCache(cacheDir); err != nil {
		t.Skipf("packed cache unavailable: %v", err)
	}

	f := inproc.New()
	t.Cleanup(func() { _ = f.Close() })

	cfg, err := NewConfig(Options{
		DataDir:      t.TempDir(),
		CacheDir:     cacheDir,
		WorldPort:    43594,
		OndemandPort: 8080,
		LoginPort:    2004,
		FriendsPort:  2005,
		Fabric:       f,
	})
	if err != nil {
		t.Fatalf("NewConfig: %v", err)
	}

	srv, err := Start(slog.New(slog.DiscardHandler), cfg)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	ondemandEP := f.Endpoint(EndpointOndemand, inproc.OndemandBufSize)
	httpClient := &http.Client{Transport: &http.Transport{DialContext: ondemandEP.DialContext}}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := srv.WaitReady(ctx, httpClient, "http://ondemand"); err != nil {
		t.Fatalf("WaitReady over in-process HTTP: %v", err)
	}

	// The whole point: no socket anywhere.
	for _, port := range []int{43594, 8080, 2004, 2005} {
		addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
		conn, err := net.DialTimeout("tcp", addr, 250*time.Millisecond)
		if err == nil {
			conn.Close()
			t.Errorf("something is listening on %s; the in-process build must bind nothing", addr)
		}
	}

	if err := srv.Stop(30 * time.Second); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}
