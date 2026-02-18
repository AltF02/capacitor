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
	ErrEmptyUID     = errors.New("capacitor: uid must not be empty")
	ErrEvalResponse = errors.New("capacitor: invalid eval response")
)

// capacitor implements leaky bucket using valkey-go native client.
type Capacitor struct {
	client   valkey.Client
	config   Config
	logger   *slog.Logger
	fallback FallbackStrategy
	metrics  MetricsCollector
}

type Option func(*Capacitor)

type Result struct {
	Allowed    bool
	Remaining  int64
	Limit      int64
	RetryAfter time.Duration // Zero when allowed.
}

func New(client valkey.Client, cfg Config, opts ...Option) *Capacitor {
	s := &Capacitor{
		client:   client,
		config:   cfg,
		logger:   slog.Default(),
		fallback: FallbackFailOpen,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func WithLogger(logger *slog.Logger) Option {
	return func(s *Capacitor) { s.logger = logger }
}

// HealthCheck verifies connectivity.
func (s *Capacitor) HealthCheck(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return s.client.Do(ctx, s.client.B().Ping().Build()).Error()
}

// Close gracefully shuts down the client.
func (s *Capacitor) Close() {
	s.client.Close()
}

// Attempt checks whether the request identified by uid is allowed.
// On Valkey errors it returns a fallback result and the underlying error.
func (s *Capacitor) Attempt(ctx context.Context, uid string) (Result, error) {
	start := time.Now()
	if s.metrics != nil {
		defer func() { s.metrics.RecordLatency(time.Since(start)) }()
	}

	if uid == "" {
		return Result{}, ErrEmptyUID
	}

	ctx, cancel := context.WithTimeout(ctx, s.config.Timeout)
	defer cancel()

	key := s.config.KeyPrefix + ":" + uid
	now := float64(time.Now().UnixMilli()) / 1000.0

	args := []string{
		strconv.FormatInt(s.config.Capacity, 10),
		strconv.FormatFloat(s.config.LeakRate, 'f', -1, 64),
		strconv.FormatFloat(now, 'f', -1, 64),
	}

	res := leakyBucketScript.Exec(ctx, s.client, []string{key}, args)
	if err := res.Error(); err != nil {
		s.logger.Error("valkey eval failed", "error", err, "uid", uid)
		return s.fallbackResult(), fmt.Errorf("capacitor: eval: %w", err)
	}

	arr, err := res.ToArray()
	if err != nil || len(arr) != 2 {
		s.logger.Error("unexpected eval response", "error", err, "len", len(arr))
		return s.fallbackResult(), fmt.Errorf("%w: %v", ErrEvalResponse, err)
	}

	allowedInt, err := arr[0].ToInt64()
	if err != nil {
		return Result{}, fmt.Errorf("capacitor: parse allowed: %w", err)
	}
	remaining, _ := arr[1].ToInt64() // 0 on error is safe

	allowed := allowedInt == 1

	if s.metrics != nil {
		s.metrics.RecordAttempt(uid)
		if !allowed {
			s.metrics.RecordDenied(uid)
		}
	}

	return Result{
		Allowed:    allowed,
		Remaining:  remaining,
		Limit:      s.config.Capacity,
		RetryAfter: s.retryAfter(allowed),
	}, nil
}

func (s *Capacitor) retryAfter(allowed bool) time.Duration {
	if allowed {
		return 0
	}
	secs := math.Ceil(1 / s.config.LeakRate)
	return time.Duration(secs) * time.Second
}
