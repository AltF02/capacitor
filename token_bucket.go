package capacitor

import (
	"context"
	"strconv"
	"time"

	"github.com/valkey-io/valkey-go"

	"codeberg.org/matthew/capacitor/internal/ratelimit"
	"codeberg.org/matthew/capacitor/internal/scripts"
)

// TokenBucketConfig defines the parameters for a token-bucket rate limiter.
type TokenBucketConfig struct {
	Capacity   int64         // maximum number of tokens the bucket can hold
	RefillRate float64       // tokens refilled per second
	KeyPrefix  string        // Valkey key prefix
	Timeout    time.Duration // per-operation Valkey timeout
}

// NewTokenBucketDefaultConfig returns a TokenBucketConfig with sensible defaults.
func NewTokenBucketDefaultConfig() TokenBucketConfig {
	return TokenBucketConfig{
		Capacity:   20,
		RefillRate: 5,
		KeyPrefix:  "capacitor:token",
		Timeout:    50 * time.Millisecond,
	}
}

type tokenBucket struct {
	*ratelimit.Base
	config TokenBucketConfig
}

var _ Capacitor = (*tokenBucket)(nil)

// NewTokenBucket creates a token-bucket Capacitor backed by the given Valkey client.
func NewTokenBucket(client valkey.Client, cfg TokenBucketConfig, opts ...Option) Capacitor {
	if cfg.Capacity <= 0 {
		panic("capacitor: tokenbucket: capacity must be positive")
	}
	if cfg.RefillRate <= 0 {
		panic("capacitor: tokenbucket: refill rate must be positive")
	}
	if cfg.Timeout <= 0 {
		panic("capacitor: tokenbucket: timeout must be positive")
	}
	return &tokenBucket{
		Base:   &ratelimit.Base{Client: client, Opts: ratelimit.ApplyOptions(opts)},
		config: cfg,
	}
}

func (l *tokenBucket) Attempt(ctx context.Context, uid string) (Result, error) {
	start := time.Now()
	if l.Opts.Metrics != nil {
		defer func() { l.Opts.Metrics.RecordLatency(time.Since(start)) }()
	}

	if err := l.CheckUID(uid); err != nil {
		return Result{}, err
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

	res := scripts.TokenBucket.Exec(ctx, l.Client, []string{key}, args)
	allowedInt, remaining, retryAfterSecs, err := ratelimit.ParseResponse(res, "tokenbucket", l.Opts.Logger, uid)
	if err != nil {
		if ratelimit.IsFallbackError(err) {
			return FallbackResult(l.Opts.Fallback, l.config.Capacity, 1/l.config.RefillRate), err
		}
		return Result{}, err
	}

	allowed := allowedInt == 1
	l.RecordMetrics(uid, allowed)

	return Result{
		Allowed:    allowed,
		Remaining:  remaining,
		Limit:      l.config.Capacity,
		RetryAfter: time.Duration(retryAfterSecs) * time.Second,
	}, nil
}
