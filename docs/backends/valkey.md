# Valkey Backend

The Valkey backend uses [Valkey](https://valkey.io) (Redis-compatible) for storing bucket state. It provides the lowest latency and is the recommended backend for high-throughput applications.

## Comparison

| Aspect | Valkey | PostgreSQL |
|--------|--------|------------|
| Latency | ~1ms | ~5-10ms |
| Throughput | Very high | Medium |
| Persistence | Optional | Built-in |
| Clustering | Yes | Yes |
| Setup | Separate service | Already running |

## Usage

```go
import (
    "github.com/valkey-io/valkey-go"
    "codeberg.org/matthew/capacitor"
)

func main() {
    client, err := valkey.NewClient(valkey.ClientOption{
        InitAddress: []string{"localhost:6379"},
        Password:    "", // optional
        DB:          0, // optional
    })
    if err != nil {
        log.Fatal(err)
    }
    defer client.Close()

    cfg := capacitor.DefaultConfig()
    cfg.Capacity = 100
    cfg.LeakRate = 10

    store := capacitor.NewValkeyStore(client, cfg)
    limiter := capacitor.New(store, cfg)
    defer limiter.Close()
}
```

## Configuration

The `Config` struct for Valkey:

```go
type Config struct {
    KeyPrefix string        // Prefix for keys (default: "capacitor")
    Capacity  int64         // Bucket capacity (default: 20)
    LeakRate  float64      // Tokens per second (default: 5)
    Timeout   time.Duration // Per-request timeout (default: 50ms)
}
```

## How It Works

1. Uses a Lua script executed atomically in Valkey
2. Single round-trip per request
3. Hash structure stores bucket state: `level` and `last_leak`
4. Keys expire automatically based on capacity/leak_rate

## Connection Options

```go
client, err := valkey.NewClient(valkey.ClientOption{
    InitAddress:        []string{"localhost:6379"},
    Password:           "mypassword",
    DB:                 0,
    PoolSize:           10,        // Connections per CPU
    MinIdleConns:       5,
    MaxRetries:         3,
    DialTimeout:        5 * time.Second,
    ReadTimeout:        3 * time.Second,
    WriteTimeout:       3 * time.Second,
})
```

## Health Check

```go
if err := limiter.HealthCheck(ctx); err != nil {
    log.Fatal("Valkey unreachable:", err)
}
```

## Running Integration Tests

```sh
# Start Valkey
docker run -d -p 6379:6379 valkey/valkey

# Run tests
VALKEY_URL=localhost:6379 go test -tags=integration -run Valkey ./...
```
