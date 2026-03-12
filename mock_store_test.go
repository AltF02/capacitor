package capacitor_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"codeberg.org/matthew/capacitor"

	"github.com/google/go-cmp/cmp"
)

// MockStore implements capacitor.Store for testing without real backends.
type MockStore struct {
	mu        sync.Mutex
	buckets   map[string]*bucketState
	allowAll  bool
	denyAll   bool
	closed    bool
	healthErr error

	// Config for simulating leaky bucket
	capacity int64
	leakRate float64
}

type bucketState struct {
	level     float64
	lastLeak  float64
	updatedAt time.Time
}

func NewMockStore(capacity int64, leakRate float64) *MockStore {
	return &MockStore{
		buckets:  make(map[string]*bucketState),
		capacity: capacity,
		leakRate: leakRate,
	}
}

// AllowAll makes the mock allow all requests (for testing).
func (m *MockStore) AllowAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.allowAll = true
	m.denyAll = false
}

// DenyAll makes the mock deny all requests (for testing).
func (m *MockStore) DenyAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.denyAll = true
	m.allowAll = false
}

// SetHealthError sets the error to return on HealthCheck.
func (m *MockStore) SetHealthError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.healthErr = err
}

// SetCapacity sets the capacity for a specific key (for testing specific scenarios).
func (m *MockStore) SetCapacity(key string, level float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.buckets[key] = &bucketState{
		level:     level,
		lastLeak:  float64(time.Now().UnixMilli()) / 1000.0,
		updatedAt: time.Now(),
	}
}

func (m *MockStore) Attempt(ctx context.Context, uid string) (capacitor.Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return capacitor.Result{}, errors.New("store closed")
	}

	if uid == "" {
		return capacitor.Result{}, capacitor.ErrEmptyUID
	}

	if m.allowAll {
		return capacitor.Result{
			Allowed:   true,
			Remaining: m.capacity - 1,
			Limit:     m.capacity,
		}, nil
	}

	if m.denyAll {
		return capacitor.Result{
			Allowed:    false,
			Remaining:  0,
			Limit:      m.capacity,
		}, nil
	}

	now := float64(time.Now().UnixMilli()) / 1000.0
	key := "capacitor:uid:" + uid

	bucket, exists := m.buckets[key]
	if !exists {
		bucket = &bucketState{
			level:    0,
			lastLeak: now,
		}
		m.buckets[key] = bucket
	}

	// Leaky bucket algorithm
	elapsed := now - bucket.lastLeak
	leaked := elapsed * m.leakRate
	bucket.level = max(0, bucket.level-leaked)

	allowed := bucket.level+1 <= float64(m.capacity)
	if allowed {
		bucket.level++
	}
	bucket.lastLeak = now

	remaining := int64(max(0, float64(m.capacity)-bucket.level))

	return capacitor.Result{
		Allowed:   allowed,
		Remaining: remaining,
		Limit:     m.capacity,
	}, nil
}

func (m *MockStore) HealthCheck(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.healthErr
}

func (m *MockStore) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return nil
}

func TestMockStore_Basic(t *testing.T) {
	store := NewMockStore(10, 5)
	defer store.Close()

	// First request should be allowed
	result, err := store.Attempt(context.Background(), "user:1")
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

func TestMockStore_RateLimitExceeded(t *testing.T) {
	store := NewMockStore(2, 5)
	defer store.Close()

	// First two should be allowed
	for i := 0; i < 2; i++ {
		result, err := store.Attempt(context.Background(), "user:1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !result.Allowed {
			t.Errorf("request %d should be allowed", i+1)
		}
	}

	// Third should be denied
	result, err := store.Attempt(context.Background(), "user:1")
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

func TestMockStore_EmptyUID(t *testing.T) {
	store := NewMockStore(10, 5)
	defer store.Close()

	_, err := store.Attempt(context.Background(), "")
	if !errors.Is(err, capacitor.ErrEmptyUID) {
		t.Errorf("expected ErrEmptyUID, got %v", err)
	}
}

func TestMockStore_DifferentKeys(t *testing.T) {
	store := NewMockStore(1, 5)
	defer store.Close()

	// Different keys should have independent limits
	result1, _ := store.Attempt(context.Background(), "user:1")
	result2, _ := store.Attempt(context.Background(), "user:2")

	if !result1.Allowed || !result2.Allowed {
		t.Error("different keys should have independent limits")
	}
}

func TestMockStore_HealthCheck(t *testing.T) {
	store := NewMockStore(10, 5)
	defer store.Close()

	// No error by default
	if err := store.HealthCheck(context.Background()); err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// Set error
	store.SetHealthError(errors.New("connection refused"))
	if err := store.HealthCheck(context.Background()); err == nil {
		t.Error("expected error, got nil")
	}
}

func TestMockStore_Close(t *testing.T) {
	store := NewMockStore(10, 5)
	store.Close()

	// After close, attempts should fail
	_, err := store.Attempt(context.Background(), "user:1")
	if err == nil {
		t.Error("expected error after close")
	}
}

// TestCapacitorWithMockStore tests the full Capacitor with a mock store.
func TestCapacitorWithMockStore(t *testing.T) {
	cfg := capacitor.DefaultConfig()
	cfg.Capacity = 5
	cfg.LeakRate = 10

	store := NewMockStore(cfg.Capacity, cfg.LeakRate)
	defer store.Close()

	limiter := capacitor.New(store, cfg)

	// Allow all for this test
	store.AllowAll()

	result, err := limiter.Attempt(context.Background(), "user:1")
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

// BenchmarkMockStore benchmarks the mock store.
func BenchmarkMockStore(b *testing.B) {
	store := NewMockStore(100, 50)
	defer store.Close()

	ctx := context.Background()
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		store.Attempt(ctx, "user:1")
	}
}

// BenchmarkMockStoreConcurrent benchmarks concurrent access to the mock store.
func BenchmarkMockStoreConcurrent(b *testing.B) {
	store := NewMockStore(100, 50)
	defer store.Close()

	var wg sync.WaitGroup
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			store.Attempt(context.Background(), "user:1")
		}()
	}
	wg.Wait()
}
