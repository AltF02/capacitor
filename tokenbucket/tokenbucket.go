// Package tokenbucket implements a token-bucket rate limiter backed
// by Valkey. Tokens accumulate over time up to a maximum capacity,
// and each request consumes one token. This allows controlled bursts
// while enforcing a steady average rate.
package tokenbucket

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
var luaTokenBucket string

var tokenBucketScript = valkey.NewLuaScript(luaTokenBucket)

// Config defines the parameters for a token-bucket rate limiter.
type Config struct {
	Capacity   int64         // maximum number of tokens the bucket can hold
	RefillRate float64       // tokens refilled per second
	KeyPrefix  string        // Valkey key prefix
	Timeout    time.Duration // per-operation Valkey timeout
}

// DefaultConfig returns a Config with sensible defaults for general use.
func DefaultConfig() Config {
	return Config{
		Capacity:   20,
		RefillRate: 5,
		KeyPrefix:  "capacitor:token",
		Timeout:    50 * time.Millisecond,
	}
}

type limiter struct {
	client valkey.Client
	config Config
	opts   capacitor.Options
}

var _ capacitor.Capacitor = (*limiter)(nil)

// New creates a token-bucket Capacitor backed by the given Valkey client.
func New(client valkey.Client, cfg Config, opts ...capacitor.Option) capacitor.Capacitor {
	if cfg.Capacity <= 0 {
		panic("capacitor: tokenbucket: capacity must be positive")
	}
	if cfg.RefillRate <= 0 {
		panic("capacitor: tokenbucket: refill rate must be positive")
	}
	if cfg.Timeout <= 0 {
		panic("capacitor: tokenbucket: timeout must be positive")
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
		strconv.FormatFloat(l.config.RefillRate, 'f', -1, 64),
		strconv.FormatFloat(now, 'f', -1, 64),
	}

	res := tokenBucketScript.Exec(ctx, l.client, []string{key}, args)
	if err := res.Error(); err != nil {
		l.opts.Logger.Error("valkey eval failed", "error", err, "uid", uid)
		return capacitor.FallbackResult(l.opts.Fallback, l.config.Capacity, 1/l.config.RefillRate),
			fmt.Errorf("capacitor: tokenbucket: eval: %w", err)
	}

	arr, err := res.ToArray()
	if err != nil {
		l.opts.Logger.Error("valkey response parse failed", "error", err)
		return capacitor.FallbackResult(l.opts.Fallback, l.config.Capacity, 1/l.config.RefillRate),
			fmt.Errorf("capacitor: tokenbucket: response: %w", err)
	}
	if len(arr) != 3 {
		l.opts.Logger.Error("unexpected eval response length", "len", len(arr))
		return capacitor.FallbackResult(l.opts.Fallback, l.config.Capacity, 1/l.config.RefillRate),
			fmt.Errorf("%w: expected 3 elements, got %d", capacitor.ErrEvalResponse, len(arr))
	}

	allowedInt, err := arr[0].ToInt64()
	if err != nil {
		return capacitor.Result{}, fmt.Errorf("capacitor: tokenbucket: parse allowed: %w", err)
	}
	remaining, err := arr[1].ToInt64()
	if err != nil {
		return capacitor.Result{}, fmt.Errorf("capacitor: tokenbucket: parse remaining: %w", err)
	}
	retryAfterSecs, err := arr[2].ToInt64()
	if err != nil {
		return capacitor.Result{}, fmt.Errorf("capacitor: tokenbucket: parse retry_after: %w", err)
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
