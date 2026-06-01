package capacitor

import (
	"context"
	"strconv"
	"time"

	"github.com/valkey-io/valkey-go"

	"codeberg.org/matthew/capacitor/internal/ratelimit"
	"codeberg.org/matthew/capacitor/internal/scripts"
)

// LeakyBucketConfig defines the parameters for a leaky-bucket rate limiter.
type LeakyBucketConfig struct {
	Capacity  int64         // maximum number of requests the bucket can hold
	LeakRate  float64       // requests drained per second
	KeyPrefix string        // Valkey key prefix for bucket storage
	Timeout   time.Duration // per-operation Valkey timeout
}

// NewLeakyBucketDefaultConfig returns a LeakyBucketConfig with sensible defaults.
func NewLeakyBucketDefaultConfig() LeakyBucketConfig {
	return LeakyBucketConfig{
		Capacity:  20,
		LeakRate:  5,
		KeyPrefix: "capacitor:leaky",
		Timeout:   50 * time.Millisecond,
	}
}

type leakyBucket struct {
	*ratelimit.Base
	config LeakyBucketConfig
}

var _ Capacitor = (*leakyBucket)(nil)

// NewLeakyBucket creates a leaky-bucket Capacitor backed by the given Valkey client.
func NewLeakyBucket(client valkey.Client, cfg LeakyBucketConfig, opts ...Option) Capacitor {
	if cfg.Capacity <= 0 {
		panic("capacitor: leakybucket: capacity must be positive")
	}
	if cfg.LeakRate <= 0 {
		panic("capacitor: leakybucket: leak rate must be positive")
	}
	if cfg.Timeout <= 0 {
		panic("capacitor: leakybucket: timeout must be positive")
	}
	return &leakyBucket{
		Base:   &ratelimit.Base{Client: client, Opts: ratelimit.ApplyOptions(opts)},
		config: cfg,
	}
}

func (l *leakyBucket) Attempt(ctx context.Context, uid string) (Result, error) {
	if err := l.CheckUID(uid); err != nil {
		return Result{}, err
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

	res := scripts.LeakyBucket.Exec(ctx, l.Client, []string{key}, args)
	allowedInt, remaining, retryAfterSecs, err := ratelimit.ParseResponse(res, "leakybucket", l.Opts.Logger, uid)
	if err != nil {
		if ratelimit.IsFallbackError(err) {
			return FallbackResult(l.Opts.Fallback, l.config.Capacity, 1/l.config.LeakRate), err
		}
		return Result{}, err
	}

	return Result{
		Allowed:    allowedInt == 1,
		Remaining:  remaining,
		Limit:      l.config.Capacity,
		RetryAfter: time.Duration(retryAfterSecs) * time.Second,
	}, nil
}
