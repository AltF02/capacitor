# Testing

Capacitor provides multiple testing strategies.

## Unit Tests with MockStore

Use the built-in `MockStore` for unit tests without external dependencies:

```go
import (
    "codeberg.org/matthew/capacitor"
    "codeberg.org/matthew/capacitor/capacitor_test"
)

func TestMyHandler(t *testing.T) {
    // Create mock store
    store := capacitor_test.NewMockStore(10, 5) // capacity 10, leak rate 5
    defer store.Close()

    // Configure behavior
    store.AllowAll()  // Optional: allow all requests
    // or
    store.DenyAll()   // Optional: deny all requests

    // Set specific bucket level
    store.SetCapacity("user:123", 9.5)

    // Create limiter
    limiter := capacitor.New(store, capacitor.DefaultConfig())

    // Test your handler
    // ...
}
```

## MockStore API

```go
// Create with capacity and leak rate
store := NewMockStore(capacity, leakRate)

// Control behavior
store.AllowAll()           // Always allow requests
store.DenyAll()            // Always deny requests
store.SetCapacity(key, level) // Set bucket level for specific key

// Implements Store interface
store.Attempt(ctx, uid)    // Rate limit check
store.HealthCheck(ctx)     // Returns nil
store.Close()              // Close store
```

## Integration Tests

Run against real backends using build tags:

```sh
# Run all tests including integration
go test -tags=integration ./...

# Run specific backend
VALKEY_URL=localhost:6379 go test -tags=integration -run Valkey
POSTGRES_URL=localhost:5432 go test -tags=integration -run Postgres
```

### Valkey Integration Tests

```go
//go:build integration
// +build integration

func TestValkeyStore_Integration(t *testing.T) {
    client, _ := valkey.NewClient(valkey.ClientOption{
        InitAddress: []string{"localhost:6379"},
    })
    defer client.Close()

    store := capacitor.NewValkeyStore(client, cfg)
    // Test...
}
```

### PostgreSQL Integration Tests

```go
//go:build integration
// +build integration

func TestPostgresStore_Integration(t *testing.T) {
    pool, _ := pgxpool.New(ctx, "postgres://localhost:5432/mydb")
    defer pool.Close()

    store, _ := capacitor.NewPostgresStore(ctx, cfg)
    // Test...
}
```

## Testing Your Own Code

### With MockStore

```go
func TestMyService_RateLimited(t *testing.T) {
    store := capacitor_test.NewMockStore(1, 100)
    defer store.Close()
    store.DenyAll() // Simulate rate limit exceeded

    service := NewMyService(store)

    err := service.DoSomething()
    if err != nil {
        t.Logf("Got expected error: %v", err)
    }
}
```

### Testing Middleware

```go
func TestMiddlewareIntegration(t *testing.T) {
    store := capacitor_test.NewMockStore(10, 5)
    defer store.Close()

    limiter := capacitor.New(store, cfg)
    handler := capacitor.NewMiddleware(limiter)

    // Use httptest
    req := httptest.NewRequest("GET", "/", nil)
    w := httptest.NewRecorder()

    handler(next).ServeHTTP(w, req)

    if w.Code == http.StatusTooManyRequests {
        t.Log("Request was rate limited")
    }
}
```

## Benchmark Tests

```go
func BenchmarkMyService(b *testing.B) {
    store := capacitor_test.NewMockStore(1000, 500)
    defer store.Close()

    limiter := capacitor.New(store, cfg)

    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        limiter.Attempt(context.Background(), "user:1")
    }
}
```
