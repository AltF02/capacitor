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
go test -v ./...   # verbose
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
| `capacitor.go` | Core `Capacitor` struct, `New()` constructor, `Attempt()` method, `Result` type, options (`WithLogger`) |
| `config.go` | `Config` struct and `DefaultConfig()` |
| `bucket.go` | Lua script for atomic leaky-bucket logic executed server-side in Valkey |
| `fallback.go` | `FallbackStrategy` enum (`FallbackFailOpen`, `FallbackFailClosed`), `WithFallback` option |
| `metrics.go` | `MetricsCollector` interface and `WithMetrics` option |
| `middleware.go` | `net/http` middleware, `KeyFunc` type, built-in key extractors (`KeyFromRemoteIP`, `KeyFromHeader`) |

### Test Files

| File | Covers |
|---|---|
| `capacitor_test.go` | `Attempt()` — allowed/denied/empty-uid, fallback strategies, metrics recording |
| `middleware_test.go` | HTTP middleware — key extraction, header writing, deny handlers, skip-on-empty-key |
| `metrics_test.go` | `metricsMock` helper implementing `MetricsCollector` for tests |

## Architecture & Key Patterns

### Functional Options

Both the limiter and middleware use the functional options pattern:

- **Limiter options** (`Option` = `func(*Capacitor)`): `WithLogger`, `WithFallback`, `WithMetrics`
- **Middleware options** (`MiddlewareOption` = `func(*mw)`): `WithKeyFunc`, `WithDenyHandler`

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

## Naming Conventions & Style

- **Exported types**: `PascalCase` — `Capacitor`, `Config`, `Result`, `FallbackStrategy`, `KeyFunc`, `MetricsCollector`
- **Option constructors**: `With*` prefix — `WithLogger`, `WithFallback`, `WithMetrics`, `WithKeyFunc`, `WithDenyHandler`
- **Sentinel errors**: `Err*` prefix as package-level `var` — `ErrEmptyUID`, `ErrEvalResponse`
- **Constants**: iota enums — `FallbackFailOpen`, `FallbackFailClosed`
- **Test package**: External test package (`package capacitor_test`) — tests import the library as a consumer would
- **Test structure**: Table-driven tests using `map[string]struct{}` with descriptive string keys
- **Assertions**: `github.com/google/go-cmp/cmp` for struct diffs, manual comparisons for simple values
- **Mocking**: `valkey-go/mock` with `go.uber.org/mock/gomock` for Valkey client mocks

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
