# Using with chi Router

Integrate Capacitor with the chi router.

## Basic Setup

```go
package main

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"codeberg.org/matthew/capacitor"
)

func main() {
	// Create limiter (see other examples for store setup)
	limiter := capacitor.New(store, cfg)

	// Create middleware
	mw := capacitor.NewMiddleware(limiter)

	// Create chi router
	r := chi.NewRouter()

	// Apply to all routes
	r.Use(mw.Handler)

	r.Get("/users", listUsers)
	r.Get("/posts", listPosts)

	http.ListenAndServe(":8080", r)
}
```

## Per-Route Rate Limiting

```go
r := chi.NewRouter()

// No rate limit for health
r.Get("/health", healthHandler)

// Strict rate limit for API
r.Group(func(api chi.Router) {
	api.Use(mw.Handler)
	api.Get("/users", listUsers)
	api.Post("/users", createUser)
})

// Different limits for admin
r.Group(func(admin chi.Router) {
	admin.Use(adminLimiter.Handler)
	admin.Get("/admin/stats", adminStats)
})
```

## Multiple Rate Limits

```go
// Create different limiters
publicLimiter := capacitor.New(publicStore, capacitor.Config{
	Capacity: 100,
	LeakRate: 10,
})

strictLimiter := capacitor.New(strictStore, capacitor.Config{
	Capacity: 10,
	LeakRate: 1,
})

// Apply to routes
r.Group(func(api chi.Router) {
	api.Use(capacitor.NewMiddleware(publicLimiter).Handler)
	api.Get("/public/data", publicData)
})

r.Group(func(internal chi.Router) {
	internal.Use(capacitor.NewMiddleware(strictLimiter).Handler)
	internal.Get("/internal/sensitive", sensitiveData)
})
```

## Custom Key with chi

```go
r := chi.NewRouter()

r.Use(capacitor.NewMiddleware(limiter,
	capacitor.WithKeyFunc(func(r *http.Request) string {
		// Get user ID from chi URL parameter
		userID := chi.URLParam(r, "userID")
		if userID != "" {
			return "user:" + userID
		}
		
		// Fall back to API key
		return r.Header.Get("X-API-Key")
	}),
).Handler)

r.Get("/users/{userID}", getUser)
```

## Skip Rate Limiting

```go
r := chi.NewRouter()

r.Use(capacitor.NewMiddleware(limiter,
	capacitor.WithKeyFunc(func(r *http.Request) string {
		// Skip certain paths
		switch r.URL.Path {
		case "/health":
			return ""
		case "/ready":
			return ""
		}
		
		// Use API key for others
		return r.Header.Get("X-API-Key")
	}),
).Handler)

r.Get("/health", healthHandler)
r.Get("/ready", readyHandler)
r.Get("/api/data", apiHandler)
```

## Full Example

```go
package main

import (
	"context"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/valkey-io/valkey-go"
	"codeberg.org/matthew/capacitor"
)

func main() {
	// Setup Valkey
	client, _ := valkey.NewClient(valkey.ClientOption{
		InitAddress: []string{"localhost:6379"},
	})
	defer client.Close()

	// Create limiter
	cfg := capacitor.DefaultConfig()
	cfg.Capacity = 100
	cfg.LeakRate = 10

	store := capacitor.NewValkeyStore(client, cfg)
	limiter := capacitor.New(store, cfg)
	defer limiter.Close()

	// Create middleware with custom key
	mw := capacitor.NewMiddleware(limiter,
		capacitor.WithKeyFunc(func(r *http.Request) string {
			// Skip health checks
			if r.URL.Path == "/health" {
				return ""
			}
			// Use API key or fall back to IP
			if key := r.Header.Get("X-API-Key"); key != "" {
				return key
			}
			return r.RemoteAddr
		}),
	)

	// Create router
	r := chi.NewRouter()

	// Public routes (with rate limiting)
	r.Group(func(r chi.Router) {
		r.Use(mw.Handler)
		r.Get("/api/users", listUsers)
		r.Get("/api/posts", listPosts)
	})

	// Health (no rate limiting)
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("OK"))
	})

	log.Println("Server starting on :8080")
	log.Fatal(http.ListenAndServe(":8080", r))
}
```
