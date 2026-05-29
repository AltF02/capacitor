package capacitor_test

import (
	"testing"
	"time"

	"codeberg.org/matthew/capacitor"
	"codeberg.org/matthew/capacitor/internal/testutil"

	"github.com/valkey-io/valkey-go"
)

func slidingWindowCounterCtor(t *testing.T, client valkey.Client, opts ...capacitor.Option) capacitor.Capacitor {
	t.Helper()
	return capacitor.NewSlidingWindowCounter(client, capacitor.NewSlidingWindowCounterDefaultConfig(), opts...)
}

func TestSlidingWindowCounter(t *testing.T) {
	testutil.RunAttemptCases(t, slidingWindowCounterCtor, map[string]testutil.AttemptCase{
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

func TestSlidingWindowCounter_Fallback(t *testing.T) {
	testutil.RunFallbackCases(t, slidingWindowCounterCtor, map[string]testutil.FallbackCase{
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

func TestSlidingWindowCounter_Metrics(t *testing.T) {
	testutil.RunMetricsCases(t, slidingWindowCounterCtor, map[string]testutil.MetricsCase{
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
