# Multiple Backends

Switch between Valkey and PostgreSQL at runtime.

## Environment-Based Selection

```go
package main

import (
	"context"
	"log"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/valkey-io/valkey-go"
	"codeberg.org/matthew/capacitor"
)

func main() {
	backend := os.Getenv("RATE_LIMIT_BACKEND") // "valkey" or "postgres"

	var store capacitor.Store
	var err error

	switch backend {
	case "postgres":
		ctx := context.Background()
		pool, err := pgxpool.New(ctx, "postgres://localhost:5432/mydb")
		if err != nil {
			log.Fatal(err)
		}
		store, err = capacitor.NewPostgresStore(ctx, capacitor.PostgresConfig{
			Pool:      pool,
			Capacity:  100,
			LeakRate:  10,
		})
		if err != nil {
			log.Fatal(err)
		}

	default: // Valkey
		client, err := valkey.NewClient(valkey.ClientOption{
			InitAddress: []string{"localhost:6379"},
		})
		if err != nil {
			log.Fatal(err)
		}
		store = capacitor.NewValkeyStore(client, capacitor.DefaultConfig())
	}

	limiter := capacitor.New(store, capacitor.DefaultConfig())
	_ = limiter
}
```

## Factory Pattern

```go
package myapp

type StoreFactory interface {
	Create() (capacitor.Store, error)
}

type ValkeyFactory struct {
	Addr     string
	Capacity int64
	LeakRate float64
}

func (f *ValkeyFactory) Create() (capacitor.Store, error) {
	client, err := valkey.NewClient(valkey.ClientOption{
		InitAddress: []string{f.Addr},
	})
	if err != nil {
		return nil, err
	}

	cfg := capacitor.DefaultConfig()
	cfg.Capacity = f.Capacity
	cfg.LeakRate = f.LeakRate

	return capacitor.NewValkeyStore(client, cfg), nil
}

type PostgresFactory struct {
	DSN      string
	Capacity int64
	LeakRate float64
}

func (f *PostgresFactory) Create() (capacitor.Store, error) {
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, f.DSN)
	if err != nil {
		return nil, err
	}

	cfg := capacitor.DefaultPostgresConfig()
	cfg.Pool = pool
	cfg.Capacity = f.Capacity
	cfg.LeakRate = f.LeakRate

	return capacitor.NewPostgresStore(ctx, cfg)
}

// Usage
var factory StoreFactory
if usePostgres {
	factory = &PostgresFactory{DSN: "postgres://...", Capacity: 100, LeakRate: 10}
} else {
	factory = &ValkeyFactory{Addr: "localhost:6379", Capacity: 100, LeakRate: 10}
}

store, err := factory.Create()
```

## Feature Flags

```go
type RateLimiterConfig struct {
	Backend   string // "valkey" or "postgres"
	ValkeyAddr string
	PostgresDSN string
	Capacity   int64
	LeakRate   float64
}

func NewRateLimiter(cfg RateLimiterConfig) (capacitor.Store, error) {
	if cfg.Backend == "postgres" {
		ctx := context.Background()
		pool, err := pgxpool.New(ctx, cfg.PostgresDSN)
		if err != nil {
			return nil, err
		}
		return capacitor.NewPostgresStore(ctx, capacitor.PostgresConfig{
			Pool:      pool,
			Capacity:  cfg.Capacity,
			LeakRate:  cfg.LeakRate,
		})
	}

	client, err := valkey.NewClient(valkey.ClientOption{
		InitAddress: []string{cfg.ValkeyAddr},
	})
	if err != nil {
		return nil, err
	}

	cfg := capacitor.DefaultConfig()
	cfg.Capacity = cfg.Capacity
	cfg.LeakRate = cfg.LeakRate

	return capacitor.NewValkeyStore(client, cfg), nil
}
```
