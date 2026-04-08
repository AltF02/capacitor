# Capacitor

A leaky-bucket rate limiter for Go, backed by [Valkey](https://valkey.io). Atomic bucket logic runs server-side via a Lua script, making it safe for distributed deployments. Ships with drop-in `net/http` middleware.

## Features

- Atomic leaky-bucket algorithm executed in a single Valkey round-trip
- Standard `func(http.Handler) http.Handler` middleware — works with `http.ServeMux`, chi, gorilla/mux, and any `http.Handler`-based router
- Configurable key extraction (IP, header, custom function)
- [IETF RateLimit header fields](https://datatracker.ietf.org/doc/draft-ietf-httpapi-ratelimit-headers/) on every response
- Fallback strategy when Valkey is unreachable (fail-open or fail-closed)
- Per-profile rate limits with configurable request-to-profile mapping
- Optional structured logging (`log/slog`) and metrics collection

## Installation

```sh
go get codeberg.org/matthew/capacitor
```

Requires Go 1.22+ and a running Valkey (or Redis 7+) instance.

## Quick Start

```go
package main

import (
	"log"
	"net/http"
	"time"

	"github.com/valkey-io/valkey-go"
	"codeberg.org/matthew/capacitor"
)

func main() {
	client, err := valkey.NewClient(valkey.ClientOption{
		InitAddress: []string{"localhost:6379"},
	})
	if err != nil {
		log.Fatal(err)
	}

	limiter := capacitor.New(client, capacitor.Config{
		Capacity:  10,
		LeakRate:  1, // 1 token per second
		Timeout:   500 * time.Millisecond,
	})
	defer limiter.Close()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /hello", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("Hello, world!\n"))
	})

	rl := capacitor.NewMiddleware(limiter)

	log.Println("listening on :8080")
	http.ListenAndServe(":8080", rl(mux))
}
```

## Configuration

| Field | Type | Description |
|---|---|---|
| `KeyPrefix` | `string` | Prefix for Valkey keys (e.g. `"capacitor"` → `capacitor:uid:<uid>`) |
| `Capacity` | `int64` | Maximum tokens in the bucket |
| `LeakRate` | `float64` | Tokens drained per second |
| `Timeout` | `time.Duration` | Per-call Valkey timeout |

## Middleware Options

### `WithKeyFunc`

Controls how the rate-limit key is derived from each request. Defaults to `KeyFromRemoteIP`.

```go
// Rate-limit by API key header.
rl := capacitor.NewMiddleware(limiter,
	capacitor.WithKeyFunc(capacitor.KeyFromHeader("X-API-Key")),
)
```

Built-in key functions:

| Function | Key source |
|---|---|
| `KeyFromRemoteIP` | Client IP from `RemoteAddr` (default) |
| `KeyFromHeader(name)` | Value of the given HTTP header |

You can provide any `func(*http.Request) string`. Return an empty string to skip rate limiting for that request.

### `WithDenyHandler`

Replaces the default plain-text 429 response.

```go
rl := capacitor.NewMiddleware(limiter,
	capacitor.WithDenyHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":"rate limited"}`))
	})),
)
```

## Limiter Options

Pass these to `capacitor.New`:

| Option | Description |
|---|---|
| `WithLogger(logger)` | Structured logger (`*slog.Logger`) |
| `WithFallback(strategy)` | `FallbackFailOpen` (default) or `FallbackFailClosed` |
| `WithMetrics(collector)` | Optional `MetricsCollector` implementation |

## Response Headers

Every response includes standard rate-limit headers:

| Header | Description |
|---|---|
| `RateLimit-Limit` | Bucket capacity |
| `RateLimit-Remaining` | Tokens remaining |
| `RateLimit-Reset` | Seconds until a token becomes available (denied requests only) |
| `Retry-After` | Same value as `RateLimit-Reset` (denied requests only) |

## Per-Profile Rate Limits

Use `WithProfiles` and `WithClassifier` to apply different rate limits based on an arbitrary per-request categorization (plan, tier, user type, etc.):

```go
profiles := capacitor.ProfileConfig{
    "basic": {
        Capacity:  10,
        LeakRate:  1,
        KeyPrefix: "capacitor",
        Timeout:   50 * time.Millisecond,
    },
    "premium": {
        Capacity:  100,
        LeakRate:  10,
        KeyPrefix: "capacitor",
        Timeout:   50 * time.Millisecond,
    },
}

rl := capacitor.NewMiddleware(limiter,
    capacitor.WithProfiles(profiles),
    capacitor.WithClassifier(func(r *http.Request) string {
        return r.Header.Get("X-Plan") // e.g. "basic" or "premium"
    }),
)
```

- Each profile creates an independent limiter sharing the same Valkey client
- If the classifier returns `""` or a name not in `ProfileConfig`, the default limiter is used
- Key prefixes are auto-namespaced per profile (`capacitor:profile:premium:uid:<uid>`) to prevent collisions
- The default limiter keeps its original key format (`capacitor:uid:<uid>`) — no migration needed
- Omit `WithProfiles` entirely for single-global-limit behavior

## Direct Usage (Without Middleware)

You can call the limiter directly for non-HTTP use cases such as background workers or gRPC interceptors:

```go
result, err := limiter.Attempt(ctx, "user:42")
if err != nil {
	// Valkey unreachable — result contains the fallback decision.
	log.Println("fallback used:", err)
}

if !result.Allowed {
	log.Printf("denied, retry after %s\n", result.RetryAfter)
}
```

## Health Check

```go
if err := limiter.HealthCheck(ctx); err != nil {
	log.Fatal("valkey unreachable:", err)
}
```

## License

[EUPL-1.2](LICENSE)
