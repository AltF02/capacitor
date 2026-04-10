// Package token implements a token-bucket rate limiter backed
// by Valkey. Tokens accumulate over time up to a maximum capacity,
// and each request consumes one token. This allows controlled bursts
// while enforcing a steady average rate.
package token

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
	*ratelimit.Base
	config Config
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
		strconv.FormatFloat(l.config.RefillRate, 'f', -1, 64),
		strconv.FormatFloat(now, 'f', -1, 64),
	}

	res := tokenBucketScript.Exec(ctx, l.Client, []string{key}, args)
	allowedInt, remaining, retryAfterSecs, err := ratelimit.ParseResponse(res, "tokenbucket", l.Opts.Logger, uid)
	if err != nil {
		if ratelimit.IsFallbackError(err) {
			// Fallback result returned directly without recording metrics.
			// Metrics are only recorded for successful Valkey responses.
			return capacitor.FallbackResult(l.Opts.Fallback, l.config.Capacity, 1/l.config.RefillRate), err
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
