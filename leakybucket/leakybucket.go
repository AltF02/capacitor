// Package leakybucket implements a leaky-bucket (policing) rate limiter
// backed by Valkey. The bucket drains at a constant rate; requests that
// arrive when the bucket is full are denied.
package leakybucket

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/valkey-io/valkey-go"

	"codeberg.org/matthew/capacitor"
)

// Config defines the parameters for a leaky-bucket rate limiter.
type Config struct {
	Capacity  int64         // maximum number of requests the bucket can hold
	LeakRate  float64       // requests drained per second
	KeyPrefix string        // Valkey key prefix for bucket storage
	Timeout   time.Duration // per-operation Valkey timeout
}

// DefaultConfig returns a Config with sensible defaults for general use.
func DefaultConfig() Config {
	return Config{
		Capacity:  20,
		LeakRate:  5,
		KeyPrefix: "capacitor",
		Timeout:   50 * time.Millisecond,
	}
}

type limiter struct {
	client valkey.Client
	config Config
	opts   capacitor.Options
}

// New creates a leaky-bucket Capacitor backed by the given Valkey client.
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

	key := l.config.KeyPrefix + ":uid:" + uid
	now := float64(time.Now().UnixMilli()) / 1000.0

	args := []string{
		strconv.FormatInt(l.config.Capacity, 10),
		strconv.FormatFloat(l.config.LeakRate, 'f', -1, 64),
		strconv.FormatFloat(now, 'f', -1, 64),
	}

	res := leakyBucketScript.Exec(ctx, l.client, []string{key}, args)
	if err := res.Error(); err != nil {
		l.opts.Logger.Error("valkey eval failed", "error", err, "uid", uid)
		return capacitor.FallbackResult(l.opts.Fallback, l.config.Capacity, 1/l.config.LeakRate),
			fmt.Errorf("capacitor: leakybucket: eval: %w", err)
	}

	arr, err := res.ToArray()
	if err != nil || len(arr) != 2 {
		l.opts.Logger.Error("unexpected eval response", "error", err, "len", len(arr))
		return capacitor.FallbackResult(l.opts.Fallback, l.config.Capacity, 1/l.config.LeakRate),
			fmt.Errorf("%w: %v", capacitor.ErrEvalResponse, err)
	}

	allowedInt, err := arr[0].ToInt64()
	if err != nil {
		return capacitor.Result{}, fmt.Errorf("capacitor: leakybucket: parse allowed: %w", err)
	}
	remaining, _ := arr[1].ToInt64()

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
		Limit:      l.config.Capacity,
		RetryAfter: l.retryAfter(allowed),
	}, nil
}

func (l *limiter) retryAfter(allowed bool) time.Duration {
	if allowed {
		return 0
	}
	secs := math.Ceil(1 / l.config.LeakRate)
	return time.Duration(secs) * time.Second
}

func (l *limiter) HealthCheck(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return l.client.Do(ctx, l.client.B().Ping().Build()).Error()
}

func (l *limiter) Close() {
	l.client.Close()
}

// IMPORTANT: This script expects 'now' in the same time unit as leak_rate
// (if leak_rate is per second, now should be in seconds, not milliseconds).
const luaLeakyBucket = `
local key = KEYS[1]
local capacity = tonumber(ARGV[1])
local leak_rate = tonumber(ARGV[2])
local now = tonumber(ARGV[3])

local data = valkey.call('HGETALL', key)
local level = 0
local last_leak = now

if #data > 0 then
  local fields = {}
  for i = 1, #data, 2 do
    fields[data[i]] = data[i + 1]
  end
  level = tonumber(fields['level']) or 0
  last_leak = tonumber(fields['last_leak']) or now
end

local elapsed = now - last_leak
local leaked = elapsed * leak_rate
level = math.max(0, level - leaked)

local allowed = 0
local remaining = math.max(0, math.floor(capacity - level))

if level + 1 <= capacity then
  level = level + 1
  remaining = math.max(0, math.floor(capacity - level))
  allowed = 1
end

valkey.call('HSET', key, 'level', tostring(level), 'last_leak', tostring(now))
valkey.call('EXPIRE', key, math.ceil(capacity / leak_rate) * 2)

return { allowed, remaining }
`

var leakyBucketScript = valkey.NewLuaScript(luaLeakyBucket)
