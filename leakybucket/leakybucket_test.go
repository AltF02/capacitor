package leakybucket_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"codeberg.org/matthew/capacitor"
	"codeberg.org/matthew/capacitor/internal/testutil"
	"codeberg.org/matthew/capacitor/leakybucket"

	"github.com/google/go-cmp/cmp"
	"github.com/valkey-io/valkey-go/mock"
	"go.uber.org/mock/gomock"
)

func TestAttempt(t *testing.T) {
	cfg := leakybucket.DefaultConfig()

	cases := map[string]struct {
		uid            string
		allowed        bool
		remaining      int
		retryAfter     int
		mockValkey     bool
		expectedResult capacitor.Result
		expectedErr    error
	}{
		"empty uid returns error": {
			uid:            "",
			mockValkey:     false,
			expectedResult: capacitor.Result{},
			expectedErr:    capacitor.ErrEmptyUID,
		},
		"request allowed": {
			uid:        "user:1",
			allowed:    true,
			remaining:  9,
			retryAfter: 0,
			mockValkey: true,
			expectedResult: capacitor.Result{
				Allowed:   true,
				Remaining: 9,
				Limit:     20,
			},
		},
		"request denied": {
			uid:        "user:1",
			allowed:    false,
			remaining:  0,
			retryAfter: 1,
			mockValkey: true,
			expectedResult: capacitor.Result{
				Allowed:    false,
				Remaining:  0,
				Limit:      20,
				RetryAfter: 1 * time.Second,
			},
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			client := mock.NewClient(ctrl)

			if c.mockValkey {
				client.EXPECT().
					Do(gomock.Any(), gomock.Any()).
					Return(mock.Result(mock.ValkeyArray(
						mock.ValkeyInt64(testutil.Btoi(c.allowed)),
						mock.ValkeyInt64(int64(c.remaining)),
						mock.ValkeyInt64(int64(c.retryAfter)),
					)))
			}

			lim := leakybucket.New(client, cfg)
			actualRes, err := lim.Attempt(context.Background(), c.uid)

			if !errors.Is(err, c.expectedErr) {
				t.Fatalf("Attempt() error; got = %v, want = %v", err, c.expectedErr)
			}

			if diff := cmp.Diff(c.expectedResult, actualRes); diff != "" {
				t.Errorf("capacitor.Result mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestAttempt_Fallback(t *testing.T) {
	cfg := leakybucket.DefaultConfig()

	cases := map[string]struct {
		fallback       capacitor.FallbackStrategy
		expectedResult capacitor.Result
	}{
		"fail open on valkey error": {
			fallback: capacitor.FallbackFailOpen,
			expectedResult: capacitor.Result{
				Allowed:   true,
				Remaining: 0,
				Limit:     20,
			},
		},
		"fail closed on valkey error": {
			fallback: capacitor.FallbackFailClosed,
			expectedResult: capacitor.Result{
				Allowed:    false,
				Remaining:  0,
				Limit:      20,
				RetryAfter: 1 * time.Second,
			},
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			client := mock.NewClient(ctrl)

			client.EXPECT().
				Do(gomock.Any(), gomock.Any()).
				Return(mock.Result(mock.ValkeyError("ERR test error")))

			lim := leakybucket.New(client, cfg,
				capacitor.WithFallback(c.fallback),
				capacitor.WithLogger(slog.Default()),
			)
			actualRes, err := lim.Attempt(context.Background(), "user:1")

			if err == nil {
				t.Fatal("expected error, got nil")
			}

			if diff := cmp.Diff(c.expectedResult, actualRes); diff != "" {
				t.Errorf("capacitor.Result mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestAttempt_Metrics(t *testing.T) {
	cfg := leakybucket.DefaultConfig()

	cases := map[string]struct {
		uid             string
		allowed         bool
		remaining       int
		retryAfter      int
		expectAttempts  []string
		expectDenied    []string
		expectLatencies int
	}{
		"allowed records attempt and latency": {
			uid:             "user:1",
			allowed:         true,
			remaining:       9,
			retryAfter:      0,
			expectAttempts:  []string{"user:1"},
			expectDenied:    nil,
			expectLatencies: 1,
		},
		"denied records attempt, denied, and latency": {
			uid:             "user:2",
			allowed:         false,
			remaining:       0,
			retryAfter:      1,
			expectAttempts:  []string{"user:2"},
			expectDenied:    []string{"user:2"},
			expectLatencies: 1,
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			client := mock.NewClient(ctrl)

			client.EXPECT().
				Do(gomock.Any(), gomock.Any()).
				Return(mock.Result(mock.ValkeyArray(
					mock.ValkeyInt64(testutil.Btoi(c.allowed)),
					mock.ValkeyInt64(int64(c.remaining)),
					mock.ValkeyInt64(int64(c.retryAfter)),
				)))

			mMock := &testutil.MetricsMock{}
			lim := leakybucket.New(client, cfg, capacitor.WithMetrics(mMock))
			_, err := lim.Attempt(context.Background(), c.uid)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if diff := cmp.Diff(c.expectAttempts, mMock.Attempts); diff != "" {
				t.Errorf("attempts mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(c.expectDenied, mMock.Denied); diff != "" {
				t.Errorf("denied mismatch (-want +got):\n%s", diff)
			}
			if mMock.Latencies != c.expectLatencies {
				t.Errorf("latencies = %d, want %d", mMock.Latencies, c.expectLatencies)
			}
		})
	}
}
