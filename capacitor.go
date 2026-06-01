// Package capacitor provides rate limiting backed by Valkey
// (Redis-compatible). All algorithm implementations are in this package.
//
// Usage:
//
//	lim := capacitor.NewLeakyBucket(client, capacitor.NewLeakyBucketDefaultConfig())
//	mw := capacitor.NewMiddleware(lim)
//	http.Handle("/", mw(myHandler))
package capacitor

import (
	"context"
	"math"
	"time"

	"codeberg.org/matthew/capacitor/internal/ratelimit"
)

// Re-exported types from internal/ratelimit.
type (
	Options          = ratelimit.Options
	Option           = ratelimit.Option
	FallbackStrategy = ratelimit.FallbackStrategy
)

// Re-exported constants.
const (
	FallbackFailOpen   = ratelimit.FallbackFailOpen
	FallbackFailClosed = ratelimit.FallbackFailClosed
)

// Re-exported errors.
var (
	ErrEmptyUID     = ratelimit.ErrEmptyUID
	ErrEvalResponse = ratelimit.ErrEvalResponse
)

// Re-exported option constructors.
var (
	DefaultOptions = ratelimit.DefaultOptions
	WithLogger     = ratelimit.WithLogger
	WithFallback   = ratelimit.WithFallback
)

// Result holds the outcome of a rate-limit check.
type Result struct {
	Allowed    bool
	Remaining  int64
	Limit      int64
	RetryAfter time.Duration
}

// FallbackResult returns a degraded Result based on the given strategy.
// Algorithm implementations call this when Valkey is unreachable.
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
