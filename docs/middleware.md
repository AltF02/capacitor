# Middleware

Capacitor provides `net/http` middleware that wraps your handlers.

## Basic Usage

```go
mux := http.NewServeMux()
mux.HandleFunc("GET /", handler)

limiter := capacitor.New(store, cfg)
handler := capacitor.NewMiddleware(limiter)

http.ListenAndServe(":8080", handler(mux))
```

## Options

### Key Functions

Control how rate-limit keys are extracted from requests:

```go
// By IP (default)
capacitor.WithKeyFunc(capacitor.KeyFromRemoteIP)

// By header
capacitor.WithKeyFunc(capacitor.KeyFromHeader("X-API-Key"))

// Custom function
capacitor.WithKeyFunc(func(r *http.Request) string {
    return r.Header.Get("X-User-ID")
})
```

Return empty string to skip rate limiting for a request:

```go
capacitor.WithKeyFunc(func(r *http.Request) string {
    path := r.URL.Path
    if path == "/health" || path == "/ready" {
        return "" // Skip rate limiting
    }
    return r.RemoteAddr
})
```

### Custom Deny Handler

Default returns 429 with plain text. Customize:

```go
capacitor.WithDenyHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(http.StatusTooManyRequests)
    w.Write([]byte(`{"error":"rate limit exceeded","retry_after":1}`))
}))
```

### Custom Response Headers

```go
capacitor.WithDenyHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("X-RateLimit-Limit", "20")
    w.Header().Set("X-RateLimit-Remaining", "0")
    w.Header().Set("X-RateLimit-Reset", "1")
    http.Error(w, "rate limited", http.StatusTooManyRequests)
}))
```

## Response Headers

The middleware adds headers to every response:

| Header | Description | Always Present |
|--------|-------------|----------------|
| `RateLimit-Limit` | Bucket capacity | Yes |
| `RateLimit-Remaining` | Tokens remaining | Yes |
| `RateLimit-Reset` | Seconds until token available | Yes |
| `Retry-After` | Seconds until request allowed | Only when denied |

## Using with Other Routers

### net/http ServeMux

```go
mux := http.NewServeMux()
mux.HandleFunc("GET /api/", apiHandler)

rl := capacitor.NewMiddleware(limiter)
mux.Handle("GET /api/", rl(http.HandlerFunc(apiHandler)))
```

### chi

```go
import "github.com/go-chi/chi/v5"

r := chi.NewRouter()
r.Use(capacitor.NewMiddleware(limiter).Handler)
r.Get("/api/users", listUsers)
```

### gorilla/mux

```go
import "github.com/gorilla/mux"

r := mux.NewRouter()
r.Use(capacitor.NewMiddleware(limiter).Handler)
r.HandleFunc("/api/users", listUsers)
```

## Without Middleware

Call `Attempt` directly for non-HTTP use cases:

```go
result, err := limiter.Attempt(ctx, "user:123")
if err != nil {
    // Backend error - check fallback behavior
    log.Println("error:", err)
}

if !result.Allowed {
    log.Printf("denied, retry after %v\n", result.RetryAfter)
}
```
