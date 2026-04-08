package testutil

import (
	"time"
	"unsafe"
)

func Btoi(b bool) int64 {
	return int64(*(*byte)(unsafe.Pointer(&b)))
}

type MetricsMock struct {
	Attempts  []string
	Denied    []string
	Latencies int
}

func (m *MetricsMock) RecordAttempt(key string)      { m.Attempts = append(m.Attempts, key) }
func (m *MetricsMock) RecordDenied(key string)       { m.Denied = append(m.Denied, key) }
func (m *MetricsMock) RecordLatency(_ time.Duration) { m.Latencies++ }
