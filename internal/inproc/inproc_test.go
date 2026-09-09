package inproc

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"testing"
	"time"
)

func TestEndpointRoundTrip(t *testing.T) {
	f := New()
	defer f.Close()
	ep := f.Endpoint("world", RPCBufSize)

	go func() {
		conn, err := ep.Listener().Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_, _ = conn.Write([]byte("hello"))
	}()

	conn, err := ep.Dial()
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close()

	buf := make([]byte, 5)
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("ReadFull: %v", err)
	}
	if string(buf) != "hello" {
		t.Fatalf("got %q, want %q", buf, "hello")
	}
}

// The three dialer shapes must all reach the same endpoint.
func TestAllDialerShapesReachTheEndpoint(t *testing.T) {
	f := New()
	defer f.Close()
	ep := f.Endpoint("ondemand", RPCBufSize)

	go func() {
		for {
			conn, err := ep.Listener().Accept()
			if err != nil {
				return
			}
			go func() { _, _ = conn.Write([]byte("x")); conn.Close() }()
		}
	}()

	ctx := context.Background()
	dials := map[string]func() (net.Conn, error){
		"Dial":        ep.Dial,
		"DialContext": func() (net.Conn, error) { return ep.DialContext(ctx, "tcp", "127.0.0.1:8080") },
		"DialGRPC":    func() (net.Conn, error) { return ep.DialGRPC(ctx, "passthrough:///ondemand") },
	}
	for name, dial := range dials {
		conn, err := dial()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		b := make([]byte, 1)
		if _, err := io.ReadFull(conn, b); err != nil {
			t.Fatalf("%s read: %v", name, err)
		}
		conn.Close()
	}
}

// The JAGGRAB case: a port with no endpoint must error, not mis-route.
func TestDialPortRejectsUnknownPort(t *testing.T) {
	f := New()
	defer f.Close()
	ep := f.Endpoint("world", RPCBufSize)
	f.BindPort(43594, ep)

	if _, err := f.DialPort(43595); err == nil {
		t.Fatal("DialPort(43595) = nil error, want an error for an unserved port")
	}
}

func TestDialPortReachesBoundPort(t *testing.T) {
	f := New()
	defer f.Close()
	ep := f.Endpoint("world", RPCBufSize)
	f.BindPort(43594, ep)

	go func() {
		conn, err := ep.Listener().Accept()
		if err != nil {
			return
		}
		_, _ = conn.Write([]byte("y"))
		conn.Close()
	}()

	conn, err := f.DialPort(43594)
	if err != nil {
		t.Fatalf("DialPort: %v", err)
	}
	defer conn.Close()
	b := make([]byte, 1)
	if _, err := io.ReadFull(conn, b); err != nil {
		t.Fatalf("read: %v", err)
	}
}

// Mirrors signlink.OpenSocket's dialTimeout: if nothing ever calls Accept on
// the endpoint (e.g. the server died during startup, or the fabric wired the
// wrong endpoint to a port), DialPort must return an error instead of
// blocking forever. Shrinks dialPortTimeout for the duration so the test
// stays fast rather than waiting out the real 10s bound.
func TestDialPortTimesOutWhenNothingAccepts(t *testing.T) {
	orig := dialPortTimeout
	dialPortTimeout = 200 * time.Millisecond
	t.Cleanup(func() { dialPortTimeout = orig })

	f := New()
	defer f.Close()
	ep := f.Endpoint("world", RPCBufSize)
	f.BindPort(43594, ep)
	// Deliberately no goroutine calling ep.Listener().Accept().

	done := make(chan error, 1)
	go func() {
		_, err := f.DialPort(43594)
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("DialPort = nil error, want a timeout error when nothing accepts")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("DialPort did not return within 5s; it appears to block forever when nothing accepts")
	}
}

func TestConnHonoursDeadlines(t *testing.T) {
	f := New()
	defer f.Close()
	ep := f.Endpoint("world", RPCBufSize)

	go func() {
		conn, err := ep.Listener().Accept()
		if err != nil {
			return
		}
		// Hold the conn open without writing.
		time.Sleep(2 * time.Second)
		conn.Close()
	}()

	conn, err := ep.Dial()
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close()

	if err := conn.SetReadDeadline(time.Now().Add(50 * time.Millisecond)); err != nil {
		t.Fatalf("SetReadDeadline: %v", err)
	}
	_, err = conn.Read(make([]byte, 1))
	if err == nil {
		t.Fatal("Read = nil error, want a deadline error")
	}
	if !errors.Is(err, os.ErrDeadlineExceeded) && !isTimeout(err) {
		t.Fatalf("Read error = %v, want a timeout", err)
	}
}

func isTimeout(err error) bool {
	var te interface{ Timeout() bool }
	return errors.As(err, &te) && te.Timeout()
}

// The ondemand archive case: a transfer far larger than the buffer must
// stream through rather than deadlock.
func TestLargeTransferDoesNotDeadlock(t *testing.T) {
	f := New()
	defer f.Close()
	ep := f.Endpoint("ondemand", OndemandBufSize)

	const size = 8 << 20 // 8 MB, comparable to main_file_cache.dat
	go func() {
		conn, err := ep.Listener().Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_, _ = io.Copy(conn, io.LimitReader(zeroReader{}, size))
	}()

	conn, err := ep.Dial()
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close()

	done := make(chan int64, 1)
	go func() {
		n, _ := io.Copy(io.Discard, conn)
		done <- n
	}()

	select {
	case n := <-done:
		if n != size {
			t.Fatalf("copied %d bytes, want %d", n, size)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("8 MB transfer deadlocked")
	}
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 0
	}
	return len(p), nil
}
