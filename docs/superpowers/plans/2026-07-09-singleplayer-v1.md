# goscape-singleplayer v1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** One binary that boots the full goscape server stack in-process on loopback TCP and runs the goscape-client render loop against it, with graceful server shutdown on window close.

**Architecture:** Two behavior-preserving extractions in goscape-client (an exported exit hook in `clientextras`, and the `cmd/client` startup wiring moved to a new `pkg/jagex2/launch` package), plus a new `goscape-singleplayer` module whose `internal/server` package builds the goscape `app.Config` programmatically, runs `app.Run` on a goroutine, polls `/crc` for readiness, and stops it gracefully via `app.Stop`. Spec: `docs/superpowers/specs/2026-07-09-singleplayer-design.md`.

**Tech Stack:** Go 1.26, goscape `cmd/goscape/app` (dskit modules/services), goscape-client `pkg/jagex2/*` (GLFW/OpenGL — CGO), testify (already in goscape's tree).

## Global Constraints

- Three repos are involved. Tasks 1–2 commit to **goscape-client** on branch `rev-274` (`/home/owner/Code/github.com/zsrv/goscape-client`, currently on `rev-274`; untracked `audit-274/` is pre-existing — never `git add` it). Tasks 3–5 commit to **goscape-singleplayer** on branch `rev-274` (`/home/owner/Code/github.com/zsrv/goscape-singleplayer`). Do not touch the goscape server repo.
- Run `git status` before every commit and stage only the files this plan names.
- All commits: `git commit --no-gpg-sign`.
- All `go` invocations: prefix with `GOPATH="${TMPDIR:-/tmp}/go" GOCACHE="${TMPDIR:-/tmp}/go-cache"` (`$TMPDIR` can be empty when the sandbox is off — the `${TMPDIR:-/tmp}` form is mandatory).
- goscape-client and goscape-singleplayer builds need `CGO_ENABLED=1` (GLFW/OpenGL).
- Shell writes to both repos are outside the Bash sandbox allowlist — if a command fails with "Read-only file system", retry it with the sandbox disabled.
- Behavior preservation is a hard requirement in Tasks 1–2: the stock `cmd/client` binary must produce the identical startup sequence and output on the success path. Carry over the original comment blocks verbatim when moving code.
- Fidelity policy: these are Go-original standalone-launcher files (`cmd/client/main.go` is already documented as a Go-original interface), so restructuring them does not need a PORTING-EXCEPTION.

---

### Task 1: Exit hook in goscape-client (`clientextras.ExitFunc`)

**Files:**
- Modify: `/home/owner/Code/github.com/zsrv/goscape-client/pkg/jagex2/client/clientextras/clientextras.go`
- Modify: `/home/owner/Code/github.com/zsrv/goscape-client/pkg/jagex2/client/gameshell.go:1-33`
- Test: `/home/owner/Code/github.com/zsrv/goscape-client/pkg/jagex2/client/clientextras/clientextras_test.go` (create)

**Interfaces:**
- Consumes: nothing.
- Produces: `clientextras.ExitFunc func(code int)` (package var, default `os.Exit`). Task 2's launch loop and Task 5's singleplayer main assign/call it.

**Why `clientextras`, not the launch package:** `GameShell.Shutdown` lives in package `client`, which the launch package imports — a hook in `launch` would be an import cycle. `clientextras` is this codebase's designated cycle-breaker package (its package comment says exactly that). The spec's "exported hook" lands here; the spec file has been amended to match.

- [ ] **Step 1: Write the failing test**

Create `pkg/jagex2/client/clientextras/clientextras_test.go`:

```go
package clientextras

import (
	"os"
	"reflect"
	"testing"
)

// The default must remain a direct os.Exit so the stock client's shutdown
// behavior is unchanged; only embedders (e.g. goscape-singleplayer) replace it.
func TestExitFuncDefaultsToOSExit(t *testing.T) {
	if reflect.ValueOf(ExitFunc).Pointer() != reflect.ValueOf(os.Exit).Pointer() {
		t.Fatal("clientextras.ExitFunc default is not os.Exit")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /home/owner/Code/github.com/zsrv/goscape-client
GOPATH="${TMPDIR:-/tmp}/go" GOCACHE="${TMPDIR:-/tmp}/go-cache" CGO_ENABLED=1 go test ./pkg/jagex2/client/clientextras/ -run TestExitFuncDefaultsToOSExit -v
```

Expected: FAIL to build with `undefined: ExitFunc`.

- [ ] **Step 3: Add the hook**

In `clientextras.go`: add `import "os"` (the file currently has no imports) and append at the end of the file:

```go
// ExitFunc terminates the process and defaults to os.Exit. GameShell.Shutdown
// and the standalone launch loop exit through it. Embedders that co-host other
// subsystems in the process (e.g. goscape-singleplayer's in-process server)
// replace it at startup — before the client runs — to perform a graceful
// shutdown of those subsystems first. Lives here rather than in pkg/jagex2/launch
// because package client (GameShell) must reach it without an import cycle.
var ExitFunc func(code int) = os.Exit
```

- [ ] **Step 4: Run test to verify it passes**

Same command as Step 2. Expected: PASS.

- [ ] **Step 5: Route GameShell.Shutdown through the hook**

In `gameshell.go`, the current tail of `Shutdown` (lines 27–33) is:

```go
	time.Sleep(1 * time.Second)
	// os.Exit halts the Go program cleanly on wasm too (handled by the
	// wasm_exec.js exit callback). Reviewed for the browser and intentionally
	// unchanged: DestroyEvent only fires on tab/canvas teardown, when the page
	// is going away regardless, so the best-effort Unload above is sufficient.
	os.Exit(0)
}
```

Replace the last two lines of the function body with:

```go
	// os.Exit (the ExitFunc default) halts the Go program cleanly on wasm too
	// (handled by the wasm_exec.js exit callback). Reviewed for the browser and
	// intentionally unchanged: DestroyEvent only fires on tab/canvas teardown,
	// when the page is going away regardless, so the best-effort Unload above
	// is sufficient. The indirection through clientextras.ExitFunc lets an
	// embedder (goscape-singleplayer) stop its in-process server before exit.
	clientextras.ExitFunc(0)
}
```

Then fix imports: `os` was only used by line 32 (verified by grep), so remove `"os"` and add `"github.com/zsrv/goscape-client/pkg/jagex2/client/clientextras"` to the import block.

- [ ] **Step 6: Verify the whole module still builds and tests pass**

```bash
cd /home/owner/Code/github.com/zsrv/goscape-client
GOPATH="${TMPDIR:-/tmp}/go" GOCACHE="${TMPDIR:-/tmp}/go-cache" CGO_ENABLED=1 go build ./... && \
GOPATH="${TMPDIR:-/tmp}/go" GOCACHE="${TMPDIR:-/tmp}/go-cache" CGO_ENABLED=1 go test ./pkg/jagex2/client/... ./pkg/sign/...
```

Expected: build OK, tests PASS (no test currently drives Shutdown; this is a compile/regression gate).

- [ ] **Step 7: Commit**

```bash
cd /home/owner/Code/github.com/zsrv/goscape-client && git status --short
git add pkg/jagex2/client/clientextras/clientextras.go pkg/jagex2/client/clientextras/clientextras_test.go pkg/jagex2/client/gameshell.go
git commit --no-gpg-sign -m "feat(clientextras): ExitFunc hook so embedders can intercept process exit"
```

---

### Task 2: Extract `pkg/jagex2/launch` from `cmd/client`

**Files:**
- Create: `/home/owner/Code/github.com/zsrv/goscape-client/pkg/jagex2/launch/launch.go`
- Test: `/home/owner/Code/github.com/zsrv/goscape-client/pkg/jagex2/launch/launch_test.go`
- Modify: `/home/owner/Code/github.com/zsrv/goscape-client/cmd/client/main.go` (full rewrite shown below; `worldserver.go` and the `parseOndemandServer` file stay in `cmd/client` untouched)

**Interfaces:**
- Consumes: `clientextras.ExitFunc` (Task 1), existing `client`/`signlink`/`audio`/`platform`/`profiling` package APIs.
- Produces (used by Task 5):
  ```go
  package launch // github.com/zsrv/goscape-client/pkg/jagex2/launch

  type Options struct {
      NodeID          int
      StoreID         int
      LowMemory       bool
      Members         bool
      Host            string
      Transport       clientextras.TransportKind
      WorldPort       int
      WSPath          string
      OndemandBaseURL string
  }
  func Run(opts Options) // blocks for the life of the process
  ```

**Behavior note:** the startup banner moves inside `Run` (single source for both binaries). On the success path output order is identical. On the *flag-validation error* path the stock binary previously printed the banner before the error; now it prints only the error. This is the one accepted output difference (flags are a Go-original surface; no Java-parity impact).

- [ ] **Step 1: Write the failing test**

Create `pkg/jagex2/launch/launch_test.go`:

```go
package launch

import (
	"testing"

	"github.com/zsrv/goscape-client/pkg/jagex2/client"
	"github.com/zsrv/goscape-client/pkg/jagex2/client/clientextras"
	"github.com/zsrv/goscape-client/pkg/sign/signlink"
)

// configure writes package globals across client/signlink/clientextras; save
// and restore them so this test cannot leak state into other tests.
func TestConfigureAppliesOptions(t *testing.T) {
	savedStoreID := signlink.StoreID
	savedNodeID := client.NodeID
	savedLowMem := client.LowMemory
	savedMembers := client.MembersWorld
	savedHost := clientextras.Host
	savedTransport := clientextras.Transport
	savedPort := clientextras.WorldPort
	savedWSPath := clientextras.WSPath
	savedBase := clientextras.OndemandBaseURL
	t.Cleanup(func() {
		signlink.StoreID = savedStoreID
		client.NodeID = savedNodeID
		if savedLowMem {
			client.SetLowMem()
		} else {
			client.SetHighMem()
		}
		client.MembersWorld = savedMembers
		clientextras.Host = savedHost
		clientextras.Transport = savedTransport
		clientextras.WorldPort = savedPort
		clientextras.WSPath = savedWSPath
		clientextras.OndemandBaseURL = savedBase
	})

	configure(Options{
		NodeID:          12,
		StoreID:         33,
		LowMemory:       true,
		Members:         true,
		Host:            "127.0.0.1",
		Transport:       clientextras.TransportTCP,
		WorldPort:       40594,
		WSPath:          "",
		OndemandBaseURL: "http://127.0.0.1:40080",
	})

	if signlink.StoreID != 33 {
		t.Errorf("signlink.StoreID = %d, want 33", signlink.StoreID)
	}
	if client.NodeID != 12 {
		t.Errorf("client.NodeID = %d, want 12", client.NodeID)
	}
	if !client.LowMemory {
		t.Error("client.LowMemory = false, want true (SetLowMem not called)")
	}
	if !client.MembersWorld {
		t.Error("client.MembersWorld = false, want true")
	}
	if clientextras.Host != "127.0.0.1" {
		t.Errorf("clientextras.Host = %q", clientextras.Host)
	}
	if clientextras.Transport != clientextras.TransportTCP {
		t.Errorf("clientextras.Transport = %v", clientextras.Transport)
	}
	if clientextras.WorldPort != 40594 {
		t.Errorf("clientextras.WorldPort = %d", clientextras.WorldPort)
	}
	if clientextras.WSPath != "" {
		t.Errorf("clientextras.WSPath = %q", clientextras.WSPath)
	}
	if clientextras.OndemandBaseURL != "http://127.0.0.1:40080" {
		t.Errorf("clientextras.OndemandBaseURL = %q", clientextras.OndemandBaseURL)
	}

	// HighMem path too — SetHighMem must be called when LowMemory is false.
	configure(Options{Host: "127.0.0.1", OndemandBaseURL: "http://127.0.0.1:40080"})
	if client.LowMemory {
		t.Error("client.LowMemory = true after configure with LowMemory:false")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /home/owner/Code/github.com/zsrv/goscape-client
GOPATH="${TMPDIR:-/tmp}/go" GOCACHE="${TMPDIR:-/tmp}/go-cache" CGO_ENABLED=1 go test ./pkg/jagex2/launch/ -v
```

Expected: FAIL to build (package `launch` does not exist yet).

- [ ] **Step 3: Create `pkg/jagex2/launch/launch.go`**

Move the post-flag wiring of `cmd/client/main.go:51-163` here. **Carry the original comment blocks verbatim** (banner/deviation comment from lines 48-50, StoreID comment 53-54, world-server comment 79-81, ondemand comment 92-93, ConfigureTransport comment 101-102, profiling comment 105-107, subsystems comment 110-114, audio comment 120-133, platform.Main comment 141-158) — they carry Java line references. Skeleton with the code exact and comment markers indicating what to carry:

```go
// Package launch wires up and runs the standalone game client: the startup
// sequence extracted from cmd/client so other binaries (goscape-singleplayer's
// combined server+client) can embed the client with programmatic options
// instead of flags. Run blocks for the life of the process and leaves through
// clientextras.ExitFunc.
package launch

import (
	"fmt"
	"strconv"
	"sync"

	"github.com/zsrv/goscape-client/pkg/jagex2/client"
	"github.com/zsrv/goscape-client/pkg/jagex2/client/clientextras"
	"github.com/zsrv/goscape-client/pkg/jagex2/platform"
	"github.com/zsrv/goscape-client/pkg/jagex2/sound/audio"
	"github.com/zsrv/goscape-client/pkg/profiling"
	"github.com/zsrv/goscape-client/pkg/sign/signlink"
)

// Options carries the startup configuration for Run. cmd/client builds it
// from flags (already validated); embedders build it directly. The zero value
// is not meaningful — every field mirrors a previously-mandatory flag.
type Options struct {
	NodeID          int                        // -node-id; server node id
	StoreID         int                        // -store-id; .file_store_<id> disk cache dir
	LowMemory       bool                       // -mem low
	Members         bool                       // -world-type members
	Host            string                     // bare hostname for GetHost/GetCodeBase/TCP dial
	Transport       clientextras.TransportKind // tcp/ws/wss
	WorldPort       int                        // game-server port (every transport)
	WSPath          string                     // ws/wss path; "" means "/"
	OndemandBaseURL string                     // scheme://host:port for cache/asset HTTP fetches
}

// configure applies Options to the package-global client/signlink/clientextras
// state. Split from Run so the assignment wiring is unit-testable without
// opening a window.
func configure(opts Options) {
	// [carry comment from main.go:53-54 — StoreID before first store use]
	signlink.StoreID = opts.StoreID

	client.NodeID = opts.NodeID

	if opts.LowMemory {
		client.SetLowMem()
	} else {
		client.SetHighMem()
	}

	client.MembersWorld = opts.Members

	// [carry comment from main.go:79-81 — Host/Transport/WorldPort/WSPath roles]
	clientextras.Host = opts.Host
	clientextras.Transport = opts.Transport
	clientextras.WorldPort = opts.WorldPort
	clientextras.WSPath = opts.WSPath

	// [carry comment from main.go:92-93 — OndemandBaseURL consumers]
	clientextras.OndemandBaseURL = opts.OndemandBaseURL
}

// Run configures the process-global client state, starts the background
// subsystems, and runs the game loop on the calling goroutine (which must be
// the main goroutine — platform.Main locks the OS thread on native builds).
// It never returns: the loop closure leaves through clientextras.ExitFunc.
func Run(opts Options) {
	// [carry comment from main.go:48-50 — banner/deviation note]
	fmt.Println("RS2 user client - release #" + strconv.Itoa(signlink.ClientVersion))

	configure(opts)

	// [carry comment from main.go:101-102]
	signlink.ConfigureTransport()

	// [carry comment from main.go:105-107]
	profiling.Start()

	// [carry comment from main.go:110-114, updating "calls os.Exit(0)" to
	// "calls clientextras.ExitFunc(0)"]
	var wg sync.WaitGroup
	wg.Go(func() {
		signlink.StartPriv()
	})
	wg.Go(func() {
		// [carry comment from main.go:120-133]
		if client.LowMemory {
			audio.DisableForLowMemory()
			return
		}
		audio.Start()
	})

	// [carry comment from main.go:141-158, updating "then os.Exit(0)" to
	// "then clientextras.ExitFunc(0)"]
	platform.Main(765, 503, "Jagex", func() {
		c := client.NewClient()
		c.RunShell()
		clientextras.ExitFunc(0)
	})
}
```

- [ ] **Step 4: Run test to verify it passes**

Same command as Step 2. Expected: PASS (both subtests of assignment wiring).

- [ ] **Step 5: Rewrite `cmd/client/main.go` as flags → Options → Run**

Keep the file's opening comment block (lines 20-27, the Java port-offset provenance) above the flag definitions. Full new file:

```go
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/zsrv/goscape-client/pkg/jagex2/launch"
	"github.com/zsrv/goscape-client/pkg/util/build"
)

func main() {
	// [carry comment from main.go:20-27 — flags vs Java positional args]
	nodeID := flag.Int("node-id", 10, "server node id")
	mem := flag.String("mem", "high", "memory mode: high|low")
	worldType := flag.String("world-type", "members", "world type: free|members")
	worldServer := flag.String("world-server", "tcp://127.0.0.1:43594",
		"game server as [tcp|ws|wss]://host:port")
	ondemandServer := flag.String("ondemand-server", "http://127.0.0.1:8080",
		"on-demand/cache server as [http|https]://host:port")
	showVersion := flag.Bool("version", false, "print build version information and exit")
	storeID := flag.Int("store-id", 32, "disk cache directory id (.file_store_<id>, clamped to 32-34)")
	flag.Parse()

	// [carry comment from main.go:39-42 — -version prints and exits headlessly]
	if *showVersion {
		fmt.Println(build.Info())
		return
	}

	opts := launch.Options{
		NodeID:  *nodeID,
		StoreID: *storeID,
	}

	switch *mem {
	case "high":
		opts.LowMemory = false
	case "low":
		opts.LowMemory = true
	default:
		fmt.Printf("invalid -mem %q (want high|low)\n", *mem)
		os.Exit(1)
	}

	switch *worldType {
	case "free":
		opts.Members = false
	case "members":
		opts.Members = true
	default:
		fmt.Printf("invalid -world-type %q (want free|members)\n", *worldType)
		os.Exit(1)
	}

	kind, host, port, path, err := parseWorldServer(*worldServer)
	if err != nil {
		fmt.Printf("invalid -world-server: %v\n", err)
		os.Exit(1)
	}
	opts.Transport = kind
	opts.Host = host
	opts.WorldPort = port
	opts.WSPath = path

	base, err := parseOndemandServer(*ondemandServer)
	if err != nil {
		fmt.Printf("invalid -ondemand-server: %v\n", err)
		os.Exit(1)
	}
	opts.OndemandBaseURL = base

	// launch.Run prints the release banner, applies opts to the process-global
	// client state, starts signlink/audio, and blocks in the game loop on this
	// (main) goroutine. It leaves via clientextras.ExitFunc.
	launch.Run(opts)
}
```

- [ ] **Step 6: Verify build, tests, and formatting across the module**

```bash
cd /home/owner/Code/github.com/zsrv/goscape-client
GOPATH="${TMPDIR:-/tmp}/go" GOCACHE="${TMPDIR:-/tmp}/go-cache" CGO_ENABLED=1 go build ./... && \
GOPATH="${TMPDIR:-/tmp}/go" GOCACHE="${TMPDIR:-/tmp}/go-cache" CGO_ENABLED=1 go test ./... && \
gofmt -l pkg/jagex2/launch cmd/client
```

Expected: build OK, all tests PASS, `gofmt -l` prints nothing.

- [ ] **Step 7: Verify the stock binary still behaves (headless paths)**

```bash
cd /home/owner/Code/github.com/zsrv/goscape-client
GOPATH="${TMPDIR:-/tmp}/go" GOCACHE="${TMPDIR:-/tmp}/go-cache" CGO_ENABLED=1 go run ./cmd/client -version
GOPATH="${TMPDIR:-/tmp}/go" GOCACHE="${TMPDIR:-/tmp}/go-cache" CGO_ENABLED=1 go run ./cmd/client -mem bogus; echo "exit=$?"
```

Expected: first prints build info and exits 0; second prints `invalid -mem "bogus" (want high|low)` and `exit=1`. (Full windowed run is covered by the end-of-project user smoke test.)

- [ ] **Step 8: Commit**

```bash
cd /home/owner/Code/github.com/zsrv/goscape-client && git status --short
git add pkg/jagex2/launch/launch.go pkg/jagex2/launch/launch_test.go cmd/client/main.go
git commit --no-gpg-sign -m "refactor(launch): extract embeddable client startup from cmd/client"
```

---

### Task 3: goscape-singleplayer module scaffold + config builder

**Files (all under `/home/owner/Code/github.com/zsrv/goscape-singleplayer`, branch `rev-274`):**
- Create: `go.mod`, `.gitignore`
- Create: `internal/server/config.go`
- Test: `internal/server/config_test.go`

**Interfaces:**
- Consumes: `github.com/zsrv/goscape/cmd/goscape/app` — `app.NewDefaultConfig() *app.Config`, `app.SingleBinary` (= `"all"`), `(*app.Config).Validate() error`, and the module config fields shown in the code below (verified against goscape rev-274 source).
- Produces (used by Tasks 4–5):
  ```go
  package server // github.com/zsrv/goscape-singleplayer/internal/server

  type Options struct {
      DataDir      string
      CacheDir     string
      WorldPort    int
      OndemandPort int
      LoginPort    int
      FriendsPort  int
  }
  func NewConfig(opts Options) (*app.Config, error)
  func CheckCache(cacheDir string) error
  ```

- [ ] **Step 1: Create `go.mod` and `.gitignore`**

`go.mod`:

```
module github.com/zsrv/goscape-singleplayer

go 1.26

require (
	github.com/zsrv/goscape v0.0.0
	github.com/zsrv/goscape-client v0.0.0
)

replace (
	github.com/zsrv/goscape => ../goscape
	github.com/zsrv/goscape-client => ../goscape-client
)
```

(`go mod tidy` in Step 5 normalizes the require versions and generates `go.sum`; the `replace` block makes the version strings inert.)

`.gitignore`:

```
/goscape-singleplayer
/data/
```

- [ ] **Step 2: Write the failing test**

Create `internal/server/config_test.go`:

```go
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
```

- [ ] **Step 3: Run test to verify it fails**

```bash
cd /home/owner/Code/github.com/zsrv/goscape-singleplayer
GOPATH="${TMPDIR:-/tmp}/go" GOCACHE="${TMPDIR:-/tmp}/go-cache" go test ./internal/server/ -v
```

Expected: FAIL to build (`config.go` doesn't exist; possibly missing go.sum first — if so run Step 5's tidy, then re-run and expect the compile failure).

- [ ] **Step 4: Create `internal/server/config.go`**

```go
// Package server boots the full goscape server stack (world, login, friends,
// ondemand, sqlite) inside the singleplayer process, on loopback listeners.
package server

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/zsrv/goscape/cmd/goscape/app"
)

// Options configures the embedded server stack. All listeners bind 127.0.0.1.
type Options struct {
	DataDir      string // world database, player saves
	CacheDir     string // packed game cache (goscape `make pack` output)
	WorldPort    int    // game TCP port the client connects to
	OndemandPort int    // cache/OnDemand HTTP port the client fetches from
	LoginPort    int    // internal login gRPC port
	FriendsPort  int    // internal friends gRPC port
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

	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// CheckCache fails fast with an actionable message when the packed cache is
// missing — the expected first-run mistake. The repo ships no cache; goscape's
// `make pack` produces one.
func CheckCache(cacheDir string) error {
	if _, err := os.Stat(filepath.Join(cacheDir, "main_file_cache.dat")); err != nil {
		return fmt.Errorf("no packed game cache at %s (main_file_cache.dat missing): "+
			"run `make pack` in the goscape repo or point --cache-dir at an existing pack: %w",
			cacheDir, err)
	}
	return nil
}
```

- [ ] **Step 5: Tidy and run tests to verify they pass**

```bash
cd /home/owner/Code/github.com/zsrv/goscape-singleplayer
GOPATH="${TMPDIR:-/tmp}/go" GOCACHE="${TMPDIR:-/tmp}/go-cache" go mod tidy && \
GOPATH="${TMPDIR:-/tmp}/go" GOCACHE="${TMPDIR:-/tmp}/go-cache" go test ./internal/server/ -v
```

Expected: tidy rewrites the require versions to `v0.0.0-00010101000000-000000000000` and writes `go.sum`; both tests PASS. (Sibling checkouts must be on `rev-274` — verify with `git -C ../goscape branch --show-current` and `git -C ../goscape-client branch --show-current` if anything looks off.)

- [ ] **Step 6: Commit**

```bash
cd /home/owner/Code/github.com/zsrv/goscape-singleplayer && git status --short
git add go.mod go.sum .gitignore internal/server/config.go internal/server/config_test.go
git commit --no-gpg-sign -m "feat(server): module scaffold + programmatic loopback stack config"
```

---

### Task 4: Server lifecycle — Start / WaitReady / Stop

**Files (goscape-singleplayer):**
- Create: `internal/server/server.go`
- Test: `internal/server/server_test.go`

**Interfaces:**
- Consumes: Task 3's `NewConfig`/`CheckCache`; goscape's `app.New(logger *slog.Logger, cfg app.Config) (*app.App, error)`, `(*app.App).Run() error` (blocks; installs the process SIGINT/SIGTERM handler — the desired owner in singleplayer), `(*app.App).Stop()` (panics if the app is not running), `pkg/util/log.NewLogger(level slog.Level, format string, w io.Writer, opts ...log.Option) (*slog.Logger, error)`.
- Produces (used by Task 5):
  ```go
  func NewLogger() (*slog.Logger, error)
  func Start(logger *slog.Logger, cfg *app.Config) (*Server, error)
  func (s *Server) Done() <-chan struct{}      // closed when app.Run returns
  func (s *Server) Err() error                 // valid only after Done is closed
  func (s *Server) WaitReady(ctx context.Context, ondemandPort int) error
  func (s *Server) Stop(timeout time.Duration) error
  ```

- [ ] **Step 1: Write the failing test**

Create `internal/server/server_test.go`:

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /home/owner/Code/github.com/zsrv/goscape-singleplayer
GOPATH="${TMPDIR:-/tmp}/go" GOCACHE="${TMPDIR:-/tmp}/go-cache" go test ./internal/server/ -run TestServerBootsReadyAndStops -v
```

Expected: FAIL to build (`Start`, `Server`, etc. undefined).

- [ ] **Step 3: Create `internal/server/server.go`**

```go
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
```

- [ ] **Step 4: Run the test to verify it passes (or legitimately skips)**

```bash
cd /home/owner/Code/github.com/zsrv/goscape-singleplayer
GOPATH="${TMPDIR:-/tmp}/go" GOCACHE="${TMPDIR:-/tmp}/go-cache" go test ./internal/server/ -run TestServerBootsReadyAndStops -v -timeout 120s
```

Expected: PASS if `../goscape/data/pack` holds a packed cache (boot logs are discarded; the test takes a few seconds), otherwise SKIP with the CheckCache message. If it skips, state that explicitly in the task report — do not claim the boot path was exercised.

- [ ] **Step 5: Run the whole package suite and vet**

```bash
cd /home/owner/Code/github.com/zsrv/goscape-singleplayer
GOPATH="${TMPDIR:-/tmp}/go" GOCACHE="${TMPDIR:-/tmp}/go-cache" go test ./... && \
GOPATH="${TMPDIR:-/tmp}/go" GOCACHE="${TMPDIR:-/tmp}/go-cache" go vet ./...
```

Expected: PASS / no vet findings.

- [ ] **Step 6: Commit**

```bash
cd /home/owner/Code/github.com/zsrv/goscape-singleplayer && git status --short
git add internal/server/server.go internal/server/server_test.go
git commit --no-gpg-sign -m "feat(server): in-process lifecycle — Start, /crc readiness, graceful Stop"
```

---

### Task 5: `cmd/goscape-singleplayer` main + README usage

**Files (goscape-singleplayer):**
- Create: `cmd/goscape-singleplayer/main.go`
- Modify: `README.md` (append usage section)

**Interfaces:**
- Consumes: Task 3's `server.Options`/`NewConfig`/`CheckCache`, Task 4's `NewLogger`/`Start`/`Done`/`Err`/`WaitReady`/`Stop`, Task 2's `launch.Options`/`launch.Run`, Task 1's `clientextras.ExitFunc`, `clientextras.TransportTCP`.
- Produces: the shippable binary.

- [ ] **Step 1: Create `cmd/goscape-singleplayer/main.go`**

```go
// Command goscape-singleplayer runs a complete singleplayer game: the full
// goscape server stack (world, login, friends, ondemand, sqlite) in-process
// on loopback TCP, and the goscape-client game window in the same process.
//
// Exit paths (all converge on exitOnce so the server stops exactly once):
//   - window close → clientextras.ExitFunc → graceful server Stop → exit 0
//   - SIGINT/SIGTERM → app's signal handler stops services → Done watcher exits
//   - server module failure → Done watcher logs and exits 1
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/zsrv/goscape-client/pkg/jagex2/client/clientextras"
	"github.com/zsrv/goscape-client/pkg/jagex2/launch"
	"github.com/zsrv/goscape-singleplayer/internal/server"
)

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}

func main() {
	dataDir := flag.String("data-dir", "./data", "directory for the world database and player saves")
	cacheDir := flag.String("cache-dir", "./data/pack", "packed game cache directory (goscape `make pack` output)")
	worldPort := flag.Int("world-port", 43594, "loopback game (world) TCP port")
	ondemandPort := flag.Int("ondemand-port", 8080, "loopback cache/OnDemand HTTP port")
	loginPort := flag.Int("login-port", 2004, "loopback login gRPC port (internal)")
	friendsPort := flag.Int("friends-port", 2005, "loopback friends gRPC port (internal)")
	mem := flag.String("mem", "high", "client memory mode: high|low")
	worldType := flag.String("world-type", "members", "world type: free|members")
	flag.Parse()

	var lowMemory bool
	switch *mem {
	case "high":
		lowMemory = false
	case "low":
		lowMemory = true
	default:
		fatalf("invalid -mem %q (want high|low)", *mem)
	}

	var members bool
	switch *worldType {
	case "free":
		members = false
	case "members":
		members = true
	default:
		fatalf("invalid -world-type %q (want free|members)", *worldType)
	}

	if err := server.CheckCache(*cacheDir); err != nil {
		fatalf("%v", err)
	}

	cfg, err := server.NewConfig(server.Options{
		DataDir:      *dataDir,
		CacheDir:     *cacheDir,
		WorldPort:    *worldPort,
		OndemandPort: *ondemandPort,
		LoginPort:    *loginPort,
		FriendsPort:  *friendsPort,
	})
	if err != nil {
		fatalf("server config: %v", err)
	}

	logger, err := server.NewLogger()
	if err != nil {
		fatalf("logger: %v", err)
	}

	srv, err := server.Start(logger, cfg)
	if err != nil {
		fatalf("server start: %v", err)
	}

	readyCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	err = srv.WaitReady(readyCtx, *ondemandPort)
	cancel()
	if err != nil {
		fatalf("server failed to become ready: %v", err)
	}

	var exitOnce sync.Once

	// Window close / client shutdown: stop the server first (player saves +
	// sqlite flush happen during service shutdown), then leave the process.
	clientextras.ExitFunc = func(code int) {
		exitOnce.Do(func() {
			if err := srv.Stop(10 * time.Second); err != nil {
				logger.Error("server shutdown", "err", err)
				if code == 0 {
					code = 1
				}
			}
			os.Exit(code)
		})
		// Another goroutine is already driving the exit; park until it does.
		select {}
	}

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

	launch.Run(launch.Options{
		NodeID:          10,
		StoreID:         32,
		LowMemory:       lowMemory,
		Members:         members,
		Host:            "127.0.0.1",
		Transport:       clientextras.TransportTCP,
		WorldPort:       *worldPort,
		WSPath:          "",
		OndemandBaseURL: fmt.Sprintf("http://127.0.0.1:%d", *ondemandPort),
	})
}
```

- [ ] **Step 2: Build everything and exercise the headless paths**

```bash
cd /home/owner/Code/github.com/zsrv/goscape-singleplayer
GOPATH="${TMPDIR:-/tmp}/go" GOCACHE="${TMPDIR:-/tmp}/go-cache" CGO_ENABLED=1 go build -o goscape-singleplayer ./cmd/goscape-singleplayer
./goscape-singleplayer --help
./goscape-singleplayer --cache-dir /nonexistent; echo "exit=$?"
```

Expected: build OK; `--help` lists the eight flags; the bad cache-dir run prints the `no packed game cache at /nonexistent ...` message and `exit=1` **without opening a window or binding any port**.

- [ ] **Step 3: Run the full test suite once more**

```bash
cd /home/owner/Code/github.com/zsrv/goscape-singleplayer
GOPATH="${TMPDIR:-/tmp}/go" GOCACHE="${TMPDIR:-/tmp}/go-cache" go test ./... && gofmt -l cmd internal
```

Expected: PASS (server boot test runs or skips per cache availability); `gofmt -l` prints nothing.

- [ ] **Step 4: Append usage to `README.md`**

Add after the "Dependency wiring" section:

```markdown
## Usage (rev-274)

Build (CGO required — GLFW/OpenGL):

    CGO_ENABLED=1 go build -o goscape-singleplayer ./cmd/goscape-singleplayer

You need a packed game cache. In the goscape repo, `make pack` produces one;
point `--cache-dir` at its output (default `./data/pack`).

    ./goscape-singleplayer --cache-dir ../goscape/data/pack

The world database and your character's saves live under `--data-dir`
(default `./data`). Accounts auto-register: type any username/password at the
login screen and the character is created on first login. Closing the window
shuts the server down gracefully (saves flush) before the process exits.

All listeners bind 127.0.0.1 only. Ports are adjustable if the defaults
collide: `--world-port 43594 --ondemand-port 8080 --login-port 2004
--friends-port 2005`.
```

- [ ] **Step 5: Commit**

```bash
cd /home/owner/Code/github.com/zsrv/goscape-singleplayer && git status --short
git add cmd/goscape-singleplayer/main.go README.md
git commit --no-gpg-sign -m "feat: goscape-singleplayer binary — embedded server + client over loopback"
```

- [ ] **Step 6: Hand off the windowed smoke test**

The final verification is user-launched (windowed GUI): build, run against a real packed cache, log in with a fresh username, walk around, close the window, confirm the server-shutdown log lines appear, relaunch, log in again, confirm the character persisted. Report readiness for this handoff; do not attempt to drive the GUI.

---

## Amendment 1 (2026-07-09): wordenc path knob — user-approved

Task 4's boot smoke test surfaced a real embedding defect: goscape's
`encfilter.Load()` (`pkg/wordenc/encfilter/encfilter.go:60`) reads the chat
word-filter jagfile from the hardcoded cwd-relative path `data/raw/wordenc`
(TS-faithful, `WordEnc.ts:35-37`; consumed at `modules/world/server.go:717`),
so `world.NewServer` fails when the singleplayer binary runs from any other
directory. The user approved a goscape-side config knob. The "do not touch
the goscape server repo" global constraint is lifted for exactly the Task 4a
change below.

### Task 4a (inserted before Task 4 resumes): goscape `world.wordenc_path`

**Files (goscape repo, branch rev-274):**
- Modify: `pkg/wordenc/encfilter/encfilter.go` — `func Load() (*Filter, error)`
  becomes `func Load(path string) (*Filter, error)`; the body uses `path`
  instead of the literal; doc comments updated to say the default path comes
  from world config while keeping the TS reference.
- Modify: `modules/world/config.go` — add field
  `WordEncPath string  \`yaml:"wordenc_path"\`` with flag registration
  `f.StringVar(&c.WordEncPath, "world.wordenc-path", "data/raw/wordenc", ...)`.
  Doc comment: Go-original embedding knob (same pattern as
  `rsa_private_key_path`); the default preserves the TS-faithful hardcoded
  relative path, resolved against the process working directory as before.
- Modify: `modules/world/server.go:717` — `encfilter.Load(cfg.WordEncPath)`.
- Modify: `examples/full-config-reference.yaml` — document `wordenc_path`
  at its default in the world section (the reference documents every option).
- Tests: adapt existing callers/tests of `Load()` (e.g.
  `modules/world/server_wordenc_test.go`, encfilter tests); add/extend a case
  proving a custom absolute path loads without chdir.
- Verify: `go build ./...` plus `go test ./pkg/wordenc/... ./modules/world/...`
  in goscape; behavior with the default value must be unchanged.
- Commit (goscape): `feat(world): configurable wordenc_path for embedders (default TS-faithful)`

### Task 4 (amended)

`internal/server/config.go`: `Options` gains `WordEncPath string` (empty ⇒
derive `filepath.Join(cacheDir, "..", "raw", "wordenc")` after CacheDir is
absolutized); `NewConfig` sets `cfg.World.WordEncPath` to the absolute path.
Add `CheckWordEnc(path string) error` (os.Stat with an actionable message —
the raw file sits next to the pack in goscape's layout; `--wordenc-path`
overrides). Extend `config_test.go`: derived default, explicit override, and
CheckWordEnc failure/success. `server.go`/`server_test.go` unchanged from the
original task text.

### Task 5 (amended)

Add flag `--wordenc-path` (default `""` = derived next to the cache); pass
through `server.Options.WordEncPath`; call `server.CheckWordEnc` on the
resolved path (from `cfg.World.WordEncPath`) right after `server.CheckCache`;
README usage mentions the wordenc file and the override flag.
