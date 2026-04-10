# AGENTS.md

## Project Overview

**Capacitor** is a Go library implementing multiple rate-limiting algorithms backed by Valkey (Redis-compatible). All bucket logic runs atomically server-side via Lua scripts. Ships as a library with drop-in `net/http` middleware.

The root package defines the `Capacitor` interface (similar to `hash.Hash` in stdlib); algorithm implementations live in sub-packages and return `capacitor.Capacitor`.

- **Module**: `codeberg.org/matthew/capacitor`
- **Go version**: 1.25.5 (toolchain via Nix flake using `go_1_26`)
- **License**: EUPL-1.2
- **CI**: Woodpecker CI (`.woodpecker/go-test.yaml`)

## Commands

### Build

```sh
go build ./...
```

### Test

```sh
go test ./...
go test -v ./...                       # verbose
go test -run TestAttempt ./...         # single test
go test -run TestAttempt/FailOpen ...  # subtest
```

### Dev Environment (Nix)

The project uses a Nix flake with direnv integration (`.envrc`). Entering the directory automatically provides:

- `go_1_26`, `clang` (build tools)
- `gopls`, `delve` (dev tools)

```sh
nix develop          # manual shell entry
direnv allow         # one-time direnv setup
```

### CI Pipeline

Woodpecker CI runs on every push:

```sh
nix develop -c go test -v ./...
```

## Code Organization

```
capacitor/                    # root package: interface, Result, Options, middleware
├── capacitor.go              # Capacitor interface, Result, FallbackStrategy, FallbackResult(), Options, Option, With*
├── metrics.go                # MetricsCollector interface
├── middleware.go             # NewMiddleware, ProfileConfig, KeyFunc, ClassifyFunc, writeHeaders
├── capacitor_test.go         # Attempt tests via bucket/leaky (interface-level)
├── middleware_test.go        # HTTP middleware tests
├── metrics_test.go           # type alias for testutil.MetricsMock
│
├── bucket/
│   ├── leaky/                # leaky-bucket algorithm (HASH: level + last_leak) — package leaky
│   │   ├── leakybucket.go
│   │   ├── leakybucket_test.go
│   │   └── script.lua
│   └── token/                # token-bucket algorithm (HASH: tokens + last_refill) — package token
│       ├── tokenbucket.go
│       ├── tokenbucket_test.go
│       └── script.lua
│
├── fixedwindow/              # fixed-window counter (INCR + EXPIRE + PTTL) — package fixedwindow
│   ├── fixedwindow.go
│   ├── fixedwindow_test.go
│   └── script.lua
│
├── slidingwindow/
│   ├── counter/              # sliding-window counter (STRING x2, cluster hash tags) — package counter
│   │   ├── slidingwindowcounter.go
│   │   ├── slidingwindowcounter_test.go
│   │   └── script.lua
│   └── timelog/              # sliding-window log (SORTED SET) — package timelog
│       ├── slidingwindowlog.go
│       ├── slidingwindowlog_test.go
│       └── script.lua
│
└── internal/
    ├── ratelimit/            # shared rate-limit helpers
    └── testutil/             # shared test helpers (Btoi, MetricsMock)
        └── testutil.go
```

## Architecture & Key Patterns

### Capacitor Interface

Root package defines `Capacitor` as an interface; sub-packages implement and return it. This is the stdlib `hash.Hash` pattern: no circular dependencies.

```go
type Capacitor interface {
    Attempt(ctx context.Context, uid string) (Result, error)
    HealthCheck(ctx context.Context) error
    Close()
}
```

### Functional Options

Cross-cutting options are defined once in the root package:

- **Root options** (`Option` = `func(*Options)`): `WithLogger`, `WithFallback`, `WithMetrics`
- **Middleware options** (`MiddlewareOption` = `func(*middleware)`): `WithKeyFunc`, `WithDenyHandler`, `WithProfiles`, `WithClassifier`

### Lua Script Execution

Each algorithm defines a Lua script via `valkey.NewLuaScript()`. Scripts execute atomically in a single Valkey round-trip. All scripts return `{allowed, remaining, retry_after}` (3 values).

**Important**: The `now` timestamp passed to scripts must be in seconds (not milliseconds). The Go code converts via `float64(time.Now().UnixMilli()) / 1000.0`.

### Config Validation

All `New()` functions validate their config and panic on invalid values (zero/negative capacity, rate, window, timeout). This follows the Go convention of panicking for programmer errors in constructors.

### Fallback Strategy

When Valkey is unreachable, `Attempt()` returns a degraded result via `capacitor.FallbackResult()` and also returns the underlying error. Callers get both the fallback decision and the error.

### Middleware Behavior

- Returns standard `func(http.Handler) http.Handler` signature
- Empty key from `KeyFunc` skips rate limiting (passes request through)
- Sets IETF `RateLimit-*` headers on every response
- `Retry-After` and `RateLimit-Reset` only set on denied requests
- Per-profile routing via `ClassifyFunc`: selects a `Capacitor` instance by profile name
- Unknown or empty profile falls back to the default limiter
- `ProfileConfig` is `map[string]Capacitor`: users construct limiters and pass them in
- `Result.writeHeaders` method sets IETF rate-limit headers

### Compile-Time Interface Checks

Each sub-package includes a compile-time assertion:

```go
var _ capacitor.Capacitor = (*limiter)(nil)
```

## Naming Conventions & Style

- **Exported types**: `PascalCase`: `Capacitor`, `Result`, `FallbackStrategy`, `KeyFunc`, `ClassifyFunc`, `ProfileConfig`, `MetricsCollector`
- **Option constructors**: `With*` prefix: `WithLogger`, `WithFallback`, `WithMetrics`, `WithKeyFunc`, `WithDenyHandler`, `WithProfiles`, `WithClassifier`
- **Sentinel errors**: `Err*` prefix as package-level `var`: `ErrEmptyUID`, `ErrEvalResponse`
- **Constants**: iota enums: `FallbackFailOpen`, `FallbackFailClosed`
- **Default key prefixes**: `capacitor:<name>` pattern: `capacitor:leaky`, `capacitor:fixedwin`, `capacitor:token`, `capacitor:swcounter`, `capacitor:swlog`
- **Test package**: External test package (`package xxx_test`): tests import the library as a consumer would
- **Test structure**: Table-driven tests using `map[string]struct{}` with descriptive string keys
- **Assertions**: `github.com/google/go-cmp/cmp` for struct diffs, manual comparisons for simple values
- **Mocking**: `valkey-go/mock` with `go.uber.org/mock/gomock` for Valkey client mocks

## Code Style Guidelines

### Imports

Standard library first, third-party after, with a blank line between groups:

```go
import (
    "context"
    "errors"
    "fmt"
    "log/slog"
    "time"

    "github.com/google/go-cmp/cmp"
    "github.com/valkey-io/valkey-go/mock"
    "go.uber.org/mock/gomock"
)
```

### Error Handling

- Use `errors.Is()` to check sentinel errors: `if errors.Is(err, ErrEmptyUID) { ... }`
- Wrap errors with context using `fmt.Errorf("capacitor: leakybucket: eval: %w", err)`
- Return early on errors to avoid deep nesting
- Log errors at the call site before returning: `l.opts.Logger.Error("valkey eval failed", "error", err, "uid", uid)`

### Context

- Always accept `context.Context` as the first parameter in public methods
- Use `context.WithTimeout` for operations with deadlines
- Always `defer cancel()` when using `context.WithTimeout` or `context.WithCancel`

### Receiver Design

- Use pointer receivers (`*T`) for mutable objects and when methods need to mutate state
- Use value receivers (`T`) only for immutable data or small types where copy is cheap
- Consistency matters more than specific choice: if one method needs a pointer receiver, all should

### Short Variable Declarations

Prefer `:=` for local variables when type is obvious from the right side:

```go
ctrl := gomock.NewController(t)
client := mock.NewClient(ctrl)
```

Use `var` only for package-level variables or when explicit type is needed.

### Formatting

- Tab indentation, no tabs in output
- No blank lines between consecutive variable declarations
- Blank line between groups of related declarations
- 80-120 character line length typical
- Always handle the error (no `_` ignored errors unless explicitly intentional)

## Testing Approach

All tests use mocked Valkey clients: no real Valkey instance needed.

### Mock Pattern

```go
ctrl := gomock.NewController(t)
client := mock.NewClient(ctrl)

client.EXPECT().
    Do(gomock.Any(), gomock.Any()).
    Return(mock.Result(mock.ValkeyArray(
        mock.ValkeyInt64(testutil.Btoi(allowed)),
        mock.ValkeyInt64(int64(remaining)),
        mock.ValkeyInt64(int64(retryAfter)),
    )))

lim := leaky.New(client, cfg)
```

### Shared Test Helpers

`internal/testutil/testutil.go` provides:

- `Btoi(bool) int64`: converts bool to int64 using `unsafe.Pointer` (test-only)
- `MetricsMock`: implements `MetricsCollector`, records calls for assertion

## Gotchas

1. **Time units in Lua scripts**: All Lua scripts expect `now` in seconds. Keep rate/window parameters in the same unit or the math breaks.
2. **`Attempt()` returns both result and error on Valkey failure**: The result contains the fallback decision; don't discard it when `err != nil`.
3. **`unsafe` in tests**: The `Btoi` helper uses `unsafe.Pointer`: this is intentional and test-only.
4. **Go 1.25.5 / 1.26**: The project uses a very recent Go version provided via Nix. Ensure your toolchain matches.
5. **No `go generate`**: Mock generation is handled by the upstream `valkey-go/mock` package; there are no local `go:generate` directives.
6. **Lua scripts return 3 values**: All algorithms return `{allowed, remaining, retry_after}`. If adding a new algorithm, follow this convention.
7. **Cluster hash tags in slidingwindow/counter**: Keys use `{baseKey}` pattern to ensure both windows hash to the same Redis Cluster slot.
8. **Config validation panics**: `New()` panics on invalid config (zero/negative values). This is intentional: these are programmer errors.
