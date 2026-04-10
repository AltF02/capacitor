// Package leakybucket implements a leaky-bucket (policing) rate limiter
// backed by Valkey. The bucket drains at a constant rate; requests that
// arrive when the bucket is full are denied.
package leakybucket

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
	*ratelimit.Base
	config Config
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
	return &limiter{
		Base:   &ratelimit.Base{Client: client, Opts: ratelimit.ApplyOptions(opts)},
		config: cfg,
	}
}

func (l *limiter) Attempt(ctx context.Context, uid string) (capacitor.Result, error) {
	// Record total method wall-clock time, including validation
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
	now := ratelimit.NowSeconds()

	args := []string{
		strconv.FormatInt(l.config.Capacity, 10),
		strconv.FormatFloat(l.config.LeakRate, 'f', -1, 64),
		strconv.FormatFloat(now, 'f', -1, 64),
	}

	res := leakyBucketScript.Exec(ctx, l.Client, []string{key}, args)
	allowedInt, remaining, retryAfterSecs, err := ratelimit.ParseResponse(res, "leakybucket", l.Opts.Logger, uid)
	if err != nil {
		if ratelimit.IsFallbackError(err) {
			// Fallback result returned directly without recording metrics.
			// Metrics are only recorded for successful Valkey responses.
			return capacitor.FallbackResult(l.Opts.Fallback, l.config.Capacity, 1/l.config.LeakRate), err
		}
		return capacitor.Result{}, err
	}

	allowed := allowedInt == 1
	l.RecordMetrics(uid, allowed)

	return capacitor.Result{
		Allowed:    allowed,
		Remaining:  remaining,
		Limit:      l.config.Capacity,
		RetryAfter: time.Duration(retryAfterSecs) * time.Second,
	}, nil
}
