// Package capacitor provides rate limiting backed by Valkey
// (Redis-compatible). Algorithm implementations live in sub-packages;
// all satisfy the Capacitor interface.
//
// Typical usage:
//
//	limiter := leakybucket.New(client, leakybucket.DefaultConfig())
//	mw := capacitor.NewMiddleware(limiter)
//	http.Handle("/", mw(myHandler))
package capacitor

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"time"
)

var (
	// ErrEmptyUID is returned when Attempt is called with an empty uid.
	ErrEmptyUID = errors.New("capacitor: uid must not be empty")
	// ErrEvalResponse is returned when the Lua script returns an unexpected result.
	ErrEvalResponse = errors.New("capacitor: invalid eval response")
)

// Capacitor checks whether a request is allowed under a rate-limiting
// policy. Implementations must be safe for concurrent use.
type Capacitor interface {
	// Attempt checks whether the request identified by uid is allowed.
	// On Valkey errors it returns a fallback result and the underlying error.
	Attempt(ctx context.Context, uid string) (Result, error)
	// HealthCheck verifies connectivity to the backing store.
	HealthCheck(ctx context.Context) error
	// Close releases resources held by the Capacitor.
	Close()
}

// Result holds the outcome of a rate-limit check.
type Result struct {
	Allowed    bool
	Remaining  int64
	Limit      int64
	RetryAfter time.Duration
}

// FallbackStrategy determines how the limiter behaves when Valkey is unreachable.
type FallbackStrategy int

const (
	// FallbackFailOpen allows requests when Valkey is unreachable.
	FallbackFailOpen FallbackStrategy = iota
	// FallbackFailClosed denies requests when Valkey is unreachable.
	FallbackFailClosed
)

// FallbackResult returns a degraded Result based on the given strategy.
// Algorithm sub-packages call this when Valkey is unreachable.
func FallbackResult(strategy FallbackStrategy, limit int64, retryAfterSecs float64) Result {
	if strategy == FallbackFailOpen {
		return Result{Allowed: true, Remaining: 0, Limit: limit}
	}

	retry := math.Ceil(retryAfterSecs)
	return Result{
		Allowed:    false,
		Remaining:  0,
		Limit:      limit,
		RetryAfter: time.Duration(retry) * time.Second,
	}
}

// Options holds cross-cutting configuration shared by all algorithm
// implementations. Sub-packages embed this in their internal state.
type Options struct {
	Logger   *slog.Logger
	Fallback FallbackStrategy
	Metrics  MetricsCollector
}

// DefaultOptions returns Options with sensible defaults.
func DefaultOptions() Options {
	return Options{
		Logger:   slog.Default(),
		Fallback: FallbackFailOpen,
	}
}

// Option configures cross-cutting behavior for any Capacitor implementation.
type Option func(*Options)

// WithLogger sets the logger used for diagnostics.
func WithLogger(logger *slog.Logger) Option {
	return func(o *Options) { o.Logger = logger }
}

// WithFallback sets the strategy used when Valkey is unreachable.
func WithFallback(s FallbackStrategy) Option {
	return func(o *Options) { o.Fallback = s }
}

// WithMetrics enables telemetry recording via the given collector.
func WithMetrics(m MetricsCollector) Option {
	return func(o *Options) { o.Metrics = m }
}
