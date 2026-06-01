package capacitor_test

import (
	"testing"
	"time"

	"codeberg.org/matthew/capacitor"
	"codeberg.org/matthew/capacitor/internal/testutil"

	"github.com/valkey-io/valkey-go"
)

func leakyBucketCtor(t *testing.T, client valkey.Client, opts ...capacitor.Option) capacitor.Capacitor {
	t.Helper()
	return capacitor.NewLeakyBucket(client, capacitor.NewLeakyBucketDefaultConfig(), opts...)
}

func TestLeakyBucket(t *testing.T) {
	testutil.RunAttemptCases(t, leakyBucketCtor, map[string]testutil.AttemptCase{
		"empty uid returns error": {
			UID:            "",
			MockValkey:     false,
			ExpectedResult: capacitor.Result{},
			ExpectedErr:    capacitor.ErrEmptyUID,
		},
		"request allowed": {
			UID:        "user:1",
			Allowed:    true,
			Remaining:  9,
			RetryAfter: 0,
			MockValkey: true,
			ExpectedResult: capacitor.Result{
				Allowed:   true,
				Remaining: 9,
				Limit:     20,
			},
		},
		"request denied": {
			UID:        "user:1",
			Allowed:    false,
			Remaining:  0,
			RetryAfter: 1,
			MockValkey: true,
			ExpectedResult: capacitor.Result{
				Allowed:    false,
				Remaining:  0,
				Limit:      20,
				RetryAfter: 1 * time.Second,
			},
		},
	})
}

func TestLeakyBucket_Fallback(t *testing.T) {
	testutil.RunFallbackCases(t, leakyBucketCtor, map[string]testutil.FallbackCase{
		"fail open on valkey error": {
			Fallback: capacitor.FallbackFailOpen,
			ExpectedResult: capacitor.Result{
				Allowed:   true,
				Remaining: 0,
				Limit:     20,
			},
		},
		"fail closed on valkey error": {
			Fallback: capacitor.FallbackFailClosed,
			ExpectedResult: capacitor.Result{
				Allowed:    false,
				Remaining:  0,
				Limit:      20,
				RetryAfter: 1 * time.Second,
			},
		},
	})
}
