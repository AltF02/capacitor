package capacitor

import (
	"math"
	"time"
)

type FallbackStrategy int

const (
	FallbackFailOpen FallbackStrategy = iota
	FallbackFailClosed
)

func WithFallback(fs FallbackStrategy) Option {
	return func(c *Capacitor) { c.fallback = fs }
}

// fallbackResult returns a degraded result based on the configured strategy.

func (c *Capacitor) fallbackResult() Result {
	if s.fallback == FallbackFailOpen {
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
