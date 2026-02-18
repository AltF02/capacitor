package capacitor_test

import "time"

type metricsMock struct {
	attempts  []string
	denied    []string
	latencies int
}

func (m *metricsMock) RecordAttempt(key string)      { m.attempts = append(m.attempts, key) }
func (m *metricsMock) RecordDenied(key string)       { m.denied = append(m.denied, key) }
func (m *metricsMock) RecordLatency(_ time.Duration) { m.latencies++ }
