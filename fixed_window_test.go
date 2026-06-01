package capacitor_test

import (
	"testing"
	"time"

	"codeberg.org/matthew/capacitor"
	"codeberg.org/matthew/capacitor/internal/testutil"

	"github.com/valkey-io/valkey-go"
)

func fixedWindowCtor(t *testing.T, client valkey.Client, opts ...capacitor.Option) capacitor.Capacitor {
	t.Helper()
	return capacitor.NewFixedWindow(client, capacitor.NewFixedWindowDefaultConfig(), opts...)
}

func TestFixedWindow(t *testing.T) {
	testutil.RunAttemptCases(t, fixedWindowCtor, map[string]testutil.AttemptCase{
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
			RetryAfter: 30,
			MockValkey: true,
			ExpectedResult: capacitor.Result{
				Allowed:    false,
				Remaining:  0,
				Limit:      100,
				RetryAfter: 30 * time.Second,
			},
		},
	})
}

func TestFixedWindow_Fallback(t *testing.T) {
	testutil.RunFallbackCases(t, fixedWindowCtor, map[string]testutil.FallbackCase{
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
