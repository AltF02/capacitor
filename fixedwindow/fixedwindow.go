// Package fixedwindow implements a fixed-window counter rate limiter
// backed by Valkey. Requests are counted within discrete time windows;
// the counter resets when the window expires.
package fixedwindow

import (
	"context"
	_ "embed"
	"fmt"
	"strconv"
	"time"

	"github.com/valkey-io/valkey-go"

	"codeberg.org/matthew/capacitor"
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
	client valkey.Client
	config Config
	opts   capacitor.Options
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
	o := capacitor.DefaultOptions()
	for _, opt := range opts {
		opt(&o)
	}
	return &limiter{client: client, config: cfg, opts: o}
}

func (l *limiter) Attempt(ctx context.Context, uid string) (capacitor.Result, error) {
	start := time.Now()
	if l.opts.Metrics != nil {
		defer func() { l.opts.Metrics.RecordLatency(time.Since(start)) }()
	}

	if uid == "" {
		return capacitor.Result{}, capacitor.ErrEmptyUID
	}

	ctx, cancel := context.WithTimeout(ctx, l.config.Timeout)
	defer cancel()

	key := l.config.KeyPrefix + ":uid:" + uid
	windowSecs := l.config.Window.Seconds()

	args := []string{
		strconv.FormatInt(l.config.Limit, 10),
		strconv.FormatFloat(windowSecs, 'f', -1, 64),
	}

	res := fixedWindowScript.Exec(ctx, l.client, []string{key}, args)
	if err := res.Error(); err != nil {
		l.opts.Logger.Error("valkey eval failed", "error", err, "uid", uid)
		return capacitor.FallbackResult(l.opts.Fallback, l.config.Limit, windowSecs),
			fmt.Errorf("capacitor: fixedwindow: eval: %w", err)
	}

	arr, err := res.ToArray()
	if err != nil {
		l.opts.Logger.Error("valkey response parse failed", "error", err)
		return capacitor.FallbackResult(l.opts.Fallback, l.config.Limit, windowSecs),
			fmt.Errorf("capacitor: fixedwindow: response: %w", err)
	}
	if len(arr) != 3 {
		l.opts.Logger.Error("unexpected eval response length", "len", len(arr))
		return capacitor.FallbackResult(l.opts.Fallback, l.config.Limit, windowSecs),
			fmt.Errorf("%w: expected 3 elements, got %d", capacitor.ErrEvalResponse, len(arr))
	}

	allowedInt, err := arr[0].ToInt64()
	if err != nil {
		return capacitor.Result{}, fmt.Errorf("capacitor: fixedwindow: parse allowed: %w", err)
	}
	remaining, err := arr[1].ToInt64()
	if err != nil {
		return capacitor.Result{}, fmt.Errorf("capacitor: fixedwindow: parse remaining: %w", err)
	}
	retryAfterSecs, err := arr[2].ToInt64()
	if err != nil {
		return capacitor.Result{}, fmt.Errorf("capacitor: fixedwindow: parse retry_after: %w", err)
	}

	allowed := allowedInt == 1

	if l.opts.Metrics != nil {
		l.opts.Metrics.RecordAttempt(uid)
		if !allowed {
			l.opts.Metrics.RecordDenied(uid)
		}
	}

	return capacitor.Result{
		Allowed:    allowed,
		Remaining:  remaining,
		Limit:      l.config.Limit,
		RetryAfter: time.Duration(retryAfterSecs) * time.Second,
	}, nil
}

func (l *limiter) HealthCheck(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return l.client.Do(ctx, l.client.B().Ping().Build()).Error()
}

func (l *limiter) Close() {
	l.client.Close()
}
