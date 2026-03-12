# PostgreSQL Backend

The PostgreSQL backend uses PostgreSQL advisory locks for atomic operations. Ideal when you already have PostgreSQL running and don't want to manage a separate Valkey/Redis instance.

## Comparison

| Aspect | PostgreSQL | Valkey |
|--------|-----------|--------|
| Latency | ~5-10ms | ~1ms |
| Throughput | Medium | Very high |
| Persistence | Built-in | Optional |
| Clustering | Yes | Yes |
| Setup | Already running | Separate service |

## Usage

```go
import (
    "context"
    "log"

    "github.com/jackc/pgx/v5/pgxpool"
    "codeberg.org/matthew/capacitor"
)

func main() {
    ctx := context.Background()

    pool, err := pgxpool.New(ctx, "postgres://user:pass@localhost:5432/mydb")
    if err != nil {
        log.Fatal(err)
    }
    defer pool.Close()

    cfg := capacitor.DefaultPostgresConfig()
    cfg.Capacity = 100
    cfg.LeakRate = 10

    store, err := capacitor.NewPostgresStore(ctx, cfg)
    if err != nil {
        log.Fatal(err)
    }
    defer store.Close()

    limiter := capacitor.New(store, capacitor.DefaultConfig())
}
```

## Configuration

```go
type PostgresConfig struct {
    Pool        *pgxpool.Pool  // Existing pool (optional)
    ConnConfig  pgxpool.Config  // Connection config (used if Pool is nil)
    TableName   string          // Table name (default: "capacitor_buckets")
    Capacity    int64           // Bucket capacity (default: 20)
    LeakRate    float64         // Tokens per second (default: 5)
    KeyPrefix   string          // Key prefix (default: "capacitor")
    Timeout     time.Duration   // Per-request timeout (default: 50ms)
}
```

## Automatic Schema Creation

The store automatically creates the required table on initialization:

```sql
CREATE TABLE IF NOT EXISTS capacitor_buckets (
    key         TEXT PRIMARY KEY,
    level       DOUBLE PRECISION NOT NULL DEFAULT 0,
    last_leak   DOUBLE PRECISION NOT NULL,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS capacitor_buckets_updated_at ON capacitor_buckets(updated_at);
```

## How It Works

1. Begins a database transaction
2. Acquires an advisory lock (`pg_advisory_xact_lock`) on the key hash
3. Reads current bucket state, applies leaky bucket algorithm
4. Upserts new state
5. Commits (releases lock automatically)

This ensures atomicity even across multiple application instances.

## Connection Pool Options

```go
cfg := capacitor.DefaultPostgresConfig()
cfg.ConnConfig = pgxpool.Config{
    ConnConfig: pgxpool.ConnConfig{
        Host:     "localhost",
        Port:     5432,
        Database: "mydb",
        User:     "user",
        Password: "pass",
        MinConns: 5,
        MaxConns: 20,
    },
    MaxConnLifetime: time.Hour,
    MaxConnIdleTime: 30 * time.Minute,
}

store, err := capacitor.NewPostgresStore(ctx, cfg)
```

## Health Check

```go
if err := limiter.HealthCheck(ctx); err != nil {
    log.Fatal("PostgreSQL unreachable:", err)
}
```

## Running Integration Tests

```sh
# Start PostgreSQL
docker run -d -p 5432:5432 -e POSTGRES_PASSWORD=pass postgres

# Run tests
POSTGRES_URL=postgres://postgres:pass@localhost:5432/postgres go test -tags=integration -run Postgres ./...
```

## Performance Tips

- Use connection pooling (pgxpool)
- Keep `Timeout` short to avoid holding locks
- Consider `LeakRate` carefully — higher rates mean more competitive locks
- For very high throughput, consider Valkey instead
