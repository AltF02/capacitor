// Package capacitor implements a leaky-bucket rate limiter backed by
// Valkey (Redis-compatible). Bucket logic runs atomically server-side
// via a Lua script. Ships as a library with drop-in net/http middleware.
package capacitor

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"time"

	"github.com/valkey-io/valkey-go"
)

var (
	// ErrEmptyUID is returned when Attempt is called with an empty uid.
	ErrEmptyUID = errors.New("capacitor: uid must not be empty")
	// ErrEvalResponse is returned when the Lua script returns an unexpected result.
	ErrEvalResponse = errors.New("capacitor: invalid eval response")
)

// Capacitor implements a leaky-bucket rate limiter using valkey-go.
type Capacitor struct {
	client   valkey.Client
	config   Config
	logger   *slog.Logger
	fallback FallbackStrategy
	metrics  MetricsCollector
}

// Option configures a Capacitor instance.
type Option func(*Capacitor)

// Result holds the outcome of a rate-limit check.
type Result struct {
	Allowed    bool
	Remaining  int64
	Limit      int64
	RetryAfter time.Duration
}

// New creates a Capacitor backed by the given Valkey client and configuration.
func New(client valkey.Client, cfg Config, opts ...Option) *Capacitor {
	c := &Capacitor{
		client:   client,
		config:   cfg,
		logger:   slog.Default(),
		fallback: FallbackFailOpen,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// WithLogger sets the logger used by the Capacitor for diagnostics.
func WithLogger(logger *slog.Logger) Option {
	return func(c *Capacitor) { c.logger = logger }
}

// HealthCheck verifies connectivity.
func (c *Capacitor) HealthCheck(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return c.client.Do(ctx, c.client.B().Ping().Build()).Error()
}

// Close gracefully shuts down the client.
func (c *Capacitor) Close() {
	c.client.Close()
}

// Attempt checks whether the request identified by uid is allowed.
// On Valkey errors it returns a fallback result and the underlying error.
func (c *Capacitor) Attempt(ctx context.Context, uid string) (Result, error) {
	start := time.Now()
	if c.metrics != nil {
		defer func() { c.metrics.RecordLatency(time.Since(start)) }()
	}

	if uid == "" {
		return Result{}, ErrEmptyUID
	}

	ctx, cancel := context.WithTimeout(ctx, c.config.Timeout)
	defer cancel()

	key := c.config.KeyPrefix + ":uid:" + uid
	now := float64(time.Now().UnixMilli()) / 1000.0

	args := []string{
		strconv.FormatInt(c.config.Capacity, 10),
		strconv.FormatFloat(c.config.LeakRate, 'f', -1, 64),
		strconv.FormatFloat(now, 'f', -1, 64),
	}

	res := leakyBucketScript.Exec(ctx, c.client, []string{key}, args)
	if err := res.Error(); err != nil {
		c.logger.Error("valkey eval failed", "error", err, "uid", uid)
		return c.fallbackResult(), fmt.Errorf("capacitor: eval: %w", err)
	}

	arr, err := res.ToArray()
	if err != nil || len(arr) != 2 {
		c.logger.Error("unexpected eval response", "error", err, "len", len(arr))
		return c.fallbackResult(), fmt.Errorf("%w: %v", ErrEvalResponse, err)
	}

	allowedInt, err := arr[0].ToInt64()
	if err != nil {
		return Result{}, fmt.Errorf("capacitor: parse allowed: %w", err)
	}
	remaining, _ := arr[1].ToInt64()

	allowed := allowedInt == 1

	if c.metrics != nil {
		c.metrics.RecordAttempt(uid)
		if !allowed {
			c.metrics.RecordDenied(uid)
		}
	}

	return Result{
		Allowed:    allowed,
		Remaining:  remaining,
		Limit:      c.config.Capacity,
		RetryAfter: c.retryAfter(allowed),
	}, nil
}

func (c *Capacitor) retryAfter(allowed bool) time.Duration {
	if allowed {
		return 0
	}
	secs := math.Ceil(1 / c.config.LeakRate)
	return time.Duration(secs) * time.Second
}
