package capacitor

import (
	"context"
	"crypto/rand"
	"fmt"
	"strconv"
	"time"

	"github.com/valkey-io/valkey-go"

	"codeberg.org/matthew/capacitor/internal/ratelimit"
	"codeberg.org/matthew/capacitor/internal/scripts"
)

// SlidingTimeLogConfig defines the parameters for a sliding-window log rate limiter.
type SlidingTimeLogConfig struct {
	Limit     int64         // maximum requests per window
	Window    time.Duration // window duration
	KeyPrefix string        // Valkey key prefix
	Timeout   time.Duration // per-operation Valkey timeout
}

// NewSlidingTimeLogDefaultConfig returns a SlidingTimeLogConfig with sensible defaults.
func NewSlidingTimeLogDefaultConfig() SlidingTimeLogConfig {
	return SlidingTimeLogConfig{
		Limit:     100,
		Window:    time.Minute,
		KeyPrefix: "capacitor:swlog",
		Timeout:   50 * time.Millisecond,
	}
}

type slidingTimeLog struct {
	*ratelimit.Base
	config SlidingTimeLogConfig
}

var _ Capacitor = (*slidingTimeLog)(nil)

// NewSlidingTimeLog creates a sliding-window log Capacitor backed by the given Valkey client.
func NewSlidingTimeLog(client valkey.Client, cfg SlidingTimeLogConfig, opts ...Option) Capacitor {
	if cfg.Limit <= 0 {
		panic("capacitor: slidingwindowlog: limit must be positive")
	}
	if cfg.Window <= 0 {
		panic("capacitor: slidingwindowlog: window must be positive")
	}
	if cfg.Timeout <= 0 {
		panic("capacitor: slidingwindowlog: timeout must be positive")
	}
	return &slidingTimeLog{
		Base:   &ratelimit.Base{Client: client, Opts: ratelimit.ApplyOptions(opts)},
		config: cfg,
	}
}

func (l *slidingTimeLog) Attempt(ctx context.Context, uid string) (Result, error) {
	if err := l.CheckUID(uid); err != nil {
		return Result{}, err
	}

	ctx, cancel := context.WithTimeout(ctx, l.config.Timeout)
	defer cancel()

	key := ratelimit.BuildKey(l.config.KeyPrefix, uid)
	windowSecs := l.config.Window.Seconds()
	now := ratelimit.NowSeconds()
	member := fmt.Sprintf("%f:%x", now, randBytes())

	args := []string{
		strconv.FormatInt(l.config.Limit, 10),
		strconv.FormatFloat(windowSecs, 'f', -1, 64),
		strconv.FormatFloat(now, 'f', -1, 64),
		member,
	}

	res := scripts.SlidingWindowLog.Exec(ctx, l.Client, []string{key}, args)
	allowedInt, remaining, retryAfterSecs, err := ratelimit.ParseResponse(res, "slidingwindowlog", l.Opts.Logger, uid)
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

func randBytes() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x", b)
}
