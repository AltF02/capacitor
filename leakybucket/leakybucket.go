// Package leakybucket implements a leaky-bucket (policing) rate limiter
// backed by Valkey. The bucket drains at a constant rate; requests that
// arrive when the bucket is full are denied.
package leakybucket

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
var luaLeakyBucket string

var leakyBucketScript = valkey.NewLuaScript(luaLeakyBucket)

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
		KeyPrefix: "capacitor:leaky",
		Timeout:   50 * time.Millisecond,
	}
}

type limiter struct {
	client valkey.Client
	config Config
	opts   capacitor.Options
}

var _ capacitor.Capacitor = (*limiter)(nil)

// New creates a leaky-bucket Capacitor backed by the given Valkey client.
func New(client valkey.Client, cfg Config, opts ...capacitor.Option) capacitor.Capacitor {
	if cfg.Capacity <= 0 {
		panic("capacitor: leakybucket: capacity must be positive")
	}
	if cfg.LeakRate <= 0 {
		panic("capacitor: leakybucket: leak rate must be positive")
	}
	if cfg.Timeout <= 0 {
		panic("capacitor: leakybucket: timeout must be positive")
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
	if err != nil {
		l.opts.Logger.Error("valkey response parse failed", "error", err)
		return capacitor.FallbackResult(l.opts.Fallback, l.config.Capacity, 1/l.config.LeakRate),
			fmt.Errorf("capacitor: leakybucket: response: %w", err)
	}
	if len(arr) != 3 {
		l.opts.Logger.Error("unexpected eval response length", "len", len(arr))
		return capacitor.FallbackResult(l.opts.Fallback, l.config.Capacity, 1/l.config.LeakRate),
			fmt.Errorf("%w: expected 3 elements, got %d", capacitor.ErrEvalResponse, len(arr))
	}

	allowedInt, err := arr[0].ToInt64()
	if err != nil {
		return capacitor.Result{}, fmt.Errorf("capacitor: leakybucket: parse allowed: %w", err)
	}
	remaining, err := arr[1].ToInt64()
	if err != nil {
		return capacitor.Result{}, fmt.Errorf("capacitor: leakybucket: parse remaining: %w", err)
	}
	retryAfterSecs, err := arr[2].ToInt64()
	if err != nil {
		return capacitor.Result{}, fmt.Errorf("capacitor: leakybucket: parse retry_after: %w", err)
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
		Limit:      l.config.Capacity,
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
