package ratelimit_test

import (
	"errors"
	"log/slog"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/valkey-io/valkey-go"
	"github.com/valkey-io/valkey-go/mock"

	"codeberg.org/matthew/capacitor"
	"codeberg.org/matthew/capacitor/internal/ratelimit"
)

func TestBuildKey(t *testing.T) {
	tests := map[string]struct {
		prefix string
		uid    string
		want   string
	}{
		"standard key": {
			prefix: "capacitor:leaky",
			uid:    "user:1",
			want:   "capacitor:leaky:uid:user:1",
		},
		"empty uid": {
			prefix: "capacitor:leaky",
			uid:    "",
			want:   "capacitor:leaky:uid:",
		},
		"complex uid": {
			prefix: "capacitor:token",
			uid:    "ip:192.168.1.1:api:/v1/users",
			want:   "capacitor:token:uid:ip:192.168.1.1:api:/v1/users",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got := ratelimit.BuildKey(tt.prefix, tt.uid)
			if got != tt.want {
				t.Errorf("BuildKey(%q, %q) = %q, want %q", tt.prefix, tt.uid, got, tt.want)
			}
		})
	}
}

func TestBuildClusterKeys(t *testing.T) {
	tests := map[string]struct {
		base      string
		windowNum int64
		wantPrev  string
		wantCurr  string
	}{
		"window 100": {
			base:      "capacitor:swcounter:uid:user:1",
			windowNum: 100,
			wantPrev:  "{capacitor:swcounter:uid:user:1}:99",
			wantCurr:  "{capacitor:swcounter:uid:user:1}:100",
		},
		"window 0": {
			base:      "capacitor:swcounter:uid:user:1",
			windowNum: 0,
			wantPrev:  "{capacitor:swcounter:uid:user:1}:-1",
			wantCurr:  "{capacitor:swcounter:uid:user:1}:0",
		},
		"window 1": {
			base:      "capacitor:swcounter:uid:user:1",
			windowNum: 1,
			wantPrev:  "{capacitor:swcounter:uid:user:1}:0",
			wantCurr:  "{capacitor:swcounter:uid:user:1}:1",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			gotPrev, gotCurr := ratelimit.BuildClusterKeys(tt.base, tt.windowNum)
			if gotPrev != tt.wantPrev {
				t.Errorf("prev = %q, want %q", gotPrev, tt.wantPrev)
			}
			if gotCurr != tt.wantCurr {
				t.Errorf("curr = %q, want %q", gotCurr, tt.wantCurr)
			}
		})
	}
}

func TestApplyOptions(t *testing.T) {
	t.Run("no options returns defaults", func(t *testing.T) {
		got := ratelimit.ApplyOptions(nil)
		want := capacitor.DefaultOptions()
		if diff := cmp.Diff(want.Fallback, got.Fallback); diff != "" {
			t.Errorf("Fallback mismatch (-want +got):\n%s", diff)
		}
		if got.Logger == nil {
			t.Error("expected non-nil default logger")
		}
	})

	t.Run("WithFallback overrides default", func(t *testing.T) {
		got := ratelimit.ApplyOptions([]capacitor.Option{
			capacitor.WithFallback(capacitor.FallbackFailClosed),
		})
		if got.Fallback != capacitor.FallbackFailClosed {
			t.Errorf("Fallback = %v, want FallbackFailClosed", got.Fallback)
		}
	})
}

func TestNowSeconds(t *testing.T) {
	now := ratelimit.NowSeconds()
	if now <= 0 {
		t.Errorf("NowSeconds() = %f, want positive value", now)
	}
	// Sanity: after Unix epoch year 2020 (1577836800) and before 2100 (4102444800).
	if now < 1577836800 || now > 4102444800 {
		t.Errorf("NowSeconds() = %f, outside reasonable range", now)
	}
}

func TestIsFallbackError(t *testing.T) {
	t.Run("nil is not fallback", func(t *testing.T) {
		if ratelimit.IsFallbackError(nil) {
			t.Error("IsFallbackError(nil) = true, want false")
		}
	})

	t.Run("unrelated error is not fallback", func(t *testing.T) {
		if ratelimit.IsFallbackError(errors.New("random")) {
			t.Error("IsFallbackError(random) = true, want false")
		}
	})
}

func makeResult(msg valkey.ValkeyMessage) valkey.ValkeyResult {
	return mock.Result(msg)
}

func TestParseResponse(t *testing.T) {
	logger := slog.Default()

	tests := map[string]struct {
		result         valkey.ValkeyResult
		wantAllowed    int64
		wantRemaining  int64
		wantRetryAfter int64
		wantErr        bool
		wantFallback   bool
		wantEvalResp   bool
	}{
		"valid allowed response": {
			result: makeResult(mock.ValkeyArray(
				mock.ValkeyInt64(1),
				mock.ValkeyInt64(9),
				mock.ValkeyInt64(0),
			)),
			wantAllowed:    1,
			wantRemaining:  9,
			wantRetryAfter: 0,
		},
		"valid denied response": {
			result: makeResult(mock.ValkeyArray(
				mock.ValkeyInt64(0),
				mock.ValkeyInt64(0),
				mock.ValkeyInt64(5),
			)),
			wantAllowed:    0,
			wantRemaining:  0,
			wantRetryAfter: 5,
		},
		"eval error triggers fallback": {
			result:       makeResult(mock.ValkeyError("ERR script error")),
			wantErr:      true,
			wantFallback: true,
		},
		"wrong array length (2 elements) triggers fallback": {
			result: makeResult(mock.ValkeyArray(
				mock.ValkeyInt64(1),
				mock.ValkeyInt64(9),
			)),
			wantErr:      true,
			wantFallback: true,
			wantEvalResp: true,
		},
		"empty array triggers fallback": {
			result:       makeResult(mock.ValkeyArray()),
			wantErr:      true,
			wantFallback: true,
			wantEvalResp: true,
		},
		"wrong array length (4 elements) triggers fallback": {
			result: makeResult(mock.ValkeyArray(
				mock.ValkeyInt64(1),
				mock.ValkeyInt64(9),
				mock.ValkeyInt64(0),
				mock.ValkeyInt64(42),
			)),
			wantErr:      true,
			wantFallback: true,
			wantEvalResp: true,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			allowed, remaining, retryAfter, err := ratelimit.ParseResponse(tt.result, "test", logger, "user:1")

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.wantFallback && !ratelimit.IsFallbackError(err) {
					t.Errorf("expected fallback error, got: %v", err)
				}
				if tt.wantEvalResp && !errors.Is(err, capacitor.ErrEvalResponse) {
					t.Errorf("expected ErrEvalResponse in chain, got: %v", err)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if allowed != tt.wantAllowed {
				t.Errorf("allowed = %d, want %d", allowed, tt.wantAllowed)
			}
			if remaining != tt.wantRemaining {
				t.Errorf("remaining = %d, want %d", remaining, tt.wantRemaining)
			}
			if retryAfter != tt.wantRetryAfter {
				t.Errorf("retryAfter = %d, want %d", retryAfter, tt.wantRetryAfter)
			}
		})
	}
}

func TestParseResponse_FieldParseErrors(t *testing.T) {
	logger := slog.Default()

	// Field-level parse errors should NOT be fallback errors.
	// This tests the deliberate design choice: if the array structure is
	// correct (3 elements) but a field value can't be parsed to int64,
	// that's a data error, not a connectivity error.
	tests := map[string]struct {
		result valkey.ValkeyResult
	}{
		"allowed field is string": {
			result: makeResult(mock.ValkeyArray(
				mock.ValkeyString("not-a-number"),
				mock.ValkeyInt64(9),
				mock.ValkeyInt64(0),
			)),
		},
		"remaining field is string": {
			result: makeResult(mock.ValkeyArray(
				mock.ValkeyInt64(1),
				mock.ValkeyString("not-a-number"),
				mock.ValkeyInt64(0),
			)),
		},
		"retry_after field is string": {
			result: makeResult(mock.ValkeyArray(
				mock.ValkeyInt64(1),
				mock.ValkeyInt64(9),
				mock.ValkeyString("not-a-number"),
			)),
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			_, _, _, err := ratelimit.ParseResponse(tt.result, "test", logger, "user:1")

			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if ratelimit.IsFallbackError(err) {
				t.Errorf("field parse error should NOT be a fallback error, got: %v", err)
			}
		})
	}
}
