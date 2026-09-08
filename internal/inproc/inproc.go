// Package inproc provides the in-memory transports that let
// goscape-singleplayer run the whole game — client and server stack — in one
// process with no sockets. Each Endpoint is a named bufconn listener that the
// server side is handed as a net.Listener and the client side reaches through
// one of three dialer shapes, one per consumer API.
//
// Every protocol stays intact: gRPC still marshals protobuf, ondemand still
// parses HTTP, and the world still runs its accept loop. Only the transport
// underneath is memory instead of a socket.
package inproc

import (
	"context"
	"fmt"
	"net"
	"sync"

	"google.golang.org/grpc/test/bufconn"
)

// Buffer sizes, chosen for what each endpoint carries. A full buffer applies
// backpressure rather than deadlocking, because both sides of every endpoint
// read continuously on their own goroutines.
const (
	// RPCBufSize covers the world game protocol and the login/friends gRPC
	// bridges: small, frequent messages.
	RPCBufSize = 256 << 10 // 256 KiB
	// OndemandBufSize is larger because ondemand streams multi-megabyte
	// cache archives.
	OndemandBufSize = 1 << 20 // 1 MiB
)

// Endpoint is one named in-memory listener plus the dialers its clients need.
// The zero value is not usable; get one from Fabric.Endpoint.
type Endpoint struct {
	name string
	lis  *bufconn.Listener
}

// Name reports the endpoint's name, for logs and errors.
func (e *Endpoint) Name() string { return e.name }

// Listener is handed to the server module, which takes ownership.
func (e *Endpoint) Listener() net.Listener { return e.lis }

// Dial is the shape clientextras.DialInProc wants.
func (e *Endpoint) Dial() (net.Conn, error) { return e.lis.Dial() }

// DialContext is the shape http.Transport.DialContext wants. network and addr
// are ignored: the endpoint is already chosen, and the client's request URLs
// keep their original host:port purely so the ported call sites stay
// unchanged.
func (e *Endpoint) DialContext(ctx context.Context, _, _ string) (net.Conn, error) {
	return e.lis.DialContext(ctx)
}

// DialGRPC is the shape grpc.WithContextDialer wants. The target is ignored
// for the same reason; gRPC still resolves it, so callers pass
// "passthrough:///<name>".
func (e *Endpoint) DialGRPC(ctx context.Context, _ string) (net.Conn, error) {
	return e.lis.DialContext(ctx)
}

// Fabric owns every endpoint in the process and the port map the game client
// dials through.
type Fabric struct {
	mu        sync.Mutex
	endpoints map[string]*Endpoint
	ports     map[int]*Endpoint
}

// New returns an empty Fabric.
func New() *Fabric {
	return &Fabric{
		endpoints: make(map[string]*Endpoint),
		ports:     make(map[int]*Endpoint),
	}
}

// Endpoint creates (or returns) the named endpoint. bufSize is used only on
// creation.
func (f *Fabric) Endpoint(name string, bufSize int) *Endpoint {
	f.mu.Lock()
	defer f.mu.Unlock()
	if ep, ok := f.endpoints[name]; ok {
		return ep
	}
	ep := &Endpoint{name: name, lis: bufconn.Listen(bufSize)}
	f.endpoints[name] = ep
	return ep
}

// BindPort maps a TCP port number onto an endpoint, so the game client — which
// asks for a port, not a name — can be routed. Ports with no binding are
// refused by DialPort.
func (f *Fabric) BindPort(port int, ep *Endpoint) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ports[port] = ep
}

// DialPort is the shape clientextras.DialInProc wants. An unbound port must
// error rather than fall through to another endpoint: the client asks for
// 43595 when the user toggles JAGGRAB on, and this process serves no JAGGRAB
// endpoint. The error makes the client fall back to HTTP exactly as a refused
// TCP connection would.
func (f *Fabric) DialPort(port int) (net.Conn, error) {
	f.mu.Lock()
	ep, ok := f.ports[port]
	f.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("inproc: no endpoint serves port %d", port)
	}
	return ep.Dial()
}

// Close shuts every endpoint down. Call it only after the server stack has
// stopped, so in-flight player saves complete first.
func (f *Fabric) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	var firstErr error
	for _, ep := range f.endpoints {
		if err := ep.lis.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
