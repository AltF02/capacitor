package capacitor

import (
	"context"
	"strconv"
	"time"

	"github.com/valkey-io/valkey-go"

	"codeberg.org/matthew/capacitor/internal/ratelimit"
	"codeberg.org/matthew/capacitor/internal/scripts"
)

// FixedWindowConfig defines the parameters for a fixed-window rate limiter.
type FixedWindowConfig struct {
	Limit     int64         // maximum requests per window
	Window    time.Duration // window duration
	KeyPrefix string        // Valkey key prefix
	Timeout   time.Duration // per-operation Valkey timeout
}

// NewFixedWindowDefaultConfig returns a FixedWindowConfig with sensible defaults.
func NewFixedWindowDefaultConfig() FixedWindowConfig {
	return FixedWindowConfig{
		Limit:     100,
		Window:    time.Minute,
		KeyPrefix: "capacitor:fixedwin",
		Timeout:   50 * time.Millisecond,
	}
}

type fixedWindow struct {
	*ratelimit.Base
	config FixedWindowConfig
}

var _ Capacitor = (*fixedWindow)(nil)

// NewFixedWindow creates a fixed-window Capacitor backed by the given Valkey client.
func NewFixedWindow(client valkey.Client, cfg FixedWindowConfig, opts ...Option) Capacitor {
	if cfg.Limit <= 0 {
		panic("capacitor: fixedwindow: limit must be positive")
	}
	if cfg.Window <= 0 {
		panic("capacitor: fixedwindow: window must be positive")
	}
	if cfg.Timeout <= 0 {
		panic("capacitor: fixedwindow: timeout must be positive")
	}
	return &fixedWindow{
		Base:   &ratelimit.Base{Client: client, Opts: ratelimit.ApplyOptions(opts)},
		config: cfg,
	}
}

func (l *fixedWindow) Attempt(ctx context.Context, uid string) (Result, error) {
	if err := l.CheckUID(uid); err != nil {
		return Result{}, err
	}

	ctx, cancel := context.WithTimeout(ctx, l.config.Timeout)
	defer cancel()

	key := ratelimit.BuildKey(l.config.KeyPrefix, uid)
	windowSecs := l.config.Window.Seconds()

	args := []string{
		strconv.FormatInt(l.config.Limit, 10),
		strconv.FormatFloat(windowSecs, 'f', -1, 64),
	}

	res := scripts.FixedWindow.Exec(ctx, l.Client, []string{key}, args)
	allowedInt, remaining, retryAfterSecs, err := ratelimit.ParseResponse(res, "fixedwindow", l.Opts.Logger, uid)
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
