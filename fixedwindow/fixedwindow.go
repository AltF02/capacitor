// Package fixedwindow implements a fixed-window counter rate limiter
// backed by Valkey. Requests are counted within discrete time windows;
// the counter resets when the window expires.
package fixedwindow

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
var luaFixedWindow string

var fixedWindowScript = valkey.NewLuaScript(luaFixedWindow)

// Config defines the parameters for a fixed-window rate limiter.
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
		KeyPrefix: "capacitor:fixedwin",
		Timeout:   50 * time.Millisecond,
	}
}

type limiter struct {
	*ratelimit.Base
	config Config
}

var _ capacitor.Capacitor = (*limiter)(nil)

// New creates a fixed-window Capacitor backed by the given Valkey client.
func New(client valkey.Client, cfg Config, opts ...capacitor.Option) capacitor.Capacitor {
	if cfg.Limit <= 0 {
		panic("capacitor: fixedwindow: limit must be positive")
	}
	if cfg.Window <= 0 {
		panic("capacitor: fixedwindow: window must be positive")
	}
	if cfg.Timeout <= 0 {
		panic("capacitor: fixedwindow: timeout must be positive")
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

	key := ratelimit.BuildKey(l.config.KeyPrefix, uid)
	windowSecs := l.config.Window.Seconds()

	args := []string{
		strconv.FormatInt(l.config.Limit, 10),
		strconv.FormatFloat(windowSecs, 'f', -1, 64),
	}

	res := fixedWindowScript.Exec(ctx, l.Client, []string{key}, args)
	allowedInt, remaining, retryAfterSecs, err := ratelimit.ParseResponse(res, "fixedwindow", l.Opts.Logger, uid)
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
