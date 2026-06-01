package capacitor

import (
	"context"
	"strconv"
	"time"

	"github.com/valkey-io/valkey-go"

	"codeberg.org/matthew/capacitor/internal/ratelimit"
	"codeberg.org/matthew/capacitor/internal/scripts"
)

// SlidingWindowCounterConfig defines the parameters for a sliding-window counter rate limiter.
type SlidingWindowCounterConfig struct {
	Limit     int64         // maximum requests per window
	Window    time.Duration // window duration
	KeyPrefix string        // Valkey key prefix
	Timeout   time.Duration // per-operation Valkey timeout
}

// NewSlidingWindowCounterDefaultConfig returns a SlidingWindowCounterConfig with sensible defaults.
func NewSlidingWindowCounterDefaultConfig() SlidingWindowCounterConfig {
	return SlidingWindowCounterConfig{
		Limit:     100,
		Window:    time.Minute,
		KeyPrefix: "capacitor:swcounter",
		Timeout:   50 * time.Millisecond,
	}
}

type slidingWindowCounter struct {
	*ratelimit.Base
	config SlidingWindowCounterConfig
}

var _ Capacitor = (*slidingWindowCounter)(nil)

// NewSlidingWindowCounter creates a sliding-window counter Capacitor backed by the given Valkey client.
func NewSlidingWindowCounter(client valkey.Client, cfg SlidingWindowCounterConfig, opts ...Option) Capacitor {
	if cfg.Limit <= 0 {
		panic("capacitor: slidingwindowcounter: limit must be positive")
	}
	if cfg.Window <= 0 {
		panic("capacitor: slidingwindowcounter: window must be positive")
	}
	if cfg.Timeout <= 0 {
		panic("capacitor: slidingwindowcounter: timeout must be positive")
	}
	return &slidingWindowCounter{
		Base:   &ratelimit.Base{Client: client, Opts: ratelimit.ApplyOptions(opts)},
		config: cfg,
	}
}

func (l *slidingWindowCounter) Attempt(ctx context.Context, uid string) (Result, error) {
	if err := l.CheckUID(uid); err != nil {
		return Result{}, err
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

	res := scripts.SlidingWindowCounter.Exec(ctx, l.Client, []string{prevKey, currKey}, args)
	allowedInt, remaining, retryAfterSecs, err := ratelimit.ParseResponse(res, "slidingwindowcounter", l.Opts.Logger, uid)
	if err != nil {
		if ratelimit.IsFallbackError(err) {
			return FallbackResult(l.Opts.Fallback, l.config.Limit, windowSecs), err
		}
		return Result{}, err
	}

	return Result{
		Allowed:    allowedInt == 1,
		Remaining:  remaining,
		Limit:      l.config.Limit,
		RetryAfter: time.Duration(retryAfterSecs) * time.Second,
	}, nil
}
