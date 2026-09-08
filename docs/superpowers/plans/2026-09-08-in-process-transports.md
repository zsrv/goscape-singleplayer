# In-Process Transports Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Remove all four loopback TCP listeners from goscape-singleplayer by
running world, ondemand, login and friends over in-memory connections.

**Architecture:** Inject a pre-made `net.Listener` at each of goscape's four
bind sites and a dialer at each client, keeping every protocol intact — gRPC
still marshals protobuf, ondemand still parses HTTP, world still runs its
accept loop. goscape-client gains two injectable hooks. goscape-singleplayer
gains an `internal/inproc` fabric that owns four `bufconn` endpoints and wires
both sides together. `--expose-tcp` restores the old ports for debugging.

**Tech Stack:** Go 1.27, `google.golang.org/grpc/test/bufconn`, CGO (GLFW /
OpenGL / ALSA for the client window).

**Spec:** `docs/superpowers/specs/2026-09-08-in-process-transports-design.md`

## Global Constraints

- **Three repos.** `../goscape` and `../goscape-client` are sibling checkouts
  on branch `rev-274`, at the SHAs `go.mod` pins. This repo is on `rev-274`.
- **Scope is rev-274 only.** Backports to rev-254, rev-245.2, rev-244 and
  rev-225 are a separate plan written after this one is proven.
- **Upstream changes are additive.** Every new field is optional; with it
  unset, goscape and goscape-client behave exactly as today. Multiplayer must
  not change.
- **Go invocations need:** `GOPATH=$TMPDIR/go GOCACHE=$TMPDIR/go-cache
  GOPROXY=https://proxy.golang.org,direct` — the configured
  `artifactory.internal.nist.ch` is unreachable from this sandbox.
- **Commits:** `git commit --no-gpg-sign`, with
  `GIT_AUTHOR_DATE`/`GIT_COMMITTER_DATE` set to `<UTC date>T00:00:00+0000`
  (this repo family sanitizes every commit to 00:00 UTC on the UTC date).
  End every message with
  `Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>`.
- **Stage explicit paths.** `git status` surfaces phantom untracked dotfiles
  here; never `git add -A`.
- **Never commit `go.work` or the `replace` block.** They are local-only.
- **Before any PR in this repo:** `gofmt -l .` (must be empty),
  `go vet ./...`, `go test ./...`.

## File Structure

**goscape** (`../goscape`) — six optional fields, six branches at bind sites:

| File | Responsibility |
|---|---|
| `pkg/dskit/server/server.go` | `Config.Listener`; adopt or bind in `newServer` |
| `modules/login/config.go` | `Config.Listener`; relax port validation |
| `modules/login/server.go` | `listen` returns the injected listener |
| `modules/friends/config.go` | `Config.Listener`; relax port validation |
| `modules/friends/server.go` | `listen` returns the injected listener |
| `modules/world/config.go` | `Config.Listener`, two dialer fields; relax port validation |
| `modules/world/server.go` | `Listen()` adopts the injected listener |
| `modules/world/login_client.go` | variadic `grpc.DialOption` |
| `modules/world/friends_client.go` | variadic `grpc.DialOption` |
| `modules/world/world.go` | pass `grpc.WithContextDialer` when configured |

**goscape-client** (`../goscape-client`):

| File | Responsibility |
|---|---|
| `pkg/jagex2/client/clientextras/clientextras.go` | `TransportInProc`, `DialInProc`, `HTTPClient` |
| `pkg/sign/signlink/signlink.go` | `OpenSocket` in-proc case; `HTTPClient` at the OpenURL pump |
| `pkg/jagex2/client/client.go` | `HTTPClient` at the codebase fetch |
| `pkg/jagex2/launch/launch.go` | carry both hooks through `Options`/`configure` |

**goscape-singleplayer** (this repo):

| File | Responsibility |
|---|---|
| `internal/inproc/inproc.go` | the fabric: named `bufconn` endpoints + three dialer shapes |
| `internal/inproc/inproc_test.go` | round-trip, unknown endpoint, deadlines, 8 MB transfer |
| `internal/server/config.go` | wire listeners and dialers into `app.Config` |
| `internal/server/server.go` | `WaitReady` over an injected `*http.Client` |
| `internal/server/server_test.go` | `TestServerBootsInProcess` |
| `cmd/goscape-singleplayer/main.go` | `--expose-tcp`; hand the client its hooks |

---

### Task 1: Development setup

**Files:**
- Create: `../goscape-singleplayer/.gitignore` (modify)
- Delete: `go.work`

**Interfaces:**
- Consumes: nothing.
- Produces: a working three-repo build; every later task depends on it.

- [ ] **Step 1: Remove the scratch `go.work` and add the replace block**

```bash
cd /home/owner/Code/github.com/zsrv/goscape-singleplayer
rm -f go.work go.work.sum
```

Append to `go.mod` (CONTRIBUTING prescribes this; it must never be committed):

```
replace (
	github.com/zsrv/goscape        => ../goscape
	github.com/zsrv/goscape-client => ../goscape-client
)
```

- [ ] **Step 2: Guard against committing it**

Append to `.gitignore`:

```
# Local cross-repo development only — never commit (see CONTRIBUTING.md).
go.work
go.work.sum
```

- [ ] **Step 3: Verify the baseline builds and the boot test passes**

Run:

```bash
export GOPATH=$TMPDIR/go GOCACHE=$TMPDIR/go-cache GOPROXY=https://proxy.golang.org,direct
go build ./... && go test ./internal/server/ -run TestServerBootsReadyAndStops -v
```

Expected: PASS, with four listener log lines (`server listening`,
`friends gRPC server listening`, `login gRPC server listening`,
`tcp server listening`). Those four lines are what this plan removes.

- [ ] **Step 4: Commit the gitignore only**

```bash
git add .gitignore
GIT_AUTHOR_DATE="2026-09-08T00:00:00+0000" GIT_COMMITTER_DATE="2026-09-08T00:00:00+0000" \
  git commit --no-gpg-sign -m "build: ignore go.work so cross-repo dev cannot leak into a commit

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

Do **not** commit `go.mod` — the replace block stays local.

---

### Task 2: goscape — dskit server listener injection

**Files:**
- Modify: `../goscape/pkg/dskit/server/server.go:34-46` (Config), `:83-100` (newServer)
- Test: `../goscape/pkg/dskit/server/server_test.go`

**Interfaces:**
- Produces: `server.Config.Listener net.Listener` — when non-nil, `New` serves
  it instead of binding. The Server takes ownership and closes it on shutdown.

- [ ] **Step 1: Write the failing test**

Add to `../goscape/pkg/dskit/server/server_test.go`:

```go
func TestNewServesInjectedListener(t *testing.T) {
	lis := bufconn.Listen(64 * 1024)
	cfg := Config{
		Listener: lis,
		Log:      slog.New(slog.DiscardHandler),
	}
	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer srv.Close()

	srv.HTTP.HandleFunc("GET /ping", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("pong"))
	})
	go func() { _ = srv.HTTPServer.Serve(lis) }()

	client := &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		},
	}}
	resp, err := client.Get("http://bufconn/ping")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "pong" {
		t.Fatalf("body = %q, want %q", body, "pong")
	}
}
```

Imports to add: `context`, `io`, `log/slog`, `net`, `net/http`,
`google.golang.org/grpc/test/bufconn`.

- [ ] **Step 2: Run it and confirm it fails**

```bash
cd ../goscape && GOPATH=$TMPDIR/go GOCACHE=$TMPDIR/go-cache GOPROXY=https://proxy.golang.org,direct \
  go test ./pkg/dskit/server/ -run TestNewServesInjectedListener -v
```

Expected: FAIL — `unknown field Listener in struct literal`.

- [ ] **Step 3: Add the field**

In `Config`, beside the other `yaml:"-"` fields:

```go
	// Listener, when non-nil, is served instead of binding
	// HTTPListenAddress:HTTPListenPort. The Server takes ownership and
	// closes it on shutdown exactly as it would a bound listener. Set by
	// embedders that run the server on an in-memory transport; nil (the
	// default) preserves the bind-a-socket behaviour.
	Listener net.Listener `yaml:"-"`
```

- [ ] **Step 4: Adopt it in `newServer`**

Replace the bind block (currently `server.go:90-99`):

```go
	// Set up listeners first, so we can fail early if the port is in use.
	// An injected listener short-circuits the bind entirely (in-process
	// transports); ownership transfers to the Server either way.
	httpListener := cfg.Listener
	if httpListener == nil {
		network := cfg.HTTPListenNetwork
		if network == "" {
			network = DefaultNetwork
		}
		var err error
		httpListener, err = net.Listen(network, net.JoinHostPort(cfg.HTTPListenAddress, strconv.Itoa(cfg.HTTPListenPort)))
		if err != nil {
			return nil, err
		}
	}

	logger.Info("server listening", "http", httpListener.Addr())
```

- [ ] **Step 5: Run the test and the package suite**

```bash
go test ./pkg/dskit/server/ -v
```

Expected: PASS, including all pre-existing tests.

- [ ] **Step 6: Commit**

```bash
cd ../goscape && git add pkg/dskit/server/server.go pkg/dskit/server/server_test.go
GIT_AUTHOR_DATE="2026-09-08T00:00:00+0000" GIT_COMMITTER_DATE="2026-09-08T00:00:00+0000" \
  git commit --no-gpg-sign -m "feat(dskit): serve an injected listener when one is configured

Config.Listener lets an embedder hand the server a pre-made net.Listener
instead of binding HTTPListenAddress:HTTPListenPort, so the HTTP stack can
run over an in-memory transport with no socket. Unset, behaviour is
unchanged.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 3: goscape — login listener injection

**Files:**
- Modify: `../goscape/modules/login/config.go` (Config struct, `Validate`)
- Modify: `../goscape/modules/login/server.go:44-52` (`listen`)
- Test: `../goscape/modules/login/server_test.go` (create if absent)

**Interfaces:**
- Produces: `login.Config.Listener net.Listener` — when non-nil, `listen`
  returns it and no port is bound; `Validate` skips the port-range check.

- [ ] **Step 1: Write the failing test**

Create/append `../goscape/modules/login/server_test.go`:

```go
func TestListenReturnsInjectedListener(t *testing.T) {
	lis := bufconn.Listen(64 * 1024)
	s := &grpcServer{log: slog.New(slog.DiscardHandler)}

	got, err := s.listen(Config{Listener: lis})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	if got != lis {
		t.Fatalf("listen returned %v, want the injected listener", got)
	}
}

func TestValidateSkipsPortCheckWhenListenerInjected(t *testing.T) {
	cfg := Config{
		Enable:     true,
		Listener:   bufconn.Listen(64 * 1024),
		BCryptCost: 10,
		SavePath:   "data/players",
		AuthMode:   AuthModeLocal,
		// GRPCListenPort deliberately 0 — meaningless with a listener.
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}
```

- [ ] **Step 2: Run it and confirm it fails**

```bash
go test ./modules/login/ -run 'TestListenReturnsInjectedListener|TestValidateSkipsPortCheck' -v
```

Expected: FAIL — `unknown field Listener`.

- [ ] **Step 3: Add the field**

In `login.Config`:

```go
	// Listener, when non-nil, is served instead of binding
	// GRPCListenAddress:GRPCListenPort, and GRPCListenPort is ignored.
	// Set by embedders running login on an in-memory transport.
	Listener net.Listener `yaml:"-"`
```

Add `"net"` to the imports.

- [ ] **Step 4: Return it from `listen` and relax `Validate`**

`server.go`:

```go
func (s *grpcServer) listen(cfg Config) (net.Listener, error) {
	if cfg.Listener != nil {
		s.log.Info("login gRPC server listening", slog.String("addr", cfg.Listener.Addr().String()))
		return cfg.Listener, nil
	}
	addr := fmt.Sprintf("%s:%d", cfg.GRPCListenAddress, cfg.GRPCListenPort)
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("grpc listen %s: %w", addr, err)
	}
	s.log.Info("login gRPC server listening", slog.String("addr", addr))
	return lis, nil
}
```

`config.go`, replacing the port check at `:72-74`:

```go
	// An injected listener makes GRPCListenPort meaningless — skip the
	// range check rather than force embedders to set a fake port.
	if c.Listener == nil && (c.GRPCListenPort < 1 || c.GRPCListenPort > 65535) {
		return fmt.Errorf("login: GRPCListenPort must be in [1, 65535], got %d", c.GRPCListenPort)
	}
```

- [ ] **Step 5: Run the package suite**

```bash
go test ./modules/login/ -v
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add modules/login/config.go modules/login/server.go modules/login/server_test.go
GIT_AUTHOR_DATE="2026-09-08T00:00:00+0000" GIT_COMMITTER_DATE="2026-09-08T00:00:00+0000" \
  git commit --no-gpg-sign -m "feat(login): serve an injected listener when one is configured

Config.Listener lets an embedder run the login gRPC server over an
in-memory transport. Validate skips the port-range check when it is set,
since GRPCListenPort is then meaningless. Unset, behaviour is unchanged.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 4: goscape — friends listener injection

**Files:**
- Modify: `../goscape/modules/friends/config.go` (Config struct, `Validate`)
- Modify: `../goscape/modules/friends/server.go:75-84` (`listen`)
- Test: `../goscape/modules/friends/server_test.go` (create if absent)

**Interfaces:**
- Produces: `friends.Config.Listener net.Listener`, same contract as login's.

- [ ] **Step 1: Write the failing test**

Create/append `../goscape/modules/friends/server_test.go`:

```go
func TestListenReturnsInjectedListener(t *testing.T) {
	lis := bufconn.Listen(64 * 1024)
	s := &grpcServer{log: slog.New(slog.DiscardHandler)}

	got, err := s.listen(Config{Listener: lis})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	if got != lis {
		t.Fatalf("listen returned %v, want the injected listener", got)
	}
}

func TestValidateSkipsPortCheckWhenListenerInjected(t *testing.T) {
	cfg := Config{
		Enable:           true,
		Listener:         bufconn.Listen(64 * 1024),
		WorldPlayerLimit: 2000,
		Profile:          "main",
		// GRPCListenPort deliberately 0.
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}
```

- [ ] **Step 2: Run it and confirm it fails**

```bash
go test ./modules/friends/ -run 'TestListenReturnsInjectedListener|TestValidateSkipsPortCheck' -v
```

Expected: FAIL — `unknown field Listener`.

- [ ] **Step 3: Add the field**

In `friends.Config`:

```go
	// Listener, when non-nil, is served instead of binding
	// GRPCListenAddress:GRPCListenPort, and GRPCListenPort is ignored.
	// Set by embedders running friends on an in-memory transport.
	Listener net.Listener `yaml:"-"`
```

Add `"net"` to the imports.

- [ ] **Step 4: Return it from `listen` and relax `Validate`**

`server.go`:

```go
func (s *grpcServer) listen(cfg Config) (net.Listener, error) {
	if cfg.Listener != nil {
		s.log.Info("friends gRPC server listening", slog.String("addr", cfg.Listener.Addr().String()))
		return cfg.Listener, nil
	}
	addr := fmt.Sprintf("%s:%d", cfg.GRPCListenAddress, cfg.GRPCListenPort)
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("grpc listen %s: %w", addr, err)
	}
	s.log.Info("friends gRPC server listening", slog.String("addr", addr))
	return lis, nil
}
```

`config.go`, replacing the port check:

```go
	// An injected listener makes GRPCListenPort meaningless — skip the
	// range check rather than force embedders to set a fake port.
	if c.Listener == nil && (c.GRPCListenPort < 1 || c.GRPCListenPort > 65535) {
		return fmt.Errorf("friends: GRPCListenPort must be in [1, 65535], got %d", c.GRPCListenPort)
	}
```

- [ ] **Step 5: Run the package suite**

```bash
go test ./modules/friends/ -v
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add modules/friends/config.go modules/friends/server.go modules/friends/server_test.go
GIT_AUTHOR_DATE="2026-09-08T00:00:00+0000" GIT_COMMITTER_DATE="2026-09-08T00:00:00+0000" \
  git commit --no-gpg-sign -m "feat(friends): serve an injected listener when one is configured

Mirrors the login change: Config.Listener lets an embedder run the friends
gRPC server over an in-memory transport, and Validate skips the port-range
check when it is set. Unset, behaviour is unchanged.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 5: goscape — world listener injection

**Files:**
- Modify: `../goscape/modules/world/config.go` (Config struct, `Validate`)
- Modify: `../goscape/modules/world/server.go:803-812` (`Listen`)
- Test: `../goscape/modules/world/server_listen_test.go` (create)

**Interfaces:**
- Produces: `world.Config.Listener net.Listener` — `Server.Listen()` adopts it
  as `s.tcpListener`. `serveTCP`, the admission gate, `tcpWg` tracking and
  `Shutdown` are untouched: they already operate on the interface.

- [ ] **Step 1: Write the failing test**

Create `../goscape/modules/world/server_listen_test.go`:

```go
package world

import (
	"log/slog"
	"testing"

	"google.golang.org/grpc/test/bufconn"
)

func TestListenAdoptsInjectedListener(t *testing.T) {
	lis := bufconn.Listen(64 * 1024)
	s := &Server{
		cfg: Config{Listener: lis},
		log: slog.New(slog.DiscardHandler),
	}

	if err := s.Listen(); err != nil {
		t.Fatalf("Listen: %v", err)
	}
	if s.tcpListener != lis {
		t.Fatalf("tcpListener = %v, want the injected listener", s.tcpListener)
	}
}

func TestValidateSkipsPortCheckWhenListenerInjected(t *testing.T) {
	cfg := Config{
		Enable:    true,
		Listener:  bufconn.Listen(64 * 1024),
		CachePath: "data/pack",
		// TCPListenPort deliberately 0.
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}
```

- [ ] **Step 2: Run it and confirm it fails**

```bash
go test ./modules/world/ -run 'TestListenAdoptsInjectedListener|TestValidateSkipsPortCheck' -v
```

Expected: FAIL — `unknown field Listener`.

- [ ] **Step 3: Add the field**

In `world.Config`, beside `SignalHandler`:

```go
	// Listener, when non-nil, is adopted by Listen() instead of binding
	// TCPListenAddress:TCPListenPort, and TCPListenPort is ignored. The
	// accept loop, admission gate and shutdown path are unchanged — they
	// operate on the net.Listener interface. Set by embedders running the
	// world on an in-memory transport.
	Listener net.Listener `yaml:"-"`
```

Add `"net"` to the imports.

- [ ] **Step 4: Adopt it in `Listen` and relax `Validate`**

`server.go`:

```go
func (s *Server) Listen() error {
	if s.cfg.Listener != nil {
		s.log.Info("tcp server listening", "addr", s.cfg.Listener.Addr())
		s.tcpListener = s.cfg.Listener
		return nil
	}
	tcpListener, err := net.Listen(s.cfg.TCPListenNetwork, net.JoinHostPort(s.cfg.TCPListenAddress, strconv.Itoa(s.cfg.TCPListenPort)))
	if err != nil {
		return fmt.Errorf("failed to create tcp listener: %w", err)
	}
	s.log.Info("tcp server listening", "addr", tcpListener.Addr())
	s.tcpListener = tcpListener
	return nil
}
```

`config.go`, replacing the port check at `:137-139`:

```go
		// An injected listener makes TCPListenPort meaningless — skip the
		// range check rather than force embedders to set a fake port.
		if c.Listener == nil && (c.TCPListenPort < 1 || c.TCPListenPort > 65535) {
			return fmt.Errorf("world.tcp-listen-port must be in [1, 65535], got %d", c.TCPListenPort)
		}
```

- [ ] **Step 5: Run the package suite**

```bash
go test ./modules/world/ -v 2>&1 | tail -20
```

Expected: PASS. The shutdown and conn-handler suites must stay green — they
exercise the accept-loop path this change feeds.

- [ ] **Step 6: Commit**

```bash
git add modules/world/config.go modules/world/server.go modules/world/server_listen_test.go
GIT_AUTHOR_DATE="2026-09-08T00:00:00+0000" GIT_COMMITTER_DATE="2026-09-08T00:00:00+0000" \
  git commit --no-gpg-sign -m "feat(world): adopt an injected listener when one is configured

Config.Listener lets an embedder run the game protocol over an in-memory
transport. Listen() adopts it as s.tcpListener; serveTCP, the admission
gate, tcpWg tracking and Shutdown are untouched because they already
operate on the net.Listener interface. Unset, behaviour is unchanged.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 6: goscape — world gRPC dialer injection

**Files:**
- Modify: `../goscape/modules/world/login_client.go:37-50`
- Modify: `../goscape/modules/world/friends_client.go:86-99`
- Modify: `../goscape/modules/world/config.go` (two dialer fields)
- Modify: `../goscape/modules/world/world.go:48-70`
- Test: `../goscape/modules/world/grpc_dialer_test.go` (create)

**Interfaces:**
- Produces:
  - `NewLoginClient(addr string, log *slog.Logger, opts ...grpc.DialOption) (LoginClient, error)`
  - `NewFriendsClient(addr string, log *slog.Logger, opts ...grpc.DialOption) (FriendsClient, error)`
  - `world.Config.LoginServerDialer` and `.FriendsServerDialer`, both
    `func(context.Context, string) (net.Conn, error)`

**Why variadic:** `NewFriendsClient` has 11 callers in
`friends_smoke_test.go`. A variadic option is backward-compatible and needs no
new type, so those callers are untouched.

- [ ] **Step 1: Write the failing test**

Create `../goscape/modules/world/grpc_dialer_test.go`:

```go
package world

import (
	"context"
	"log/slog"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/test/bufconn"

	"github.com/zsrv/goscape/pkg/loginpb"
)

// A login client built with an injected dialer must reach a bufconn-served
// gRPC server with no TCP anywhere in the path.
func TestNewLoginClientUsesInjectedDialer(t *testing.T) {
	lis := bufconn.Listen(64 * 1024)
	srv := grpc.NewServer()
	loginpb.RegisterLoginServiceServer(srv, &stubLoginServer{})
	go func() { _ = srv.Serve(lis) }()
	defer srv.Stop()

	dial := func(ctx context.Context, _ string) (net.Conn, error) {
		return lis.DialContext(ctx)
	}

	c, err := NewLoginClient("passthrough:///login", slog.New(slog.DiscardHandler),
		grpc.WithContextDialer(dial))
	if err != nil {
		t.Fatalf("NewLoginClient: %v", err)
	}
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.WorldStartup(ctx, 10, "main"); err != nil {
		t.Fatalf("WorldStartup over bufconn: %v", err)
	}
}

type stubLoginServer struct {
	loginpb.UnimplementedLoginServiceServer
}

func (s *stubLoginServer) WorldStartup(context.Context, *loginpb.WorldStartupRequest) (*loginpb.WorldStartupResponse, error) {
	return &loginpb.WorldStartupResponse{}, nil
}
```

- [ ] **Step 2: Run it and confirm it fails**

```bash
go test ./modules/world/ -run TestNewLoginClientUsesInjectedDialer -v
```

Expected: FAIL — too many arguments to `NewLoginClient`.

- [ ] **Step 3: Make both constructors variadic**

`login_client.go`:

```go
// NewLoginClient creates a non-blocking gRPC client to the login server.
// grpc.NewClient does not block — connection is established lazily with automatic retry.
// Extra dial options are appended last, so an embedder can supply
// grpc.WithContextDialer to run the RPC over an in-memory transport.
func NewLoginClient(addr string, log *slog.Logger, opts ...grpc.DialOption) (LoginClient, error) {
	dialOpts := append([]grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		worldClientKeepalive(),
	}, opts...)
	conn, err := grpc.NewClient(addr, dialOpts...)
	if err != nil {
		return nil, fmt.Errorf("grpc dial login server: %w", err)
	}
	return &grpcLoginClient{
		conn:   conn,
		client: loginpb.NewLoginServiceClient(conn),
		log:    log,
	}, nil
}
```

`friends_client.go`, identically:

```go
func NewFriendsClient(addr string, log *slog.Logger, opts ...grpc.DialOption) (FriendsClient, error) {
	dialOpts := append([]grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		worldClientKeepalive(),
	}, opts...)
	conn, err := grpc.NewClient(addr, dialOpts...)
	if err != nil {
		return nil, fmt.Errorf("grpc dial friends server: %w", err)
	}
	return &grpcFriendsClient{
		conn:   conn,
		client: friendspb.NewFriendsServiceClient(conn),
		log:    log,
	}, nil
}
```

- [ ] **Step 4: Add the config fields**

In `world.Config`, beside `Listener`:

```go
	// LoginServerDialer and FriendsServerDialer, when non-nil, are passed to
	// grpc.WithContextDialer so the bridge RPCs run over an in-memory
	// transport. LoginServerAddress / FriendsServerAddress are then only a
	// gRPC target string — use "passthrough:///login" and
	// "passthrough:///friends", since gRPC still resolves the target even
	// when a dialer is supplied.
	LoginServerDialer   func(context.Context, string) (net.Conn, error) `yaml:"-"`
	FriendsServerDialer func(context.Context, string) (net.Conn, error) `yaml:"-"`
```

Add `"context"` to the imports.

- [ ] **Step 5: Pass them through in `world.go`**

Replace the two client-construction blocks (`world.go:48-70`):

```go
	var loginClient LoginClient
	if cfg.LoginServerEnabled {
		var opts []grpc.DialOption
		if cfg.LoginServerDialer != nil {
			opts = append(opts, grpc.WithContextDialer(cfg.LoginServerDialer))
		}
		lc, err := NewLoginClient(cfg.LoginServerAddress, logger.With("component", compLogin), opts...)
		if err != nil {
			// Log the error but don't fail startup — the world should run even if login is unreachable.
			w.log.Warn("failed to create login client", slog.Any("err", err))
		} else {
			loginClient = lc
		}
	}
	w.loginClient = loginClient

	var friendsClient FriendsClient
	if cfg.FriendsServerEnabled {
		var opts []grpc.DialOption
		if cfg.FriendsServerDialer != nil {
			opts = append(opts, grpc.WithContextDialer(cfg.FriendsServerDialer))
		}
		fc, err := NewFriendsClient(cfg.FriendsServerAddress, logger.With("component", compFriends), opts...)
		if err != nil {
			w.log.Warn("failed to create friends client", slog.Any("err", err))
		} else {
			friendsClient = fc
		}
	}
	w.friendsClient = friendsClient
```

Add `"google.golang.org/grpc"` to `world.go`'s imports.

- [ ] **Step 6: Run the world suite**

```bash
go test ./modules/world/ -v 2>&1 | tail -20
```

Expected: PASS, including the 11 untouched `friends_smoke_test.go` callers.

- [ ] **Step 7: Verify the whole module still builds and commit**

```bash
gofmt -l modules pkg   # must be empty
go build ./... && go vet ./modules/... 
git add modules/world/login_client.go modules/world/friends_client.go \
        modules/world/config.go modules/world/world.go modules/world/grpc_dialer_test.go
GIT_AUTHOR_DATE="2026-09-08T00:00:00+0000" GIT_COMMITTER_DATE="2026-09-08T00:00:00+0000" \
  git commit --no-gpg-sign -m "feat(world): allow an injected dialer for the login and friends bridges

NewLoginClient and NewFriendsClient take variadic grpc.DialOptions, and
Config gains LoginServerDialer/FriendsServerDialer which world.New turns
into grpc.WithContextDialer. That lets the bridge RPCs run over an
in-memory transport with no TCP. Variadic rather than a changed signature
so the existing smoke-test callers are untouched.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 7: goscape-client — the two hooks

**Files:**
- Modify: `../goscape-client/pkg/jagex2/client/clientextras/clientextras.go`
- Test: `../goscape-client/pkg/jagex2/client/clientextras/clientextras_test.go`

**Interfaces:**
- Produces:
  - `clientextras.TransportInProc` (a `TransportKind`)
  - `clientextras.DialInProc func(port int) (net.Conn, error)`
  - `clientextras.HTTPClient *http.Client` (defaults to `http.DefaultClient`)

- [ ] **Step 1: Write the failing test**

Append to `clientextras_test.go`:

```go
func TestHTTPClientDefaultsToDefaultClient(t *testing.T) {
	if HTTPClient != http.DefaultClient {
		t.Fatalf("HTTPClient = %v, want http.DefaultClient", HTTPClient)
	}
}

func TestTransportInProcIsDistinct(t *testing.T) {
	for _, other := range []TransportKind{TransportTCP, TransportWS, TransportWSS} {
		if TransportInProc == other {
			t.Fatalf("TransportInProc collides with %v", other)
		}
	}
}
```

- [ ] **Step 2: Run it and confirm it fails**

```bash
cd ../goscape-client && GOPATH=$TMPDIR/go GOCACHE=$TMPDIR/go-cache GOPROXY=https://proxy.golang.org,direct \
  go test ./pkg/jagex2/client/clientextras/ -v
```

Expected: FAIL — `undefined: HTTPClient`, `undefined: TransportInProc`.

- [ ] **Step 3: Add the transport kind**

Extend the enum, appending so existing values keep their numbers:

```go
const (
	TransportTCP    TransportKind = iota // raw TCP socket (default; Java parity)
	TransportWS                          // WebSocket (ws://)
	TransportWSS                         // WebSocket over TLS (wss://)
	TransportInProc                      // in-memory conn supplied by DialInProc
)
```

- [ ] **Step 4: Add the two hooks**

```go
// DialInProc supplies the game-server connection when Transport is
// TransportInProc. It takes the port so it can distinguish the world
// socket (WorldPort) from the JAGGRAB socket (43595) and reject the
// latter — an embedder that serves no JAGGRAB endpoint must return an
// error there, which makes the client fall back to HTTP exactly as a
// refused TCP connection would. Nil with TransportInProc set is a
// configuration error and OpenSocket reports it.
var DialInProc func(port int) (net.Conn, error)

// HTTPClient performs the cache/asset fetches that signlink.OpenURL and
// client.GetCodeBase issue. It defaults to http.DefaultClient, which is
// what the standalone build has always used; embedders running ondemand
// on an in-memory transport replace it with a client whose Transport
// dials that endpoint. The request URLs are unchanged either way.
var HTTPClient *http.Client = http.DefaultClient
```

Add `"net"` and `"net/http"` to the imports.

- [ ] **Step 5: Run the test**

```bash
go test ./pkg/jagex2/client/clientextras/ -v
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add pkg/jagex2/client/clientextras/
GIT_AUTHOR_DATE="2026-09-08T00:00:00+0000" GIT_COMMITTER_DATE="2026-09-08T00:00:00+0000" \
  git commit --no-gpg-sign -m "feat(clientextras): add in-process transport and HTTP client hooks

TransportInProc plus DialInProc let an embedder supply the game-server
connection from memory; HTTPClient does the same for the cache/asset
fetches. Both default to today's behaviour (TCP dial, http.DefaultClient).

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 8: goscape-client — OpenSocket in-process case

**Files:**
- Modify: `../goscape-client/pkg/sign/signlink/signlink.go:335-343`
- Test: `../goscape-client/pkg/sign/signlink/signlink_socket_test.go`

**Interfaces:**
- Consumes: `clientextras.TransportInProc`, `clientextras.DialInProc`.
- Produces: `OpenSocket` returns the hook's conn, or the hook's error.

- [ ] **Step 1: Write the failing test**

Append to `signlink_socket_test.go`:

```go
func TestOpenSocketUsesInProcDialer(t *testing.T) {
	prevTransport, prevDial := clientextras.Transport, clientextras.DialInProc
	t.Cleanup(func() {
		clientextras.Transport, clientextras.DialInProc = prevTransport, prevDial
	})

	client, server := net.Pipe()
	t.Cleanup(func() { _ = client.Close(); _ = server.Close() })

	var gotPort int
	clientextras.Transport = clientextras.TransportInProc
	clientextras.DialInProc = func(port int) (net.Conn, error) {
		gotPort = port
		return client, nil
	}

	conn, err := OpenSocket(43594)
	if err != nil {
		t.Fatalf("OpenSocket: %v", err)
	}
	if conn != client {
		t.Fatal("OpenSocket did not return the dialer's conn")
	}
	if gotPort != 43594 {
		t.Fatalf("dialer got port %d, want 43594", gotPort)
	}
}

// The JAGGRAB socket has no in-process endpoint; the dialer's error must
// reach the caller so the client falls back to HTTP.
func TestOpenSocketPropagatesInProcDialerError(t *testing.T) {
	prevTransport, prevDial := clientextras.Transport, clientextras.DialInProc
	t.Cleanup(func() {
		clientextras.Transport, clientextras.DialInProc = prevTransport, prevDial
	})

	clientextras.Transport = clientextras.TransportInProc
	clientextras.DialInProc = func(port int) (net.Conn, error) {
		return nil, fmt.Errorf("no in-process endpoint for port %d", port)
	}

	if _, err := OpenSocket(43595); err == nil {
		t.Fatal("OpenSocket(43595) = nil error, want the dialer's error")
	}
}

func TestOpenSocketInProcWithoutDialerErrors(t *testing.T) {
	prevTransport, prevDial := clientextras.Transport, clientextras.DialInProc
	t.Cleanup(func() {
		clientextras.Transport, clientextras.DialInProc = prevTransport, prevDial
	})

	clientextras.Transport = clientextras.TransportInProc
	clientextras.DialInProc = nil

	if _, err := OpenSocket(43594); err == nil {
		t.Fatal("OpenSocket with nil DialInProc = nil error, want a configuration error")
	}
}
```

- [ ] **Step 2: Run it and confirm it fails**

```bash
go test ./pkg/sign/signlink/ -run TestOpenSocket -v
```

Expected: FAIL — the in-proc cases fall through to `dialTCP` and hang or
return a dial error rather than the injected conn.

- [ ] **Step 3: Add the case**

```go
func OpenSocket(port int) (net.Conn, error) {
	const dialTimeout = 10 * time.Second
	switch clientextras.Transport {
	case clientextras.TransportWS, clientextras.TransportWSS:
		return openWebSocket(clientextras.Transport, clientextras.Host, port, dialTimeout)
	case clientextras.TransportInProc:
		// The embedder owns endpoint selection: it maps the port to one of
		// its in-memory listeners, or errors for a port it does not serve
		// (JAGGRAB's 43595), which the caller treats exactly like a refused
		// TCP connection.
		if clientextras.DialInProc == nil {
			return nil, errors.New("signlink: TransportInProc selected but DialInProc is nil")
		}
		return clientextras.DialInProc(port)
	default:
		return dialTCP(clientextras.Host, port, dialTimeout)
	}
}
```

`errors` is already imported by this file.

- [ ] **Step 4: Run the tests**

```bash
go test ./pkg/sign/signlink/ -v 2>&1 | tail -20
```

Expected: PASS, including the pre-existing TCP and WS transport tests.

- [ ] **Step 5: Commit**

```bash
git add pkg/sign/signlink/signlink.go pkg/sign/signlink/signlink_socket_test.go
GIT_AUTHOR_DATE="2026-09-08T00:00:00+0000" GIT_COMMITTER_DATE="2026-09-08T00:00:00+0000" \
  git commit --no-gpg-sign -m "feat(signlink): dial the game socket in-process when configured

Third case in OpenSocket's transport switch, beside TCP and WebSocket.
The hook receives the port so an embedder can serve the world socket and
refuse JAGGRAB's 43595, which the client already handles as a fallback.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 9: goscape-client — route the cache fetches through HTTPClient

**Files:**
- Modify: `../goscape-client/pkg/sign/signlink/signlink.go:215`
- Modify: `../goscape-client/pkg/jagex2/client/client.go:7241`
- Test: `../goscape-client/pkg/sign/signlink/signlink_url_test.go` (create)

**Interfaces:**
- Consumes: `clientextras.HTTPClient`.
- Produces: both fetch sites honour a replaced `HTTPClient`.

- [ ] **Step 1: Write the failing test**

Create `../goscape-client/pkg/sign/signlink/signlink_url_test.go`:

```go
package signlink

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/zsrv/goscape-client/pkg/jagex2/client/clientextras"
)

// OpenURL must fetch through clientextras.HTTPClient, so an embedder can
// point it at an in-process transport.
func TestOpenURLUsesInjectedHTTPClient(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/crc" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte("crc-body"))
	}))
	t.Cleanup(srv.Close)

	prevClient, prevBase := clientextras.HTTPClient, clientextras.OndemandBaseURL
	t.Cleanup(func() {
		clientextras.HTTPClient, clientextras.OndemandBaseURL = prevClient, prevBase
	})

	var used bool
	clientextras.HTTPClient = &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			used = true
			return http.DefaultTransport.RoundTrip(r)
		}),
	}
	clientextras.OndemandBaseURL = srv.URL

	// signlink exposes no Stop; the pump goroutine ends with the test binary.
	go StartPriv()

	deadline := time.Now().Add(5 * time.Second)
	var body []byte
	var err error
	for time.Now().Before(deadline) {
		body, err = OpenURL("crc")
		if err == nil {
			break
		}
	}
	if err != nil {
		t.Fatalf("OpenURL: %v", err)
	}
	if string(body) != "crc-body" {
		t.Fatalf("body = %q, want %q", body, "crc-body")
	}
	if !used {
		t.Fatal("OpenURL did not go through clientextras.HTTPClient")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
```

- [ ] **Step 2: Run it and confirm it fails**

```bash
go test ./pkg/sign/signlink/ -run TestOpenURLUsesInjectedHTTPClient -v
```

Expected: FAIL — `used` is false; the pump used `http.Get`.

- [ ] **Step 3: Swap both call sites**

`signlink.go:215`:

```go
			resp, err := clientextras.HTTPClient.Get(urlBase() + "/" + urlReq)
```

`client.go:7241`:

```go
	resp, err := clientextras.HTTPClient.Get(c.GetCodeBase() + "/" + arg0)
```

Both files already import `clientextras`. If `net/http` becomes unused in
either after the swap, drop the import.

The surrounding Java-fidelity comments stay accurate — the call shape and the
non-2xx handling are unchanged; only the client instance is now injectable.

- [ ] **Step 4: Run the tests**

```bash
go test ./pkg/sign/signlink/ ./pkg/jagex2/client/ -v 2>&1 | tail -20
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -l pkg   # must be empty
git add pkg/sign/signlink/signlink.go pkg/sign/signlink/signlink_url_test.go pkg/jagex2/client/client.go
GIT_AUTHOR_DATE="2026-09-08T00:00:00+0000" GIT_COMMITTER_DATE="2026-09-08T00:00:00+0000" \
  git commit --no-gpg-sign -m "feat(client): fetch cache assets through the injectable HTTP client

Both fetch sites — the signlink OpenURL pump and GetCodeBase — now use
clientextras.HTTPClient instead of http.Get, so an embedder can serve
ondemand over an in-process transport. The URLs, the non-2xx handling and
the Java-fidelity notes are unchanged; the default is http.DefaultClient.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 10: goscape-client — carry the hooks through launch.Options

**Files:**
- Modify: `../goscape-client/pkg/jagex2/launch/launch.go`
- Test: `../goscape-client/pkg/jagex2/launch/launch_test.go`

**Interfaces:**
- Produces: `launch.Options.DialInProc func(port int) (net.Conn, error)` and
  `launch.Options.HTTPClient *http.Client`, both applied by `configure`.

- [ ] **Step 1: Write the failing test**

Append to `launch_test.go`:

```go
func TestConfigureAppliesInProcHooks(t *testing.T) {
	prevTransport, prevDial, prevClient :=
		clientextras.Transport, clientextras.DialInProc, clientextras.HTTPClient
	t.Cleanup(func() {
		clientextras.Transport = prevTransport
		clientextras.DialInProc = prevDial
		clientextras.HTTPClient = prevClient
	})

	dial := func(int) (net.Conn, error) { return nil, nil }
	httpClient := &http.Client{}

	configure(Options{
		Transport:  clientextras.TransportInProc,
		DialInProc: dial,
		HTTPClient: httpClient,
	})

	if clientextras.Transport != clientextras.TransportInProc {
		t.Fatal("Transport not applied")
	}
	if clientextras.DialInProc == nil {
		t.Fatal("DialInProc not applied")
	}
	if clientextras.HTTPClient != httpClient {
		t.Fatal("HTTPClient not applied")
	}
}

// A caller that supplies neither hook must leave the defaults alone.
func TestConfigureLeavesHTTPClientAloneWhenUnset(t *testing.T) {
	prev := clientextras.HTTPClient
	t.Cleanup(func() { clientextras.HTTPClient = prev })

	configure(Options{Transport: clientextras.TransportTCP})

	if clientextras.HTTPClient != prev {
		t.Fatal("configure overwrote HTTPClient with nil")
	}
}
```

- [ ] **Step 2: Run it and confirm it fails**

```bash
go test ./pkg/jagex2/launch/ -run TestConfigure -v
```

Expected: FAIL — `unknown field DialInProc in struct literal`.

- [ ] **Step 3: Add the fields**

In `Options`:

```go
	DialInProc      func(port int) (net.Conn, error) // in-process game socket; required when Transport is TransportInProc
	HTTPClient      *http.Client                     // cache/asset fetches; nil keeps clientextras' default
```

Add `"net"` and `"net/http"` to the imports.

- [ ] **Step 4: Apply them in `configure`**

After the existing transport assignments:

```go
	// In-process transports: the embedder supplies the game socket and the
	// cache/asset client. Both are left at their defaults when unset, so a
	// TCP caller is unaffected.
	clientextras.DialInProc = opts.DialInProc
	if opts.HTTPClient != nil {
		clientextras.HTTPClient = opts.HTTPClient
	}
```

- [ ] **Step 5: Run the tests**

```bash
go test ./pkg/jagex2/launch/ -v
```

Expected: PASS.

- [ ] **Step 6: Verify the whole client builds, then commit**

```bash
gofmt -l pkg   # must be empty
CGO_ENABLED=1 go build ./... && go vet ./pkg/...
git add pkg/jagex2/launch/
GIT_AUTHOR_DATE="2026-09-08T00:00:00+0000" GIT_COMMITTER_DATE="2026-09-08T00:00:00+0000" \
  git commit --no-gpg-sign -m "feat(launch): carry the in-process hooks through Options

Options.DialInProc and Options.HTTPClient let an embedder select the
in-process transports programmatically, matching how the other transport
settings are already threaded. Unset, both keep clientextras' defaults.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 11: singleplayer — the inproc fabric

**Files:**
- Create: `internal/inproc/inproc.go`
- Test: `internal/inproc/inproc_test.go`

**Interfaces:**
- Produces:
  - `inproc.New() *Fabric`
  - `(*Fabric).Endpoint(name string, bufSize int) *Endpoint`
  - `(*Fabric).Close() error`
  - `(*Endpoint).Listener() net.Listener`
  - `(*Endpoint).Dial() (net.Conn, error)` — for `clientextras.DialInProc`
  - `(*Endpoint).DialContext(ctx context.Context, network, addr string) (net.Conn, error)` — for `http.Transport`
  - `(*Endpoint).DialGRPC(ctx context.Context, target string) (net.Conn, error)` — for `grpc.WithContextDialer`
  - Buffer size constants `RPCBufSize`, `OndemandBufSize`

- [ ] **Step 1: Write the failing tests**

Create `internal/inproc/inproc_test.go`:

```go
package inproc

import (
	"context"
	"errors"
	"io"
	"net"
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
```

Add `"os"` to the imports.

- [ ] **Step 2: Run and confirm it fails**

```bash
cd /home/owner/Code/github.com/zsrv/goscape-singleplayer
GOPATH=$TMPDIR/go GOCACHE=$TMPDIR/go-cache GOPROXY=https://proxy.golang.org,direct \
  go test ./internal/inproc/ -v
```

Expected: FAIL — the package does not exist.

- [ ] **Step 3: Write the fabric**

Create `internal/inproc/inproc.go`:

```go
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
```

- [ ] **Step 4: Run the tests**

```bash
go test ./internal/inproc/ -v
```

Expected: PASS, all six tests, with the 8 MB transfer well under its 30 s
budget.

- [ ] **Step 5: Tidy and commit**

`bufconn` was an indirect dependency; using it directly promotes it.

```bash
go mod tidy && gofmt -l . && go vet ./internal/inproc/
git add internal/inproc/ go.mod go.sum
GIT_AUTHOR_DATE="2026-09-08T00:00:00+0000" GIT_COMMITTER_DATE="2026-09-08T00:00:00+0000" \
  git commit --no-gpg-sign -m "feat(inproc): add the in-memory transport fabric

Named bufconn endpoints with the three dialer shapes their consumers need
(http.Transport, grpc.WithContextDialer, and the client's port-based
socket hook). Unbound ports are refused so a JAGGRAB attempt falls back to
HTTP instead of reaching the world endpoint.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

Check `go mod tidy` did not drop the local `replace` block; if it did, restore
it and do not stage `go.mod`'s replace lines.

---

### Task 12: singleplayer — wire the server side

**Files:**
- Modify: `internal/server/config.go`
- Test: `internal/server/config_test.go`

**Interfaces:**
- Consumes: `inproc.Fabric`, the six new goscape config fields.
- Produces: `server.Options.Fabric *inproc.Fabric` — when non-nil, `NewConfig`
  wires the four listeners and two dialers and the port fields are ignored.

- [ ] **Step 1: Write the failing test**

Append to `internal/server/config_test.go`:

```go
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
```

- [ ] **Step 2: Run and confirm it fails**

```bash
go test ./internal/server/ -run TestNewConfigWires -v
```

Expected: FAIL — `unknown field Fabric in struct literal`.

- [ ] **Step 3: Add the option and wire it**

In `Options`:

```go
	// Fabric, when non-nil, runs every module on in-memory transports and the
	// four port fields are ignored. Nil binds the loopback ports, which is
	// what --expose-tcp selects.
	Fabric *inproc.Fabric
```

**Leave every existing port assignment exactly as it is.** An injected
listener overrides what the module would have bound, so fabric mode is purely
additive. This matters concretely: `cfg.OnDemand.Port` is not the HTTP port
but `ondemand.node-port`, the *world* port `/rs2.cgi` uses to emit
`portoff = node-port - 43594`, and `app/config.go:117` warns when it disagrees
with `cfg.World.TCPListenPort`. Keeping both assigned keeps that check quiet
and keeps `/rs2.cgi` honest.

Append this block to `NewConfig`, immediately before `cfg.Validate()`:

```go
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
```

Add the endpoint-name constants near the top of the file:

```go
// Endpoint names for the in-process fabric. They are routing keys only —
// nothing here ever reaches a socket.
const (
	EndpointWorld    = "world"
	EndpointOndemand = "ondemand"
	EndpointLogin    = "login"
	EndpointFriends  = "friends"
)
```

- [ ] **Step 4: Run the package tests**

```bash
go test ./internal/server/ -v 2>&1 | tail -20
```

Expected: PASS, including the pre-existing config tests.

- [ ] **Step 5: Commit**

```bash
gofmt -l . && go vet ./internal/...
git add internal/server/config.go internal/server/config_test.go
GIT_AUTHOR_DATE="2026-09-08T00:00:00+0000" GIT_COMMITTER_DATE="2026-09-08T00:00:00+0000" \
  git commit --no-gpg-sign -m "feat(server): wire the module stack onto in-memory transports

Options.Fabric hands each module a bufconn listener and gives world's two
gRPC bridges an in-process dialer, so nothing binds a port. Without a
fabric the loopback ports are configured exactly as before, which is what
--expose-tcp will select.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 13: singleplayer — readiness and the client wiring

**Files:**
- Modify: `internal/server/server.go` (`WaitReady`)
- Modify: `cmd/goscape-singleplayer/main.go`

**Interfaces:**
- Produces: `(*Server).WaitReady(ctx context.Context, httpClient *http.Client, baseURL string) error`
- Produces: `--expose-tcp` flag; the client receives `DialInProc` and
  `HTTPClient` when the fabric is in use.

- [ ] **Step 1: Change WaitReady to take a client**

`internal/server/server.go`:

```go
// WaitReady polls the ondemand /crc endpoint until it serves 200, the server
// dies, or ctx expires. /crc is the first thing the game client fetches at
// boot, so "crc answers" is exactly the readiness the client needs. The
// client and base URL come from the caller because the transport underneath
// may be in-memory rather than TCP; in fabric mode bufconn's Dial blocks
// until Accept, which the per-probe timeout already covers.
func (s *Server) WaitReady(ctx context.Context, httpClient *http.Client, baseURL string) error {
	url := baseURL + "/crc"
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	lastProbe := "no probe completed"
	for {
		resp, err := httpClient.Get(url)
		if err == nil {
			if resp.StatusCode == http.StatusOK {
				resp.Body.Close()
				return nil
			}
			lastProbe = fmt.Sprintf("status %d", resp.StatusCode)
			resp.Body.Close()
		} else {
			lastProbe = err.Error()
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("server not ready before deadline (last probe of %s: %s): %w", url, lastProbe, ctx.Err())
		case <-s.done:
			return fmt.Errorf("server exited during startup: %w", s.err)
		case <-ticker.C:
		}
	}
}
```

- [ ] **Step 2: Update the existing boot test's call**

In `internal/server/server_test.go`, replace the `WaitReady` call:

```go
	httpClient := &http.Client{Timeout: 2 * time.Second}
	if err := srv.WaitReady(ctx, httpClient, "http://127.0.0.1:48080"); err != nil {
		t.Fatalf("WaitReady: %v", err)
	}
```

- [ ] **Step 3: Run the boot test**

```bash
go test ./internal/server/ -run TestServerBootsReadyAndStops -v
```

Expected: PASS, unchanged behaviour on the TCP path.

- [ ] **Step 4: Add the flag and wire the client in `main.go`**

Add the flag beside the others:

```go
	exposeTCP := flag.Bool("expose-tcp", false, "bind the loopback ports instead of running everything in-process (debugging: lets a second client, tcpdump or curl reach the server)")
```

After config resolution, build the fabric and the client hooks:

```go
	var fabric *inproc.Fabric
	if !*exposeTCP {
		fabric = inproc.New()
	}

	cfg, err := server.NewConfig(server.Options{
		DataDir:      *dataDir,
		CacheDir:     resolvedCacheDir,
		WordEncPath:  *wordencPath,
		WorldPort:    *worldPort,
		OndemandPort: *ondemandPort,
		LoginPort:    *loginPort,
		FriendsPort:  *friendsPort,
		Fabric:       fabric,
	})
```

Replace the readiness call and the `launch.Run` options:

```go
	// The ondemand base URL is a real URL in both modes; in fabric mode the
	// dialer ignores its host:port, which keeps the client's ported fetch
	// sites unchanged.
	ondemandBaseURL := fmt.Sprintf("http://127.0.0.1:%d", *ondemandPort)
	httpClient := &http.Client{Timeout: 2 * time.Second}
	transport := clientextras.TransportTCP
	var dialInProc func(port int) (net.Conn, error)

	if fabric != nil {
		ondemandEP := fabric.Endpoint(server.EndpointOndemand, inproc.OndemandBufSize)
		httpClient = &http.Client{Transport: &http.Transport{DialContext: ondemandEP.DialContext}}
		transport = clientextras.TransportInProc
		dialInProc = fabric.DialPort
	}

	readyCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	err = srv.WaitReady(readyCtx, httpClient, ondemandBaseURL)
	cancel()
	if err != nil {
		fatalf("server failed to become ready: %v", err)
	}
```

In `clientextras.ExitFunc`, close the fabric **after** the server has stopped:

```go
	clientextras.ExitFunc = func(code int) {
		exitOnce.Do(func() {
			if err := srv.Stop(10 * time.Second); err != nil {
				logger.Error("server shutdown", "err", err)
				if code == 0 {
					code = 1
				}
			}
			// Only after Stop returns: player saves and the sqlite flush
			// happen during service shutdown and still need their transports.
			if fabric != nil {
				_ = fabric.Close()
			}
			os.Exit(code)
		})
		select {}
	}
```

And in `launch.Run`:

```go
	launch.Run(launch.Options{
		NodeID:          10, // must match the server's world.node-id / ondemand.node-id defaults (both 10)
		StoreID:         32,
		LowMemory:       lowMemory,
		Members:         members,
		Host:            "127.0.0.1",
		Transport:       transport,
		WorldPort:       *worldPort,
		WSPath:          "",
		OndemandBaseURL: ondemandBaseURL,
		DialInProc:      dialInProc,
		HTTPClient:      httpClient,
	})
```

Imports to add: `net`, `net/http`, and
`github.com/zsrv/goscape-singleplayer/internal/inproc`.

`--world-port` stays meaningful in both modes: Task 12 binds it as the
fabric's routing key, and `launch.Options.WorldPort` passes the same number to
`OpenSocket`, so the two always agree.

- [ ] **Step 5: Build and vet**

```bash
CGO_ENABLED=1 go build ./... && go vet ./... && gofmt -l .
```

Expected: clean.

- [ ] **Step 6: Commit**

```bash
git add internal/server/server.go internal/server/server_test.go cmd/goscape-singleplayer/main.go
GIT_AUTHOR_DATE="2026-09-08T00:00:00+0000" GIT_COMMITTER_DATE="2026-09-08T00:00:00+0000" \
  git commit --no-gpg-sign -m "feat: run client and server over in-process transports by default

The binary now opens no sockets: the client reaches world and ondemand
through the fabric, and world reaches login and friends the same way.
WaitReady takes the caller's HTTP client so the readiness probe rides the
same transport. --expose-tcp restores the loopback ports for debugging.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 14: singleplayer — prove nothing is listening

**Files:**
- Modify: `internal/server/server_test.go`

**Interfaces:**
- Consumes: everything above.
- Produces: `TestServerBootsInProcess` — the regression guard for this plan.

- [ ] **Step 1: Write the failing test**

Append to `internal/server/server_test.go`:

```go
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
```

Imports to add: `net`, `net/http`, `strconv`, and the `inproc` package.

**Caveat for the reviewer:** this test fails if an unrelated process happens
to hold 8080. If that is a problem on the runner, tighten it to assert only on
43594, 2004 and 2005, and note why.

- [ ] **Step 2: Run and confirm it fails**

```bash
go test ./internal/server/ -run TestServerBootsInProcess -v
```

Expected: FAIL before Tasks 12–13 land; PASS after.

- [ ] **Step 3: Run the whole suite**

```bash
go test ./... 2>&1 | tail -20
```

Expected: all PASS. `TestServerBootsReadyAndStops` still covers the TCP path;
`TestServerBootsInProcess` covers the new default.

- [ ] **Step 4: Commit**

```bash
gofmt -l . && go vet ./...
git add internal/server/server_test.go
GIT_AUTHOR_DATE="2026-09-08T00:00:00+0000" GIT_COMMITTER_DATE="2026-09-08T00:00:00+0000" \
  git commit --no-gpg-sign -m "test: assert the in-process build binds no ports

Boots the real module stack on the fabric, reaches readiness over
in-process HTTP, then asserts 43594, 8080, 2004 and 2005 are all refused.
This is the regression guard for the whole change.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 15: End-to-end acceptance, then land the pins

**Files:**
- Modify: `README.md`, `CONTRIBUTING.md` (document `--expose-tcp`)
- Modify: `go.mod`, `go.sum` (pin the pushed upstream SHAs)

**Interfaces:**
- Consumes: every prior task.
- Produces: a committed build with no `replace` block.

- [ ] **Step 1: Run the real game and watch for sockets**

```bash
CGO_ENABLED=1 go build -o goscape-singleplayer ./cmd/goscape-singleplayer
./goscape-singleplayer --cache-dir ../goscape/data/pack &
sleep 20 && ss -ltnp 2>/dev/null | grep goscape-singleplayer || echo "NO LISTENERS — expected"
```

Then, in the window: log in, walk a few tiles, confirm music plays, and close
the window.

Acceptance criteria, all required:
- The login screen appears and a login succeeds.
- The character moves.
- **Music plays** — this is the end-to-end proof that the SoundFont fetch
  traversed in-process HTTP.
- `ss -ltnp` shows nothing bound for the process at any point.
- After the window closes, the process exits 0 and the player save exists
  under `<data-dir>/players/`.

- [ ] **Step 2: Verify the escape hatch still works**

```bash
./goscape-singleplayer --cache-dir ../goscape/data/pack --expose-tcp &
sleep 20 && ss -ltnp 2>/dev/null | grep -E '43594|8080|2004|2005'
```

Expected: all four bound, and the game plays exactly as before.

- [ ] **Step 3: Document the flag**

Add to `README.md`'s flag list:

```markdown
`--expose-tcp` — bind the loopback ports (43594 world, 8080 ondemand, 2004
login, 2005 friends) instead of running everything in-process. Off by
default: the binary normally opens no sockets at all. Turn it on to attach a
second client, run `tcpdump`, or `curl` the ondemand endpoints.
```

- [ ] **Step 4: Push upstream and pin the SHAs**

```bash
cd ../goscape && git push origin rev-274 && git rev-parse HEAD
cd ../goscape-client && git push origin rev-274 && git rev-parse HEAD
```

Then, in this repo, remove the local `replace` block from `go.mod` and pin:

```bash
cd ../goscape-singleplayer
go get github.com/zsrv/goscape@<goscape-sha> github.com/zsrv/goscape-client@<client-sha>
go mod tidy
```

- [ ] **Step 5: Verify the pinned build from a clean module cache**

```bash
CGO_ENABLED=1 go build ./... && go test ./... 2>&1 | tail -10
```

Expected: PASS with no `replace` block present. Confirm with
`grep -c '^replace' go.mod` returning 0.

- [ ] **Step 6: Commit**

```bash
git add go.mod go.sum README.md
GIT_AUTHOR_DATE="2026-09-08T00:00:00+0000" GIT_COMMITTER_DATE="2026-09-08T00:00:00+0000" \
  git commit --no-gpg-sign -m "build: pin the upstreams that support in-process transports

goscape <goscape-sha> adds the optional listener and dialer fields;
goscape-client <client-sha> adds the in-process socket and HTTP hooks.
Documents --expose-tcp, which restores the loopback ports for debugging.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

## Not in this plan

- **Backports** to rev-254, rev-245.2, rev-244 and rev-225. A separate plan,
  written once this shape is proven on rev-274. The upstream changes are
  mechanical; the client-side seams sit in a per-revision port whose
  surrounding code differs between branches, so they are not cherry-picks.
- **Buffer-size tuning.** The constants in Task 11 are reasoned, not
  measured. If the 8 MB test or the manual run shows stalls, revisit them.
- **Removing the port flags.** `--world-port` and friends stay, meaningful
  under `--expose-tcp`.
