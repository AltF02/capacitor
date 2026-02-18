package capacitor

import "time"

type MetricsCollector interface {
	RecordAttempt(key string)
	RecordDenied(key string)
	RecordLatency(d time.Duration)
}

func WithMetrics(m MetricsCollector) Option {
	return func(c *Capacitor) { c.metrics = m }
}
