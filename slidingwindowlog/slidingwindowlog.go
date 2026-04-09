// Package slidingwindowlog implements a sliding-window log rate limiter
// backed by Valkey. It records the exact timestamp of every request
// in a sorted set, providing a true rolling window with no boundary
// bursts at the cost of higher memory usage.
package slidingwindowlog

import (
	"context"
	"crypto/rand"
	_ "embed"
	"fmt"
	"strconv"
	"time"

	"github.com/valkey-io/valkey-go"

	"codeberg.org/matthew/capacitor"
	"codeberg.org/matthew/capacitor/internal/ratelimit"
)

//go:embed script.lua
var luaSlidingWindowLog string

var slidingWindowLogScript = valkey.NewLuaScript(luaSlidingWindowLog)

// Config defines the parameters for a sliding-window log rate limiter.
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
		KeyPrefix: "capacitor:swlog",
		Timeout:   50 * time.Millisecond,
	}
}

type limiter struct {
	*ratelimit.Base
	config Config
}

var _ capacitor.Capacitor = (*limiter)(nil)

// New creates a sliding-window log Capacitor backed by the given Valkey client.
func New(client valkey.Client, cfg Config, opts ...capacitor.Option) capacitor.Capacitor {
	if cfg.Limit <= 0 {
		panic("capacitor: slidingwindowlog: limit must be positive")
	}
	if cfg.Window <= 0 {
		panic("capacitor: slidingwindowlog: window must be positive")
	}
	if cfg.Timeout <= 0 {
		panic("capacitor: slidingwindowlog: timeout must be positive")
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
	now := ratelimit.NowSeconds()
	member := fmt.Sprintf("%f:%x", now, randBytes())

	args := []string{
		strconv.FormatInt(l.config.Limit, 10),
		strconv.FormatFloat(windowSecs, 'f', -1, 64),
		strconv.FormatFloat(now, 'f', -1, 64),
		member,
	}

	res := slidingWindowLogScript.Exec(ctx, l.Client, []string{key}, args)
	allowedInt, remaining, retryAfterSecs, err := ratelimit.ParseResponse(res, "slidingwindowlog", l.Opts.Logger, uid)
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

func randBytes() string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d%x", time.Now().UnixNano(), b)
	}
	return fmt.Sprintf("%x", b)
}
