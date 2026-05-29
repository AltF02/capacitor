// Package metrics defines the MetricsCollector interface for rate-limiter
// telemetry. It is internal so that both the root capacitor package and
// internal/ratelimit can import it without circular dependencies.
package metrics

import "time"

// MetricsCollector receives rate-limiter telemetry data.
type MetricsCollector interface {
	RecordAttempt(key string)
	RecordDenied(key string)
	RecordLatency(d time.Duration)
}
