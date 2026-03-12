# Basic HTTP Server

A complete example of a rate-limited HTTP server.

## Valkey Backend

```go
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/valkey-io/valkey-go"
	"codeberg.org/matthew/capacitor"
)

func main() {
	// Create Valkey client
	client, err := valkey.NewClient(valkey.ClientOption{
		InitAddress: []string{"localhost:6379"},
	})
	if err != nil {
		log.Fatal("Failed to create Valkey client:", err)
	}
	defer client.Close()

	// Configure rate limiter
	cfg := capacitor.DefaultConfig()
	cfg.Capacity = 100  // Allow bursts of 100
	cfg.LeakRate = 10   // Then 10 per second

	// Create store and limiter
	store := capacitor.NewValkeyStore(client, cfg)
	limiter := capacitor.New(store, cfg)
	defer limiter.Close()

	// Create middleware
	mw := capacitor.NewMiddleware(limiter)

	// Create router
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/hello", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("Hello, World!\n"))
	})
	mux.HandleFunc("GET /api/users", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("User list\n"))
	})
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("OK\n"))
	})

	// Wrap with rate limiter (skip /health)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			mux.ServeHTTP(w, r)
			return
		}
		mw(mux).ServeHTTP(w, r)
	})

	// Start server
	server := &http.Server{
		Addr:    ":8080",
		Handler: handler,
	}

	go func() {
		log.Println("Server starting on :8080")
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal("Server error:", err)
		}
	}()

	// Graceful shutdown
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig

	log.Println("Shutting down...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		log.Fatal("Shutdown error:", err)
	}
}
```

## PostgreSQL Backend

```go
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"codeberg.org/matthew/capacitor"
)

func main() {
	ctx := context.Background()

	// Create PostgreSQL pool
	pool, err := pgxpool.New(ctx, "postgres://localhost:5432/mydb")
	if err != nil {
		log.Fatal("Failed to create pool:", err)
	}
	defer pool.Close()

	// Configure rate limiter
	cfg := capacitor.DefaultPostgresConfig()
	cfg.Capacity = 100
	cfg.LeakRate = 10

	// Create store and limiter
	store, err := capacitor.NewPostgresStore(ctx, cfg)
	if err != nil {
		log.Fatal("Failed to create store:", err)
	}
	defer store.Close()

	limiter := capacitor.New(store, capacitor.DefaultConfig())

	// Create middleware
	mw := capacitor.NewMiddleware(limiter)

	// Create router
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/hello", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("Hello, World!\n"))
	})

	// Start server
	server := &http.Server{
		Addr:    ":8080",
		Handler: mw(mux),
	}

	go func() {
		log.Println("Server starting on :8080")
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal("Server error:", err)
		}
	}()

	// Graceful shutdown
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig

	log.Println("Shutting down...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		log.Fatal("Shutdown error:", err)
	}
}
```

## Testing

```go
package main

import (
	"context"
	"log"

	"codeberg.org/matthew/capacitor"
	"codeberg.org/matthew/capacitor/capacitor_test"
)

func main() {
	// Use mock for testing
	store := capacitor_test.NewMockStore(100, 10)
	defer store.Close()

	cfg := capacitor.DefaultConfig()
	limiter := capacitor.New(store, cfg)

	// Test requests
	for i := 0; i < 5; i++ {
		result, err := limiter.Attempt(context.Background(), "user:1")
		if err != nil {
			log.Fatal(err)
		}
		log.Printf("Request %d: allowed=%v remaining=%d", i+1, result.Allowed, result.Remaining)
	}
}
```
