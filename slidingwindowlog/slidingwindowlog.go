// Package slidingwindowlog implements a sliding-window log rate limiter
// backed by Valkey. It records the exact timestamp of every request
// in a sorted set, providing a true rolling window with no boundary
// bursts at the cost of higher memory usage.
package slidingwindowlog

import (
	"context"
	"crypto/rand"
	"fmt"
	"strconv"
	"time"

	"github.com/valkey-io/valkey-go"

	"codeberg.org/matthew/capacitor"
)

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
	client valkey.Client
	config Config
	opts   capacitor.Options
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
	now := float64(time.Now().UnixMilli()) / 1000.0
	member := fmt.Sprintf("%f:%x", now, randBytes())

	args := []string{
		strconv.FormatInt(l.config.Limit, 10),
		strconv.FormatFloat(windowSecs, 'f', -1, 64),
		strconv.FormatFloat(now, 'f', -1, 64),
		member,
	}

	res := slidingWindowLogScript.Exec(ctx, l.client, []string{key}, args)
	if err := res.Error(); err != nil {
		l.opts.Logger.Error("valkey eval failed", "error", err, "uid", uid)
		return capacitor.FallbackResult(l.opts.Fallback, l.config.Limit, windowSecs),
			fmt.Errorf("capacitor: slidingwindowlog: eval: %w", err)
	}

	arr, err := res.ToArray()
	if err != nil {
		l.opts.Logger.Error("valkey response parse failed", "error", err)
		return capacitor.FallbackResult(l.opts.Fallback, l.config.Limit, windowSecs),
			fmt.Errorf("capacitor: slidingwindowlog: response: %w", err)
	}
	if len(arr) != 3 {
		l.opts.Logger.Error("unexpected eval response length", "len", len(arr))
		return capacitor.FallbackResult(l.opts.Fallback, l.config.Limit, windowSecs),
			fmt.Errorf("%w: expected 3 elements, got %d", capacitor.ErrEvalResponse, len(arr))
	}

	allowedInt, err := arr[0].ToInt64()
	if err != nil {
		return capacitor.Result{}, fmt.Errorf("capacitor: slidingwindowlog: parse allowed: %w", err)
	}
	remaining, err := arr[1].ToInt64()
	if err != nil {
		return capacitor.Result{}, fmt.Errorf("capacitor: slidingwindowlog: parse remaining: %w", err)
	}
	retryAfterSecs, err := arr[2].ToInt64()
	if err != nil {
		return capacitor.Result{}, fmt.Errorf("capacitor: slidingwindowlog: parse retry_after: %w", err)
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

func randBytes() string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d%x", time.Now().UnixNano(), b)
	}
	return fmt.Sprintf("%x", b)
}

const luaSlidingWindowLog = `
local key = KEYS[1]
local limit = tonumber(ARGV[1])
local window = tonumber(ARGV[2])
local now = tonumber(ARGV[3])
local member = ARGV[4]

local window_start = now - window
valkey.call('ZREMRANGEBYSCORE', key, '-inf', window_start)

local count = valkey.call('ZCARD', key)

local allowed = 0
local remaining = 0
local retry_after = 0

if count < limit then
    valkey.call('ZADD', key, now, member)
    valkey.call('EXPIRE', key, math.ceil(window) + 1)
    count = count + 1
    allowed = 1
    remaining = limit - count
else
    remaining = 0
    local oldest = valkey.call('ZRANGE', key, 0, 0, 'WITHSCORES')
    if #oldest >= 2 then
        local oldest_time = tonumber(oldest[2])
        retry_after = math.ceil(oldest_time + window - now)
        if retry_after < 1 then retry_after = 1 end
    else
        retry_after = math.ceil(window)
        if retry_after < 1 then retry_after = 1 end
    end
end

return { allowed, remaining, retry_after }
`

var slidingWindowLogScript = valkey.NewLuaScript(luaSlidingWindowLog)
