package capacitor

import (
	"context"
	"strconv"
	"time"

	"github.com/valkey-io/valkey-go"
)

// ValkeyStore implements the Store interface using Valkey (Redis-compatible).
type ValkeyStore struct {
	client valkey.Client
	config Config
}

var _ Store = (*ValkeyStore)(nil)

// NewValkeyStore creates a new ValkeyStore.
func NewValkeyStore(client valkey.Client, cfg Config) *ValkeyStore {
	return &ValkeyStore{
		client: client,
		config: cfg,
	}
}

// HealthCheck verifies connectivity to Valkey.
func (s *ValkeyStore) HealthCheck(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return s.client.Do(ctx, s.client.B().Ping().Build()).Error()
}

// Close gracefully shuts down the client.
func (s *ValkeyStore) Close() error {
	s.client.Close()
	return nil
}

// Attempt checks whether the request identified by uid is allowed.
func (s *ValkeyStore) Attempt(ctx context.Context, uid string) (Result, error) {
	key := s.config.KeyPrefix + ":uid:" + uid
	now := float64(time.Now().UnixMilli()) / 1000.0

	args := []string{
		strconv.FormatInt(s.config.Capacity, 10),
		strconv.FormatFloat(s.config.LeakRate, 'f', -1, 64),
		strconv.FormatFloat(now, 'f', -1, 64),
	}

	res := leakyBucketScript.Exec(ctx, s.client, []string{key}, args)
	if err := res.Error(); err != nil {
		return Result{}, err
	}

	arr, err := res.ToArray()
	if err != nil || len(arr) != 2 {
		return Result{}, ErrEvalResponse
	}

	allowedInt, err := arr[0].ToInt64()
	if err != nil {
		return Result{}, err
	}
	remaining, _ := arr[1].ToInt64()

	allowed := allowedInt == 1

	return Result{
		Allowed:    allowed,
		Remaining: remaining,
		Limit:      s.config.Capacity,
	}, nil
}
