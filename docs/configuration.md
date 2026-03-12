# Configuration

## Config Types

### Valkey Config

```go
type Config struct {
    KeyPrefix string        // Prefix for keys
    Capacity  int64         // Maximum tokens in bucket
    LeakRate  float64       // Tokens leaked per second
    Timeout   time.Duration // Request timeout
}
```

### PostgreSQL Config

```go
type PostgresConfig struct {
    Pool        *pgxpool.Pool
    ConnConfig  pgxpool.Config
    TableName   string
    Capacity    int64
    LeakRate    float64
    KeyPrefix   string
    Timeout     time.Duration
}
```

## Default Values

### Valkey

```go
func DefaultConfig() Config {
    return Config{
        KeyPrefix: "capacitor",
        Capacity:  20,
        LeakRate:  5,    // 5 requests per second
        Timeout:   50 * time.Millisecond,
    }
}
```

### PostgreSQL

```go
func DefaultPostgresConfig() PostgresConfig {
    return PostgresConfig{
        TableName: "capacitor_buckets",
        Capacity:  20,
        LeakRate:  5,
        KeyPrefix: "capacitor",
        Timeout:   50 * time.Millisecond,
    }
}
```

## Capacity and LeakRate

These two values define your rate limit behavior:

| Capacity | LeakRate | Behavior |
|----------|----------|----------|
| 20 | 5 | Burst of 20, then 5/second |
| 100 | 10 | Burst of 100, then 10/second |
| 1 | 1 | Strict 1/second |

### Choosing Values

- **High-capacity, high-leak**: Allow bursts (e.g., for batch jobs starting)
- **Low-capacity, low-leak**: Strict limiting (e.g., public APIs)
- **Capacity ≈ LeakRate × expected_burst_duration**

## Timeout

Set based on your backend latency:

```go
cfg := capacitor.DefaultConfig()
cfg.Timeout = 100 * time.Millisecond // Allow for slower backends
```

For Valkey on localhost, 50ms is usually sufficient. For PostgreSQL, consider 100-200ms.

## Limiter Options

Pass options to `capacitor.New()`:

```go
limiter := capacitor.New(store, cfg,
    capacitor.WithLogger(myLogger),
    capacitor.WithFallback(capacitor.FallbackFailClosed),
    capacitor.WithMetrics(myMetrics),
)
```

| Option | Description |
|--------|-------------|
| `WithLogger(*slog.Logger)` | Structured logger |
| `WithFallback(FallbackStrategy)` | `FallbackFailOpen` or `FallbackFailClosed` |
| `WithMetrics(MetricsCollector)` | Custom metrics |
