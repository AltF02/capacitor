//go:build integration
// +build integration

package capacitor_test

import (
	"context"
	"os"
	"testing"
	"time"

	"codeberg.org/matthew/capacitor"

	"github.com/google/go-cmp/cmp"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPostgresStore_Integration(t *testing.T) {
	// Skip if no database URL
	dbURL := os.Getenv("POSTGRES_URL")
	if dbURL == "" {
		t.Skip("POSTGRES_URL not set, skipping integration test")
	}

	ctx := context.Background()
	cfg := capacitor.DefaultPostgresConfig()
	cfg.ConnConfig = pgxpool.Config{}
	cfg.ConnConfig.ConnConfig.Host = "localhost"
	cfg.ConnConfig.ConnConfig.Port = 5432
	cfg.ConnConfig.ConnConfig.Database = "test"
	cfg.ConnConfig.ConnConfig.User = "postgres"
	cfg.ConnConfig.ConnConfig.Password = ""
	cfg.Capacity = 10
	cfg.LeakRate = 5

	store, err := capacitor.NewPostgresStore(ctx, cfg)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	// Test basic functionality
	result, err := store.Attempt(ctx, "user:1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.Allowed {
		t.Error("expected first request to be allowed")
	}

	if result.Remaining != 9 {
		t.Errorf("remaining = %d, want 9", result.Remaining)
	}
}

func TestPostgresStore_RateLimitExceeded_Integration(t *testing.T) {
	dbURL := os.Getenv("POSTGRES_URL")
	if dbURL == "" {
		t.Skip("POSTGRES_URL not set, skipping integration test")
	}

	ctx := context.Background()
	cfg := capacitor.DefaultPostgresConfig()
	cfg.Capacity = 2
	cfg.LeakRate = 100 // High rate so we don't leak during test

	store, err := capacitor.NewPostgresStore(ctx, cfg)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	// First two should be allowed
	for i := 0; i < 2; i++ {
		result, err := store.Attempt(ctx, "user:1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !result.Allowed {
			t.Errorf("request %d should be allowed", i+1)
		}
	}

	// Third should be denied
	result, err := store.Attempt(ctx, "user:1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Allowed {
		t.Error("expected third request to be denied")
	}
	if result.Remaining != 0 {
		t.Errorf("remaining = %d, want 0", result.Remaining)
	}
}

func TestPostgresStore_DifferentKeys_Integration(t *testing.T) {
	dbURL := os.Getenv("POSTGRES_URL")
	if dbURL == "" {
		t.Skip("POSTGRES_URL not set, skipping integration test")
	}

	ctx := context.Background()
	cfg := capacitor.DefaultPostgresConfig()
	cfg.Capacity = 1

	store, err := capacitor.NewPostgresStore(ctx, cfg)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	// Different keys should have independent limits
	result1, _ := store.Attempt(ctx, "user:1")
	result2, _ := store.Attempt(ctx, "user:2")

	if !result1.Allowed || !result2.Allowed {
		t.Error("different keys should have independent limits")
	}
}

func TestPostgresStore_HealthCheck_Integration(t *testing.T) {
	dbURL := os.Getenv("POSTGRES_URL")
	if dbURL == "" {
		t.Skip("POSTGRES_URL not set, skipping integration test")
	}

	ctx := context.Background()
	cfg := capacitor.DefaultPostgresConfig()

	store, err := capacitor.NewPostgresStore(ctx, cfg)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	if err := store.HealthCheck(ctx); err != nil {
		t.Errorf("health check failed: %v", err)
	}
}

func TestPostgresStore_LeakRate_Integration(t *testing.T) {
	dbURL := os.Getenv("POSTGRES_URL")
	if dbURL == "" {
		t.Skip("POSTGRES_URL not set, skipping integration test")
	}

	ctx := context.Background()
	cfg := capacitor.DefaultPostgresConfig()
	cfg.Capacity = 2
	cfg.LeakRate = 10 // 10 per second = 1 every 100ms

	store, err := capacitor.NewPostgresStore(ctx, cfg)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	// Fill the bucket
	store.Attempt(ctx, "user:1")
	store.Attempt(ctx, "user:1")

	// Wait for 150ms - should leak 1.5 tokens, leaving room for 1 request
	time.Sleep(150 * time.Millisecond)

	result, err := store.Attempt(ctx, "user:1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Allowed {
		t.Error("expected request to be allowed after leak")
	}
}

// TestPostgresStore_Concurrent_Integration tests concurrent access.
func TestPostgresStore_Concurrent_Integration(t *testing.T) {
	dbURL := os.Getenv("POSTGRES_URL")
	if dbURL == "" {
		t.Skip("POSTGRES_URL not set, skipping integration test")
	}

	ctx := context.Background()
	cfg := capacitor.DefaultPostgresConfig()
	cfg.Capacity = 100

	store, err := capacitor.NewPostgresStore(ctx, cfg)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	// Run multiple concurrent requests
	const numGoroutines = 10
	const numRequests = 100

	type result struct {
		allowed bool
		err     error
	}

	results := make(chan result, numGoroutines*numRequests)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			for j := 0; j < numRequests; j++ {
				r, err := store.Attempt(ctx, "user:1")
				results <- result{allowed: r.Allowed, err: err}
			}
		}()
	}

	var denied, errors int
	for i := 0; i < numGoroutines*numRequests; i++ {
		res := <-results
		if res.err != nil {
			errors++
		}
		if !res.allowed {
			denied++
		}
	}

	if errors > 0 {
		t.Errorf("got %d errors", errors)
	}

	// With capacity 100 and 1000 requests, some should be denied
	if denied == 0 {
		t.Error("expected some requests to be denied")
	}

	t.Logf("allowed: %d, denied: %d", numGoroutines*numRequests-denied, denied)
}

func TestPostgresStore_CapacitorIntegration(t *testing.T) {
	dbURL := os.Getenv("POSTGRES_URL")
	if dbURL == "" {
		t.Skip("POSTGRES_URL not set, skipping integration test")
	}

	ctx := context.Background()
	cfg := capacitor.DefaultPostgresConfig()
	cfg.Capacity = 5

	store, err := capacitor.NewPostgresStore(ctx, cfg)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	limiter := capacitor.New(store, capacitor.DefaultConfig())

	result, err := limiter.Attempt(ctx, "user:1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := capacitor.Result{
		Allowed:    true,
		Remaining: 4,
		Limit:     5,
		RetryAfter: 0,
	}

	if diff := cmp.Diff(expected, result); diff != "" {
		t.Errorf("result mismatch (-want +got):\n%s", diff)
	}
}
