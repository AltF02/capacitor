package testutil

import (
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/valkey-io/valkey-go"
	"github.com/valkey-io/valkey-go/mock"
	"go.uber.org/mock/gomock"

	"codeberg.org/matthew/capacitor"
)

// Btoi converts a bool to int64. Test-only.
func Btoi(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

// MetricsMock implements capacitor.MetricsCollector and records all calls
// for assertion in tests.
type MetricsMock struct {
	Attempts  []string
	Denied    []string
	Latencies int
}

func (m *MetricsMock) RecordAttempt(key string)      { m.Attempts = append(m.Attempts, key) }
func (m *MetricsMock) RecordDenied(key string)       { m.Denied = append(m.Denied, key) }
func (m *MetricsMock) RecordLatency(_ time.Duration) { m.Latencies++ }

// Constructor is a function that creates a Capacitor from a Valkey client
// and optional options. Test runners pass per-case options (e.g. WithFallback)
// through the variadic parameter.
type Constructor func(t *testing.T, client valkey.Client, opts ...capacitor.Option) capacitor.Capacitor

// AttemptCase describes a single Attempt() test case.
type AttemptCase struct {
	UID            string
	Allowed        bool
	Remaining      int
	RetryAfter     int
	MockValkey     bool
	ExpectedResult capacitor.Result
	ExpectedErr    error
}

// RunAttemptCases runs a table of AttemptCase subtests using ctor to build
// each limiter. Each subtest gets its own gomock controller and mock client.
func RunAttemptCases(t *testing.T, ctor Constructor, cases map[string]AttemptCase) {
	t.Helper()
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			client := mock.NewClient(ctrl)

			if c.MockValkey {
				client.EXPECT().
					Do(gomock.Any(), gomock.Any()).
					Return(mock.Result(mock.ValkeyArray(
						mock.ValkeyInt64(Btoi(c.Allowed)),
						mock.ValkeyInt64(int64(c.Remaining)),
						mock.ValkeyInt64(int64(c.RetryAfter)),
					)))
			}

			lim := ctor(t, client)
			got, err := lim.Attempt(t.Context(), c.UID)

			if !errors.Is(err, c.ExpectedErr) {
				t.Fatalf("Attempt() error: got %v, want %v", err, c.ExpectedErr)
			}
			if diff := cmp.Diff(c.ExpectedResult, got); diff != "" {
				t.Errorf("Result mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// FallbackCase describes a single fallback behaviour test case.
type FallbackCase struct {
	Fallback       capacitor.FallbackStrategy
	ExpectedResult capacitor.Result
}

// RunFallbackCases runs a table of FallbackCase subtests. Each subtest
// configures the limiter with the given FallbackStrategy and a Valkey mock
// that returns an error, then asserts the fallback result.
func RunFallbackCases(t *testing.T, ctor Constructor, cases map[string]FallbackCase) {
	t.Helper()
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			client := mock.NewClient(ctrl)

			client.EXPECT().
				Do(gomock.Any(), gomock.Any()).
				Return(mock.Result(mock.ValkeyError("ERR test error")))

			lim := ctor(t, client,
				capacitor.WithFallback(c.Fallback),
				capacitor.WithLogger(slog.Default()),
			)
			got, err := lim.Attempt(t.Context(), "user:1")

			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if diff := cmp.Diff(c.ExpectedResult, got); diff != "" {
				t.Errorf("Result mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// MetricsCase describes a single metrics recording test case.
type MetricsCase struct {
	UID             string
	Allowed         bool
	Remaining       int
	RetryAfter      int
	ExpectAttempts  []string
	ExpectDenied    []string
	ExpectLatencies int
}

// RunMetricsCases runs a table of MetricsCase subtests. Each subtest injects
// a MetricsMock and asserts recorded attempts, denials, and latency counts.
func RunMetricsCases(t *testing.T, ctor Constructor, cases map[string]MetricsCase) {
	t.Helper()
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			client := mock.NewClient(ctrl)

			client.EXPECT().
				Do(gomock.Any(), gomock.Any()).
				Return(mock.Result(mock.ValkeyArray(
					mock.ValkeyInt64(Btoi(c.Allowed)),
					mock.ValkeyInt64(int64(c.Remaining)),
					mock.ValkeyInt64(int64(c.RetryAfter)),
				)))

			m := &MetricsMock{}
			lim := ctor(t, client, capacitor.WithMetrics(m))
			if _, err := lim.Attempt(t.Context(), c.UID); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if diff := cmp.Diff(c.ExpectAttempts, m.Attempts); diff != "" {
				t.Errorf("attempts mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(c.ExpectDenied, m.Denied); diff != "" {
				t.Errorf("denied mismatch (-want +got):\n%s", diff)
			}
			if m.Latencies != c.ExpectLatencies {
				t.Errorf("latencies = %d, want %d", m.Latencies, c.ExpectLatencies)
			}
		})
	}
}
