// Package slidingwindowcounter implements a sliding-window counter
// rate limiter backed by Valkey. It blends two fixed-window counters
// using a weighted average to approximate a true sliding window,
// offering near-exact accuracy with low memory usage.
package slidingwindowcounter

import (
	"context"
	_ "embed"
	"strconv"
	"time"

	"github.com/valkey-io/valkey-go"

	"codeberg.org/matthew/capacitor"
	"codeberg.org/matthew/capacitor/internal/ratelimit"
)

//go:embed script.lua
var luaSlidingWindowCounter string

var slidingWindowCounterScript = valkey.NewLuaScript(luaSlidingWindowCounter)

// Config defines the parameters for a sliding-window counter rate limiter.
type Config struct {
	Limit     int64         // maximum requests per window
	Window    time.Duration // window duration
	KeyPrefix string        // Valkey key prefix
	Timeout   time.Duration // per-operation Valkey timeout
}

// DefaultConfig returns a Config with sensible defaults for general use.
func DefaultConfig() Config {
	return Config{
		Limit:     100,
		Window:    time.Minute,
		KeyPrefix: "capacitor:swcounter",
		Timeout:   50 * time.Millisecond,
	}
}

type limiter struct {
	*ratelimit.Base
	config Config
}

var _ capacitor.Capacitor = (*limiter)(nil)

// New creates a sliding-window counter Capacitor backed by the given Valkey client.
func New(client valkey.Client, cfg Config, opts ...capacitor.Option) capacitor.Capacitor {
	if cfg.Limit <= 0 {
		panic("capacitor: slidingwindowcounter: limit must be positive")
	}
	if cfg.Window <= 0 {
		panic("capacitor: slidingwindowcounter: window must be positive")
	}
	if cfg.Timeout <= 0 {
		panic("capacitor: slidingwindowcounter: timeout must be positive")
	}
	return &limiter{
		Base:   &ratelimit.Base{Client: client, Opts: ratelimit.ApplyOptions(opts)},
		config: cfg,
	}
}

func (l *limiter) Attempt(ctx context.Context, uid string) (capacitor.Result, error) {
	start := time.Now()
	if l.Opts.Metrics != nil {
		defer func() { l.Opts.Metrics.RecordLatency(time.Since(start)) }()
	}

	if err := l.CheckUID(uid); err != nil {
		return capacitor.Result{}, err
	}

	ctx, cancel := context.WithTimeout(ctx, l.config.Timeout)
	defer cancel()

	windowSecs := l.config.Window.Seconds()
	now := ratelimit.NowSeconds()
	windowNum := int64(now / windowSecs)

	baseKey := ratelimit.BuildKey(l.config.KeyPrefix, uid)
	prevKey, currKey := ratelimit.BuildClusterKeys(baseKey, windowNum)

	args := []string{
		strconv.FormatInt(l.config.Limit, 10),
		strconv.FormatFloat(windowSecs, 'f', -1, 64),
		strconv.FormatFloat(now, 'f', -1, 64),
	}

	res := slidingWindowCounterScript.Exec(ctx, l.Client, []string{prevKey, currKey}, args)
	allowedInt, remaining, retryAfterSecs, err := ratelimit.ParseResponse(res, "slidingwindowcounter", l.Opts.Logger, uid)
	if err != nil {
		if ratelimit.IsFallbackError(err) {
			return capacitor.FallbackResult(l.Opts.Fallback, l.config.Limit, windowSecs), err
		}
		return capacitor.Result{}, err
	}

	allowed := allowedInt == 1
	l.RecordMetrics(uid, allowed)

	return capacitor.Result{
		Allowed:    allowed,
		Remaining:  remaining,
		Limit:      l.config.Limit,
		RetryAfter: time.Duration(retryAfterSecs) * time.Second,
	}, nil
}
