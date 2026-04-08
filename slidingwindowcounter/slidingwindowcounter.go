// Package slidingwindowcounter implements a sliding-window counter
// rate limiter backed by Valkey. It blends two fixed-window counters
// using a weighted average to approximate a true sliding window,
// offering near-exact accuracy with low memory usage.
package slidingwindowcounter

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/valkey-io/valkey-go"

	"codeberg.org/matthew/capacitor"
)

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
	client valkey.Client
	config Config
	opts   capacitor.Options
}

// New creates a sliding-window counter Capacitor backed by the given Valkey client.
func New(client valkey.Client, cfg Config, opts ...capacitor.Option) capacitor.Capacitor {
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

	windowSecs := l.config.Window.Seconds()
	now := float64(time.Now().UnixMilli()) / 1000.0
	windowNum := int64(now / windowSecs)

	baseKey := l.config.KeyPrefix + ":uid:" + uid
	prevKey := "{" + baseKey + "}:" + strconv.FormatInt(windowNum-1, 10)
	currKey := "{" + baseKey + "}:" + strconv.FormatInt(windowNum, 10)

	args := []string{
		strconv.FormatInt(l.config.Limit, 10),
		strconv.FormatFloat(windowSecs, 'f', -1, 64),
		strconv.FormatFloat(now, 'f', -1, 64),
	}

	res := slidingWindowCounterScript.Exec(ctx, l.client, []string{prevKey, currKey}, args)
	if err := res.Error(); err != nil {
		l.opts.Logger.Error("valkey eval failed", "error", err, "uid", uid)
		return capacitor.FallbackResult(l.opts.Fallback, l.config.Limit, windowSecs),
			fmt.Errorf("capacitor: slidingwindowcounter: eval: %w", err)
	}

	arr, err := res.ToArray()
	if err != nil {
		l.opts.Logger.Error("valkey response parse failed", "error", err)
		return capacitor.FallbackResult(l.opts.Fallback, l.config.Limit, windowSecs),
			fmt.Errorf("capacitor: slidingwindowcounter: response: %w", err)
	}
	if len(arr) != 3 {
		l.opts.Logger.Error("unexpected eval response length", "len", len(arr))
		return capacitor.FallbackResult(l.opts.Fallback, l.config.Limit, windowSecs),
			fmt.Errorf("%w: expected 3 elements, got %d", capacitor.ErrEvalResponse, len(arr))
	}

	allowedInt, err := arr[0].ToInt64()
	if err != nil {
		return capacitor.Result{}, fmt.Errorf("capacitor: slidingwindowcounter: parse allowed: %w", err)
	}
	remaining, err := arr[1].ToInt64()
	if err != nil {
		return capacitor.Result{}, fmt.Errorf("capacitor: slidingwindowcounter: parse remaining: %w", err)
	}
	retryAfterSecs, err := arr[2].ToInt64()
	if err != nil {
		return capacitor.Result{}, fmt.Errorf("capacitor: slidingwindowcounter: parse retry_after: %w", err)
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

const luaSlidingWindowCounter = `
local prev_key = KEYS[1]
local curr_key = KEYS[2]
local limit = tonumber(ARGV[1])
local window = tonumber(ARGV[2])
local now = tonumber(ARGV[3])

local window_num = math.floor(now / window)
local elapsed = (now / window) - window_num

local prev_count = tonumber(valkey.call('GET', prev_key)) or 0
local curr_count = tonumber(valkey.call('GET', curr_key)) or 0

local estimated = math.floor(prev_count * (1 - elapsed) + curr_count)

local allowed = 0
local remaining = 0
local retry_after = 0

if estimated < limit then
    curr_count = valkey.call('INCR', curr_key)
    if curr_count == 1 then
        valkey.call('EXPIRE', curr_key, window * 2)
    end
    remaining = math.max(0, limit - curr_count)
    allowed = 1
else
    remaining = 0
    local ttl = valkey.call('PTTL', curr_key)
    retry_after = math.ceil(ttl / 1000)
    if retry_after < 1 then retry_after = 1 end
end

return { allowed, remaining, retry_after }
`

var slidingWindowCounterScript = valkey.NewLuaScript(luaSlidingWindowCounter)
