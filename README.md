# Capacitor

A rate-limiting library for Go, backed by [Valkey](https://valkey.io). Atomic limiting logic runs server-side via Lua scripts, making it safe for distributed deployments. Ships with drop-in `net/http` middleware.

## Features

- 5 rate-limiting algorithms: leaky bucket, fixed window, token bucket, sliding window counter, sliding window log
- All algorithms execute atomically in a single Valkey round-trip via Lua scripts
- Standard `func(http.Handler) http.Handler` middleware, works with any `http.Handler`-based router
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

## Algorithms

The algorithms and Lua scripts in Capacitor follow the patterns described in the [Redis rate limiting tutorial](https://redis.io/tutorials/howtos/ratelimiting/), which covers the tradeoffs between all five approaches in depth.

| Package | Algorithm | Best for | Valkey data structure | Accuracy |
|---|---|---|---|---|
| `bucket/leaky` | Leaky bucket (policing) | Strict no-burst, constant drain | HASH (level + last_leak) | Exact |
| `fixedwindow` | Fixed-window counter | Simple, low overhead | STRING (INCR + EXPIRE) | Approximate |
| `bucket/token` | Token bucket | Controlled bursts with steady average rate | HASH (tokens + last_refill) | Exact |
| `slidingwindow/counter` | Sliding-window counter | Near-exact accuracy with low memory | STRING x2 (weighted avg) | Near-exact |
| `slidingwindow/timelog` | Sliding-window log | True rolling window, exact counting | SORTED SET | Exact |

### Choosing an Algorithm

Start with the sliding window counter if you are unsure. It handles most API rate limiting scenarios well: low memory, near-exact accuracy, and no boundary bursts. It is the best default choice for distributed rate limiting.

- **Fixed window** counts requests in discrete time windows. Minimal Valkey overhead (one key per window), but susceptible to boundary bursts: a client could send the full limit at the end of one window and the full limit at the start of the next, doubling throughput for a brief period. Good for login throttling and simple API limits where approximate enforcement is acceptable.
- **Sliding window log** records the exact timestamp of every request in a sorted set, providing a true rolling window with no boundary bursts. Memory grows linearly with request volume (O(n) entries per client). Best for high-value APIs, payment processing, and any scenario where you need exact counts and can afford the memory cost.
- **Sliding window counter** blends two fixed-window counters using a weighted average to approximate a true sliding window. Offers near-exact accuracy with the same low memory footprint as fixed window. The two keys use [Redis cluster hash tags](https://redis.io/docs/latest/operate/oss_and_stack/reference/cluster-spec/#hash-tags) so they always land on the same cluster slot.
- **Token bucket** allows controlled bursts up to bucket capacity while enforcing a steady average rate over time. Tokens accumulate over time; each request consumes one. Ideal for APIs with naturally bursty traffic patterns (e.g. mobile apps that batch requests on launch).
- **Leaky bucket** (policing mode) provides strict no-burst behavior. A counter tracks the fill level and drains at a constant rate; requests that would exceed capacity are rejected immediately. Best when your downstream service cannot handle any spikes at all. Note: this is the policing variant only (immediate allow/deny); shaping mode (delayed queue drain) is not implemented.

### Why Lua Scripts?

Every algorithm executes a Lua script via `EVAL` for atomic read-modify-write. Without atomicity, concurrent requests can read the same state, both pass the limit check, and both write back, bypassing the limit. This is a TOCTOU (time-of-check-time-of-use) race condition that matters most under the high-concurrency conditions where rate limiting is critical.

Alternatives like `MULTI/EXEC` cannot branch on intermediate results, and `WATCH`/`MULTI`/`EXEC` requires retries on every concurrent write, which is the worst behavior for a rate limiter. Lua scripts always complete on the first attempt in a single round trip with no contention.

See the [Redis tutorial on why Lua scripts](https://redis.io/tutorials/howtos/ratelimiting/#why-lua-scripts) for a detailed comparison.

## Quick Start

```go
package main

import (
	"log"
	"net/http"
	"time"

	"github.com/valkey-io/valkey-go"
	"codeberg.org/matthew/capacitor"
	"codeberg.org/matthew/capacitor/bucket/leaky"
)

func main() {
	client, err := valkey.NewClient(valkey.ClientOption{
		InitAddress: []string{"localhost:6379"},
	})
	if err != nil {
		log.Fatal(err)
	}

	limiter := leaky.New(client, leaky.Config{
		Capacity:  10,
		LeakRate:  1,
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

### Using Other Algorithms

```go
import (
	"codeberg.org/matthew/capacitor/fixedwindow"
	"codeberg.org/matthew/capacitor/bucket/token"
	"codeberg.org/matthew/capacitor/slidingwindow/counter"
	"codeberg.org/matthew/capacitor/slidingwindow/timelog"
)

// Fixed window: 100 requests per minute
fw := fixedwindow.New(client, fixedwindow.Config{
	Limit:   100,
	Window:  time.Minute,
	Timeout: 50 * time.Millisecond,
})

// Token bucket: burst up to 20, refill 5/sec
tb := token.New(client, token.Config{
	Capacity:   20,
	RefillRate: 5,
	Timeout:    50 * time.Millisecond,
})

// Sliding window counter: 100 requests per minute (near-exact)
swc := counter.New(client, counter.Config{
	Limit:   100,
	Window:  time.Minute,
	Timeout: 50 * time.Millisecond,
})

// Sliding window log: 100 requests per minute (exact)
swl := timelog.New(client, timelog.Config{
	Limit:   100,
	Window:  time.Minute,
	Timeout: 50 * time.Millisecond,
})
```

## Configuration

Each algorithm has its own `Config` struct:

### Leaky Bucket / Token Bucket

| Field | Type | Description |
|---|---|---|
| `Capacity` | `int64` | Maximum tokens in the bucket |
| `LeakRate` / `RefillRate` | `float64` | Tokens drained/refilled per second |
| `KeyPrefix` | `string` | Prefix for Valkey keys |
| `Timeout` | `time.Duration` | Per-call Valkey timeout |

### Fixed Window / Sliding Window Counter / Sliding Window Log

| Field | Type | Description |
|---|---|---|
| `Limit` | `int64` | Maximum requests per window |
| `Window` | `time.Duration` | Window duration |
| `KeyPrefix` | `string` | Prefix for Valkey keys |
| `Timeout` | `time.Duration` | Per-call Valkey timeout |

All config fields are validated in `New()`: zero or negative values panic (programmer errors).

## Middleware Options

### `WithKeyFunc`

Controls how the rate-limit key is derived from each request. Defaults to `KeyFromRemoteIP`.

```go
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

Pass these to any algorithm's `New()`:

| Option | Description |
|---|---|
| `WithLogger(logger)` | Structured logger (`*slog.Logger`) |
| `WithFallback(strategy)` | `FallbackFailOpen` (default) or `FallbackFailClosed` |
| `WithMetrics(collector)` | Optional `MetricsCollector` implementation |

## Response Headers

Every response includes standard rate-limit headers:

| Header | Description |
|---|---|
| `RateLimit-Limit` | Bucket capacity / window limit |
| `RateLimit-Remaining` | Tokens / requests remaining |
| `RateLimit-Reset` | Seconds until a slot opens (denied requests only) |
| `Retry-After` | Same value as `RateLimit-Reset` (denied requests only) |

## Per-Profile Rate Limits

Use `WithProfiles` and `WithClassifier` to apply different rate limits based on an arbitrary per-request categorization (plan, tier, user type, etc.):

```go
profiles := capacitor.ProfileConfig{
	"basic":   leaky.New(client, leaky.Config{Capacity: 10, LeakRate: 1, Timeout: 50 * time.Millisecond}),
	"premium": leaky.New(client, leaky.Config{Capacity: 100, LeakRate: 10, Timeout: 50 * time.Millisecond}),
}

rl := capacitor.NewMiddleware(defaultLimiter,
	capacitor.WithProfiles(profiles),
	capacitor.WithClassifier(func(r *http.Request) string {
		return r.Header.Get("X-Plan")
	}),
)
```

- Each profile is a `capacitor.Capacitor`, use any algorithm or config
- If the classifier returns `""` or a name not in `ProfileConfig`, the default limiter is used
- Omit `WithProfiles` entirely for single-global-limit behavior

### Mixing Algorithms Per Profile

```go
profiles := capacitor.ProfileConfig{
	"basic":   fixedwindow.New(client, fixedwindow.Config{Limit: 10, Window: time.Minute, Timeout: 50 * time.Millisecond}),
	"premium": token.New(client, token.Config{Capacity: 100, RefillRate: 10, Timeout: 50 * time.Millisecond}),
}
```

## Direct Usage (Without Middleware)

You can call the limiter directly for non-HTTP use cases such as background workers or gRPC interceptors:

```go
result, err := limiter.Attempt(ctx, "user:42")
if err != nil {
	// Valkey unreachable: result contains the fallback decision.
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
