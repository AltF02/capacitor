package timelog_test

import (
	"testing"
	"time"

	"codeberg.org/matthew/capacitor"
	"codeberg.org/matthew/capacitor/internal/testutil"
	"codeberg.org/matthew/capacitor/slidingwindow/timelog"

	"github.com/valkey-io/valkey-go"
)

func ctor(t *testing.T, client valkey.Client, opts ...capacitor.Option) capacitor.Capacitor {
	t.Helper()
	return timelog.New(client, timelog.DefaultConfig(), opts...)
}

func TestAttempt(t *testing.T) {
	testutil.RunAttemptCases(t, ctor, map[string]testutil.AttemptCase{
		"empty uid returns error": {
			UID:            "",
			MockValkey:     false,
			ExpectedResult: capacitor.Result{},
			ExpectedErr:    capacitor.ErrEmptyUID,
		},
		"request allowed": {
			UID:        "user:1",
			Allowed:    true,
			Remaining:  99,
			RetryAfter: 0,
			MockValkey: true,
			ExpectedResult: capacitor.Result{
				Allowed:   true,
				Remaining: 99,
				Limit:     100,
			},
		},
		"request denied": {
			UID:        "user:1",
			Allowed:    false,
			Remaining:  0,
			RetryAfter: 45,
			MockValkey: true,
			ExpectedResult: capacitor.Result{
				Allowed:    false,
				Remaining:  0,
				Limit:      100,
				RetryAfter: 45 * time.Second,
			},
		},
	})
}

func TestAttempt_Fallback(t *testing.T) {
	testutil.RunFallbackCases(t, ctor, map[string]testutil.FallbackCase{
		"fail open on valkey error": {
			Fallback: capacitor.FallbackFailOpen,
			ExpectedResult: capacitor.Result{
				Allowed:   true,
				Remaining: 0,
				Limit:     100,
			},
		},
		"fail closed on valkey error": {
			Fallback: capacitor.FallbackFailClosed,
			ExpectedResult: capacitor.Result{
				Allowed:    false,
				Remaining:  0,
				Limit:      100,
				RetryAfter: 60 * time.Second,
			},
		},
	})
}

func TestAttempt_Metrics(t *testing.T) {
	testutil.RunMetricsCases(t, ctor, map[string]testutil.MetricsCase{
		"allowed records attempt and latency": {
			UID:             "user:1",
			Allowed:         true,
			Remaining:       99,
			RetryAfter:      0,
			ExpectAttempts:  []string{"user:1"},
			ExpectDenied:    nil,
			ExpectLatencies: 1,
		},
		"denied records attempt, denied, and latency": {
			UID:             "user:2",
			Allowed:         false,
			Remaining:       0,
			RetryAfter:      45,
			ExpectAttempts:  []string{"user:2"},
			ExpectDenied:    []string{"user:2"},
			ExpectLatencies: 1,
		},
	})
}
