package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/zsrv/goscape/cmd/goscape/app"
	"github.com/zsrv/goscape/pkg/util/log"
)

// Server is the goscape stack running inside this process.
type Server struct {
	app  *app.App
	done chan struct{}
	err  error // app.Run's result; valid only after done is closed
}

// NewLogger builds the server-side logger the same way cmd/goscape does
// (text handler on stdout, info level).
func NewLogger() (*slog.Logger, error) {
	return log.NewLogger(slog.LevelInfo, "text", os.Stdout)
}

// Start creates the state directories, constructs the app, and runs it on a
// background goroutine. app.Run installs the process SIGINT/SIGTERM handler —
// in singleplayer the server is the intended owner of shutdown signals; the
// binary watches Done to exit after a signal-driven stop.
func Start(logger *slog.Logger, cfg *app.Config) (*Server, error) {
	for _, dir := range []string{
		filepath.Dir(cfg.Database.SQLite.DSN),
		cfg.Login.SavePath,
		cfg.OnDemand.PublicDir,
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create state dir: %w", err)
		}
	}

	a, err := app.New(logger, *cfg)
	if err != nil {
		return nil, fmt.Errorf("construct server app: %w", err)
	}

	s := &Server{app: a, done: make(chan struct{})}
	go func() {
		s.err = a.Run()
		close(s.done)
	}()
	return s, nil
}

// Done is closed when app.Run returns — after Stop, a shutdown signal, or a
// module failure. Err reports why; call it only after Done is closed.
func (s *Server) Done() <-chan struct{} { return s.done }

func (s *Server) Err() error { return s.err }

// WaitReady polls the ondemand /crc endpoint until it serves 200, the server
// dies, or ctx expires. /crc is the first thing the game client fetches at
// boot, so "crc answers" is exactly the readiness the client needs.
func (s *Server) WaitReady(ctx context.Context, ondemandPort int) error {
	url := fmt.Sprintf("http://127.0.0.1:%d/crc", ondemandPort)
	httpClient := &http.Client{Timeout: 2 * time.Second}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		resp, err := httpClient.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("server not ready before deadline (last probe of %s: %v): %w", url, err, ctx.Err())
		case <-s.done:
			return fmt.Errorf("server exited during startup: %w", s.err)
		case <-ticker.C:
		}
	}
}

// Stop asks the app to shut down and waits up to timeout for Run to return —
// player saves and the sqlite database flush during service shutdown, so the
// caller must not os.Exit before this returns. Call only after Start
// succeeded and WaitReady returned nil (app.Stop panics on a non-running
// app). Safe if the app already stopped on its own: app.Stop's underlying
// signal-handler Stop is idempotent.
func (s *Server) Stop(timeout time.Duration) error {
	s.app.Stop()
	select {
	case <-s.done:
		return s.err
	case <-time.After(timeout):
		return fmt.Errorf("server did not stop within %s", timeout)
	}
}
