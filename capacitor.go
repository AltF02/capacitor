package capacitor

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"time"
)

var (
	ErrEmptyUID     = errors.New("capacitor: uid must not be empty")
	ErrEvalResponse = errors.New("capacitor: invalid eval response")
)

// Capacitor is the main rate limiter.
// It delegates to a Store implementation for the actual storage.
type Capacitor struct {
	store    Store
	config   Config
	logger   *slog.Logger
	fallback FallbackStrategy
	metrics  MetricsCollector
}

type Option func(*Capacitor)

type Result struct {
	Allowed    bool
	Remaining  int64
	Limit      int64
	RetryAfter time.Duration // Zero when allowed.
}

// New creates a new Capacitor with the given store and config.
// The store can be either a ValkeyStore or PostgresStore.
func New(store Store, cfg Config, opts ...Option) *Capacitor {
	s := &Capacitor{
		store:    store,
		config:   cfg,
		logger:   slog.Default(),
		fallback: FallbackFailOpen,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func WithLogger(logger *slog.Logger) Option {
	return func(s *Capacitor) { s.logger = logger }
}

// HealthCheck verifies connectivity to the store.
func (s *Capacitor) HealthCheck(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return s.store.HealthCheck(ctx)
}

// Close gracefully shuts down the store connection.
func (s *Capacitor) Close() error {
	return s.store.Close()
}

// Attempt checks whether the request identified by uid is allowed.
// On store errors it returns a fallback result and the underlying error.
func (s *Capacitor) Attempt(ctx context.Context, uid string) (Result, error) {
	start := time.Now()
	if s.metrics != nil {
		defer func() { s.metrics.RecordLatency(time.Since(start)) }()
	}

	if uid == "" {
		return Result{}, ErrEmptyUID
	}

	ctx, cancel := context.WithTimeout(ctx, s.config.Timeout)
	defer cancel()

	result, err := s.store.Attempt(ctx, uid)
	if err != nil {
		s.logger.Error("store attempt failed", "error", err, "uid", uid)
		return s.fallbackResult(), fmt.Errorf("capacitor: attempt: %w", err)
	}

	if s.metrics != nil {
		s.metrics.RecordAttempt(uid)
		if !result.Allowed {
			s.metrics.RecordDenied(uid)
		}
	}

	// Add retry-after based on leak rate
	result.RetryAfter = s.retryAfter(result.Allowed)
	result.Limit = s.config.Capacity

	return result, nil
}

func (s *Capacitor) retryAfter(allowed bool) time.Duration {
	if allowed {
		return 0
	}
	secs := math.Ceil(1 / s.config.LeakRate)
	return time.Duration(secs) * time.Second
}
