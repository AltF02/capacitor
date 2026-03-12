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
	"github.com/valkey-io/valkey-go"
)

func TestValkeyStore_Integration(t *testing.T) {
	// Skip if no database URL
	valkeyURL := os.Getenv("VALKEY_URL")
	if valkeyURL == "" {
		t.Skip("VALKEY_URL not set, skipping integration test")
	}

	ctx := context.Background()
	client, err := valkey.NewClient(valkey.ClientOption{
		InitAddress: []string{"localhost:6379"},
	})
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer client.Close()

	// Wait for connection
	if err := client.Do(ctx, client.B().Ping().Build()).Error(); err != nil {
		t.Fatalf("failed to connect: %v", err)
	}

	cfg := capacitor.DefaultConfig()
	cfg.Capacity = 10

	store := capacitor.NewValkeyStore(client, cfg)

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

func TestValkeyStore_RateLimitExceeded_Integration(t *testing.T) {
	valkeyURL := os.Getenv("VALKEY_URL")
	if valkeyURL == "" {
		t.Skip("VALKEY_URL not set, skipping integration test")
	}

	ctx := context.Background()
	client, err := valkey.NewClient(valkey.ClientOption{
		InitAddress: []string{"localhost:6379"},
	})
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer client.Close()

	cfg := capacitor.DefaultConfig()
	cfg.Capacity = 2
	cfg.LeakRate = 100 // High rate so we don't leak during test

	store := capacitor.NewValkeyStore(client, cfg)

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

func TestValkeyStore_DifferentKeys_Integration(t *testing.T) {
	valkeyURL := os.Getenv("VALKEY_URL")
	if valkeyURL == "" {
		t.Skip("VALKEY_URL not set, skipping integration test")
	}

	ctx := context.Background()
	client, err := valkey.NewClient(valkey.ClientOption{
		InitAddress: []string{"localhost:6379"},
	})
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer client.Close()

	cfg := capacitor.DefaultConfig()
	cfg.Capacity = 1

	store := capacitor.NewValkeyStore(client, cfg)

	// Different keys should have independent limits
	result1, _ := store.Attempt(ctx, "user:1")
	result2, _ := store.Attempt(ctx, "user:2")

	if !result1.Allowed || !result2.Allowed {
		t.Error("different keys should have independent limits")
	}
}

func TestValkeyStore_HealthCheck_Integration(t *testing.T) {
	valkeyURL := os.Getenv("VALKEY_URL")
	if valkeyURL == "" {
		t.Skip("VALKEY_URL not set, skipping integration test")
	}

	ctx := context.Background()
	client, err := valkey.NewClient(valkey.ClientOption{
		InitAddress: []string{"localhost:6379"},
	})
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer client.Close()

	cfg := capacitor.DefaultConfig()
	store := capacitor.NewValkeyStore(client, cfg)

	if err := store.HealthCheck(ctx); err != nil {
		t.Errorf("health check failed: %v", err)
	}
}

func TestValkeyStore_LeakRate_Integration(t *testing.T) {
	valkeyURL := os.Getenv("VALKEY_URL")
	if valkeyURL == "" {
		t.Skip("VALKEY_URL not set, skipping integration test")
	}

	ctx := context.Background()
	client, err := valkey.NewClient(valkey.ClientOption{
		InitAddress: []string{"localhost:6379"},
	})
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer client.Close()

	cfg := capacitor.DefaultConfig()
	cfg.Capacity = 2
	cfg.LeakRate = 10 // 10 per second = 1 every 100ms

	store := capacitor.NewValkeyStore(client, cfg)

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

func TestValkeyStore_CapacitorIntegration(t *testing.T) {
	valkeyURL := os.Getenv("VALKEY_URL")
	if valkeyURL == "" {
		t.Skip("VALKEY_URL not set, skipping integration test")
	}

	ctx := context.Background()
	client, err := valkey.NewClient(valkey.ClientOption{
		InitAddress: []string{"localhost:6379"},
	})
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer client.Close()

	cfg := capacitor.DefaultConfig()
	cfg.Capacity = 5

	store := capacitor.NewValkeyStore(client, cfg)
	limiter := capacitor.New(store, cfg)

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
