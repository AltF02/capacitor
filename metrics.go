package capacitor

import "time"

// MetricsCollector receives rate-limiter telemetry. Profile is the name
// from ProfileConfig; empty string for the default limiter.
type MetricsCollector interface {
	RecordAttempt(key, profile string)
	RecordDenied(key, profile string)
	RecordFallback(key, profile string)
	RecordLatency(d time.Duration, profile string)
}
