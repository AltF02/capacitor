package capacitor

import (
	"math"
	"time"
)

// FallbackStrategy determines how the limiter behaves when Valkey is unreachable.
type FallbackStrategy int

const (
	// FallbackFailOpen allows requests when Valkey is unreachable.
	FallbackFailOpen FallbackStrategy = iota
	// FallbackFailClosed denies requests when Valkey is unreachable.
	FallbackFailClosed
)

// WithFallback sets the strategy used when Valkey is unreachable.
func WithFallback(s FallbackStrategy) Option {
	return func(c *Capacitor) { c.fallback = s }
}

// fallbackResult returns a degraded result based on the configured strategy.
func (c *Capacitor) fallbackResult() Result {
	if c.fallback == FallbackFailOpen {
		return Result{Allowed: true, Remaining: 0, Limit: c.config.Capacity}
	}

	retry := math.Ceil(1 / c.config.LeakRate)
	return Result{
		Allowed:    false,
		Remaining:  0,
		Limit:      c.config.Capacity,
		RetryAfter: time.Duration(retry) * time.Second,
	}
}
