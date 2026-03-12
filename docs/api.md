# API Reference

## Core Types

### Result

Returned from `Attempt()`:

```go
type Result struct {
    Allowed    bool
    Remaining  int64
    Limit      int64
    RetryAfter time.Duration // Zero when allowed
}
```

### Config

Valkey backend configuration:

```go
type Config struct {
    KeyPrefix string        // Key prefix (default: "capacitor")
    Capacity  int64         // Bucket capacity (default: 20)
    LeakRate  float64       // Tokens per second (default: 5)
    Timeout   time.Duration // Request timeout (default: 50ms)
}
```

### PostgresConfig

PostgreSQL backend configuration:

```go
type PostgresConfig struct {
    Pool       *pgxpool.Pool
    ConnConfig pgxpool.Config
    TableName  string        // Table name (default: "capacitor_buckets")
    Capacity   int64         // Bucket capacity (default: 20)
    LeakRate   float64       // Tokens per second (default: 5)
    KeyPrefix  string        // Key prefix (default: "capacitor")
    Timeout    time.Duration // Request timeout (default: 50ms)
}
```

## Store Interface

```go
type Store interface {
    Attempt(ctx context.Context, uid string) (Result, error)
    HealthCheck(ctx context.Context) error
    Close() error
}
```

## Core Functions

### New

Create a new rate limiter:

```go
func New(store Store, cfg Config, opts ...Option) *Capacitor
```

### NewValkeyStore

Create a Valkey store:

```go
func NewValkeyStore(client valkey.Client, cfg Config) *ValkeyStore
```

### NewPostgresStore

Create a PostgreSQL store:

```go
func NewPostgresStore(ctx context.Context, cfg PostgresConfig) (*PostgresStore, error)
```

## Limiter Methods

### Attempt

Check if a request is allowed:

```go
func (c *Capacitor) Attempt(ctx context.Context, uid string) (Result, error)
```

### HealthCheck

Check backend connectivity:

```go
func (c *Capacitor) HealthCheck(ctx context.Context) error
```

### Close

Close the limiter and backend connection:

```go
func (c *Capacitor) Close() error
```

## Middleware Functions

### NewMiddleware

Create HTTP middleware:

```go
func NewMiddleware(limiter *Capacitor, opts ...MiddlewareOption) http.Handler
```

### MiddlewareOption

```go
type MiddlewareOption func(*Middleware)
```

Options:
- `WithKeyFunc(KeyFunc)` - Set key extraction function
- `WithDenyHandler(http.Handler)` - Set custom deny handler

## Key Functions

### KeyFromRemoteIP

Extract key from remote IP:

```go
func KeyFromRemoteIP(r *http.Request) string
```

### KeyFromHeader

Extract key from header:

```go
func KeyFromHeader(name string) func(r *http.Request) string
```

### KeyFunc

Custom key extraction:

```go
type KeyFunc func(r *http.Request) string
```

## Limiter Options

### WithLogger

```go
func WithLogger(logger *slog.Logger) Option
```

### WithFallback

```go
func WithFallback(strategy FallbackStrategy) Option
```

### WithMetrics

```go
func WithMetrics(collector MetricsCollector) Option
```

## Fallback Strategies

```go
type FallbackStrategy int

const (
    FallbackFailOpen  FallbackStrategy // Allow requests (default)
    FallbackFailClosed                  // Deny requests
)
```

## Metrics

### MetricsCollector

```go
type MetricsCollector interface {
    RecordAttempt(key string)
    RecordDenied(key string)
    RecordLatency(d time.Duration)
}
```

## Errors

### ErrEmptyUID

Returned when empty UID is passed:

```go
var ErrEmptyUID = errors.New("capacitor: uid must not be empty")
```

## Constants

### Default Values

```go
const (
    DefaultCapacity = 20
    DefaultLeakRate = 5.0
    DefaultTimeout  = 50 * time.Millisecond
)
```
