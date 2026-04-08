package capacitor

import "time"

// MetricsCollector receives rate-limiter telemetry data.
type MetricsCollector interface {
	RecordAttempt(key string)
	RecordDenied(key string)
	RecordLatency(d time.Duration)
}
