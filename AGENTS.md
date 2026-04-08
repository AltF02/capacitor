# AGENTS.md

## Project Overview

**Capacitor** is a Go library implementing a leaky-bucket rate limiter backed by Valkey (Redis-compatible). Bucket logic runs atomically server-side via a Lua script. Ships as a library with drop-in `net/http` middleware.

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

This is a single-package Go library (`package capacitor`). All source lives at the repository root — no subdirectories for source code.

| File | Purpose |
|---|---|
| `capacitor.go` | Core `Capacitor` struct, `New()` constructor, `Attempt()` method, `Result` type, options (`WithLogger`), package comment, sentinel errors |
| `config.go` | `Config` struct, `DefaultConfig()`, `ProfileConfig` type, field documentation |
| `bucket.go` | Lua script for atomic leaky-bucket logic executed server-side in Valkey |
| `fallback.go` | `FallbackStrategy` enum (`FallbackFailOpen`, `FallbackFailClosed`), `WithFallback` option |
| `metrics.go` | `MetricsCollector` interface and `WithMetrics` option |
| `middleware.go` | `net/http` middleware, `KeyFunc`/`ProfileFunc` types, built-in key extractors (`KeyFromRemoteIP`, `KeyFromHeader`), per-profile routing (`WithProfiles`, `WithProfileFunc`) |

### Test Files

| File | Covers |
|---|---|
| `capacitor_test.go` | `Attempt()` — allowed/denied/empty-uid, fallback strategies, metrics recording |
| `middleware_test.go` | HTTP middleware — key extraction, header writing, deny handlers, skip-on-empty-key, per-profile routing |
| `metrics_test.go` | `metricsMock` helper implementing `MetricsCollector` for tests |

## Architecture & Key Patterns

### Functional Options

Both the limiter and middleware use the functional options pattern:

- **Limiter options** (`Option` = `func(*Capacitor)`): `WithLogger`, `WithFallback`, `WithMetrics`
- **Middleware options** (`MiddlewareOption` = `func(*mw)`): `WithKeyFunc`, `WithDenyHandler`, `WithProfiles`, `WithProfileFunc`

### Lua Script Execution

The leaky-bucket algorithm runs as a Lua script via `valkey.NewLuaScript()`. The script is defined in `bucket.go` and executed atomically in a single Valkey round-trip. It uses `HSET`/`HGETALL` on a hash key with fields `level` and `last_leak`.

**Important**: The `now` timestamp passed to the script must be in seconds (not milliseconds) to match the `leak_rate` unit. The Go code converts `UnixMilli` to seconds via `float64(time.Now().UnixMilli()) / 1000.0`.

### Fallback Strategy

When Valkey is unreachable, `Attempt()` returns a degraded result based on the configured strategy and also returns the underlying error. Callers get both the fallback decision and the error.

### Middleware Behavior

- Returns standard `func(http.Handler) http.Handler` signature
- Empty key from `KeyFunc` skips rate limiting (passes request through)
- Sets IETF `RateLimit-*` headers on every response
- `Retry-After` and `RateLimit-Reset` only set on denied requests
- Per-profile routing via `ProfileFunc`: selects a `Capacitor` instance by profile name
- Unknown or empty profile falls back to the default limiter
- Profile key prefixes are auto-namespaced (`:profile:<name>`), default keeps original format

## Naming Conventions & Style

- **Exported types**: `PascalCase` — `Capacitor`, `Config`, `Result`, `FallbackStrategy`, `KeyFunc`, `ProfileFunc`, `ProfileConfig`, `MetricsCollector`
- **Option constructors**: `With*` prefix — `WithLogger`, `WithFallback`, `WithMetrics`, `WithKeyFunc`, `WithDenyHandler`, `WithProfiles`, `WithProfileFunc`
- **Sentinel errors**: `Err*` prefix as package-level `var` — `ErrEmptyUID`, `ErrEvalResponse`
- **Constants**: iota enums — `FallbackFailOpen`, `FallbackFailClosed`
- **Test package**: External test package (`package capacitor_test`) — tests import the library as a consumer would
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
    "math"
    "time"

    "github.com/google/go-cmp/cmp"
    "github.com/valkey-io/valkey-go/mock"
    "go.uber.org/mock/gomock"
)
```

### Error Handling

- Use `errors.Is()` to check sentinel errors: `if errors.Is(err, ErrEmptyUID) { ... }`
- Wrap errors with context using `fmt.Errorf("capacitor: attempt: %w", err)`
- Return early on errors to avoid deep nesting
- Log errors at the call site before returning: `s.logger.Error("store attempt failed", "error", err, "uid", uid)`

### Context

- Always accept `context.Context` as the first parameter in public methods
- Use `context.WithTimeout` for operations with deadlines
- Always `defer cancel()` when using `context.WithTimeout` or `context.WithCancel`

### Receiver Design

- Use pointer receivers (`*T`) for mutable objects and when methods need to mutate state
- Use value receivers (`T`) only for immutable data or small types where copy is cheap
- Consistency matters more than specific choice — if one method needs a pointer receiver, all should

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

All tests use mocked Valkey clients — no real Valkey instance needed.

### Mock Pattern

```go
ctrl := gomock.NewController(t)
client := mock.NewClient(ctrl)

client.EXPECT().
    Do(gomock.Any(), gomock.Any()).
    Return(mock.Result(mock.ValkeyArray(
        mock.ValkeyInt64(btoi(allowed)),
        mock.ValkeyInt64(int64(remaining)),
    )))

cap := capacitor.New(client, cfg)
```

### Helper: `btoi`

Tests use an `unsafe.Pointer` trick to convert `bool` to `int64` without branching:

```go
func btoi(b bool) int64 {
    return int64(*(*byte)(unsafe.Pointer(&b)))
}
```

### Metrics Mock

`metrics_test.go` provides a `metricsMock` struct implementing `MetricsCollector` that records calls for assertion.

## Gotchas

1. **Time units in Lua script**: The Lua script expects `now` in seconds. If you change time handling, keep `leak_rate` and `now` in the same unit or the bucket math breaks.
2. **`Attempt()` returns both result and error on Valkey failure**: The result contains the fallback decision; don't discard it when `err != nil`.
3. **`unsafe` in tests**: The `btoi` helper uses `unsafe.Pointer` — this is intentional and test-only.
4. **Go 1.25.5 / 1.26**: The project uses a very recent Go version provided via Nix. Ensure your toolchain matches.
5. **No `go generate`**: Mock generation is handled by the upstream `valkey-go/mock` package; there are no local `go:generate` directives.
6. **Single package**: Everything is in the root package. Don't create subdirectories for new features — add files to the root.
