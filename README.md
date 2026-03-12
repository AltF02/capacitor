# Capacitor

A leaky-bucket rate limiter for Go. Ships with drop-in `net/http` middleware.

## Features

- **Multiple backends**: Valkey (Redis-compatible) or PostgreSQL
- Atomic leaky-bucket algorithm — safe for distributed deployments
- Standard `func(http.Handler) http.Handler` middleware — works with any router
- Configurable key extraction (IP, header, custom function)
- [IETF RateLimit header fields](https://datatracker.ietf.org/doc/draft-ietf-httpapi-ratelimit-headers/) on every response
- Fallback strategy when backend is unreachable (fail-open or fail-closed)
- Optional structured logging (`log/slog`) and metrics collection

## Installation

```sh
go get codeberg.org/matthew/capacitor
```

Requires Go 1.22+ and at least one backend (Valkey/Redis or PostgreSQL).

## Quick Start

Choose your backend:

### Valkey (Redis)

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

	cfg := capacitor.DefaultConfig()
	cfg.Capacity = 10
	cfg.LeakRate = 1

	store := capacitor.NewValkeyStore(client, cfg)
	limiter := capacitor.New(store, cfg)
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

### PostgreSQL

```go
package main

import (
	"context"
	"log"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
	"codeberg.org/matthew/capacitor"
)

func main() {
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, "postgres://localhost:5432/mydb")
	if err != nil {
		log.Fatal(err)
	}

	cfg := capacitor.DefaultPostgresConfig()
	cfg.Capacity = 10
	cfg.LeakRate = 1

	store, err := capacitor.NewPostgresStore(ctx, cfg)
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()

	limiter := capacitor.New(store, capacitor.DefaultConfig())

	mux := http.NewServeMux()
	mux.HandleFunc("GET /hello", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("Hello, world!\n"))
	})

	rl := capacitor.NewMiddleware(limiter)
	log.Println("listening on :8080")
	http.ListenAndServe(":8080", rl(mux))
}
```

## Backend Comparison

| Feature | Valkey | PostgreSQL |
|---------|--------|------------|
| Latency | ~1ms | ~5-10ms |
| Setup | Separate service | Already running |
| Persistence | Optional | Built-in |
| Clustering | Yes | Yes |

Both backends use advisory locks for atomic operations, ensuring correct behavior in distributed environments.

## Configuration

### Config (Valkey)

| Field | Type | Description |
|-------|------|-------------|
| `KeyPrefix` | `string` | Prefix for keys (e.g. `"capacitor"` → `capacitor:uid:<uid>`) |
| `Capacity` | `int64` | Maximum tokens in the bucket |
| `LeakRate` | `float64` | Tokens drained per second |
| `Timeout` | `time.Duration` | Per-call timeout |

### PostgresConfig

| Field | Type | Description |
|-------|------|-------------|
| `TableName` | `string` | Table for buckets (default: `capacitor_buckets`) |
| `Capacity` | `int64` | Maximum tokens in the bucket |
| `LeakRate` | `float64` | Tokens drained per second |
| `KeyPrefix` | `string` | Prefix for keys |
| `Timeout` | `time.Duration` | Per-call timeout |

## Middleware Options

### `WithKeyFunc`

```go
// Rate-limit by API key header.
rl := capacitor.NewMiddleware(limiter,
	capacitor.WithKeyFunc(capacitor.KeyFromHeader("X-API-Key")),
)
```

### `WithDenyHandler`

```go
rl := capacitor.NewMiddleware(limiter,
	capacitor.WithDenyHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":"rate limited"}`))
	})),
)
```

## Response Headers

| Header | Description |
|--------|-------------|
| `RateLimit-Limit` | Bucket capacity |
| `RateLimit-Remaining` | Tokens remaining |
| `RateLimit-Reset` | Seconds until a token becomes available |
| `Retry-After` | Same as `RateLimit-Reset` (denied only) |

## Testing

```sh
# Unit tests (no external dependencies)
go test ./...

# Integration tests
go test -tags=integration ./...
VALKEY_URL=localhost:6379 go test -tags=integration -run Valkey
POSTGRES_URL=localhost:5432 go test -tags=integration -run Postgres
```

Use `capacitor_test.MockStore` in your own tests:

```go
import "codeberg.org/matthew/capacitor/capacitor_test"

store := capacitor_test.NewMockStore(10, 5)
store.AllowAll() // or DenyAll() for testing denied cases
```

## License

[EUPL-1.2](LICENSE)
