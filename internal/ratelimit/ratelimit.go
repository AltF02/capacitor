// Package ratelimit provides shared infrastructure for Capacitor algorithm
// implementations. It is internal and not part of the public API.
package ratelimit

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/valkey-io/valkey-go"

	"codeberg.org/matthew/capacitor"
)

// errFallback is the sentinel used to classify errors that should trigger
// a fallback result. It is unexported; callers use IsFallbackError.
var errFallback = errors.New("fallback")

// IsFallbackError reports whether err is a response-level error (eval
// failure, parse failure, wrong array length) that should cause the caller
// to return a FallbackResult. Field-level parse errors are not fallback
// errors and should be returned as-is.
func IsFallbackError(err error) bool {
	return errors.Is(err, errFallback)
}

// Base holds the fields common to every algorithm limiter. Embed *Base in
// a limiter struct to promote HealthCheck and Close.
type Base struct {
	Client valkey.Client
	Opts   capacitor.Options
}

// HealthCheck verifies connectivity to the backing Valkey instance.
func (b *Base) HealthCheck(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return b.Client.Do(ctx, b.Client.B().Ping().Build()).Error()
}

// Close releases the underlying Valkey client.
func (b *Base) Close() {
	b.Client.Close()
}

// CheckUID returns ErrEmptyUID if uid is empty.
func (b *Base) CheckUID(uid string) error {
	if uid == "" {
		return capacitor.ErrEmptyUID
	}
	return nil
}

// RecordMetrics records an attempt and, if denied, a denial via the
// configured MetricsCollector. It is a no-op when no collector is set.
func (b *Base) RecordMetrics(uid string, allowed bool) {
	if b.Opts.Metrics == nil {
		return
	}
	b.Opts.Metrics.RecordAttempt(uid)
	if !allowed {
		b.Opts.Metrics.RecordDenied(uid)
	}
}

// ApplyOptions applies opts to a default Options value and returns the result.
func ApplyOptions(opts []capacitor.Option) capacitor.Options {
	o := capacitor.DefaultOptions()
	for _, opt := range opts {
		opt(&o)
	}
	return o
}

// NowSeconds returns the current time as fractional seconds since the Unix
// epoch. All Lua scripts in this module expect timestamps in this format;
// using UnixMilli preserves sub-second precision while keeping the unit
// consistent with the scripts.
func NowSeconds() float64 {
	return float64(time.Now().UnixMilli()) / 1000.0
}

// BuildKey returns the Valkey key for the given prefix and uid.
//
//	BuildKey("capacitor:leaky", "user:1") → "capacitor:leaky:uid:user:1"
func BuildKey(prefix, uid string) string {
	return prefix + ":uid:" + uid
}

// BuildClusterKeys returns the previous and current window keys for use with
// multi-key Lua scripts on a Valkey Cluster. Both keys share a hash tag
// derived from base so they are guaranteed to map to the same cluster slot.
//
//	base      = BuildKey("capacitor:swcounter", "user:1")
//	windowNum = int64(now / windowSecs)
func BuildClusterKeys(base string, windowNum int64) (prev, curr string) {
	tag := "{" + base + "}:"
	return tag + strconv.FormatInt(windowNum-1, 10),
		tag + strconv.FormatInt(windowNum, 10)
}

// ParseResponse validates and parses the three-element Lua script response
// [{allowed}, {remaining}, {retry_after}].
//
// Response-level errors (eval failure, ToArray failure, wrong length) are
// wrapped with errFallback so callers can use IsFallbackError to decide
// whether to apply a FallbackResult. Field-level parse errors are returned
// unwrapped and should not trigger a fallback.
func ParseResponse(
	res valkey.ValkeyResult,
	name string,
	logger *slog.Logger,
	uid string,
) (allowed, remaining, retryAfter int64, err error) {
	if err = res.Error(); err != nil {
		logger.Error("valkey eval failed", "error", err, "uid", uid)
		return 0, 0, 0, fmt.Errorf("%w: capacitor: %s: eval: %w", errFallback, name, err)
	}

	arr, err := res.ToArray()
	if err != nil {
		logger.Error("valkey response parse failed", "error", err)
		return 0, 0, 0, fmt.Errorf("%w: capacitor: %s: response: %w", errFallback, name, err)
	}
	if len(arr) != 3 {
		logger.Error("unexpected eval response length", "len", len(arr))
		return 0, 0, 0, fmt.Errorf("%w: %w: expected 3 elements, got %d", errFallback, capacitor.ErrEvalResponse, len(arr))
	}

	allowed, err = arr[0].ToInt64()
	if err != nil {
		return 0, 0, 0, fmt.Errorf("capacitor: %s: parse allowed: %w", name, err)
	}
	remaining, err = arr[1].ToInt64()
	if err != nil {
		return 0, 0, 0, fmt.Errorf("capacitor: %s: parse remaining: %w", name, err)
	}
	retryAfter, err = arr[2].ToInt64()
	if err != nil {
		return 0, 0, 0, fmt.Errorf("capacitor: %s: parse retry_after: %w", name, err)
	}

	return allowed, remaining, retryAfter, nil
}
