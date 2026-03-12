package capacitor

import (
	"context"
	"fmt"
	"hash/fnv"
	"math"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresConfig holds configuration for PostgresStore.
type PostgresConfig struct {
	// Pool is the connection pool to use. If nil, a new pool will be created from ConnConfig.
	Pool *pgxpool.Pool

	// ConnConfig is used to create a new pool if Pool is nil.
	ConnConfig pgxpool.Config

	// TableName is the name of the table to store buckets. Defaults to "capacitor_buckets".
	TableName string

	// Capacity is the maximum number of tokens in the bucket.
	Capacity int64

	// LeakRate is the number of tokens that leak per second.
	LeakRate float64

	// KeyPrefix is the prefix for all keys. Defaults to "capacitor".
	KeyPrefix string

	// Timeout is the timeout for operations. Defaults to 50ms.
	Timeout time.Duration
}

// DefaultPostgresConfig returns sensible defaults for PostgresConfig.
func DefaultPostgresConfig() PostgresConfig {
	return PostgresConfig{
		TableName:  "capacitor_buckets",
		Capacity:   20,
		LeakRate:   5,
		KeyPrefix:  "capacitor",
		Timeout:    50 * time.Millisecond,
	}
}

// PostgresStore implements the Store interface using PostgreSQL.
type PostgresStore struct {
	pool    *pgxpool.Pool
	config  PostgresConfig
}

var _ Store = (*PostgresStore)(nil)

// NewPostgresStore creates a new PostgresStore.
func NewPostgresStore(ctx context.Context, cfg PostgresConfig) (*PostgresStore, error) {
	// Create pool if not provided
	if cfg.Pool == nil {
		pool, err := pgxpool.NewWithConfig(ctx, &cfg.ConnConfig)
		if err != nil {
			return nil, fmt.Errorf("capacitor: failed to create pool: %w", err)
		}
		cfg.Pool = pool
	}

	s := &PostgresStore{
		pool:   cfg.Pool,
		config: cfg,
	}

	// Ensure table exists
	if err := s.ensureTable(ctx); err != nil {
		return nil, fmt.Errorf("capacitor: failed to ensure table: %w", err)
	}

	return s, nil
}

// ensureTable creates the buckets table if it doesn't exist.
func (s *PostgresStore) ensureTable(ctx context.Context) error {
	query := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			key TEXT PRIMARY KEY,
			level DOUBLE PRECISION NOT NULL DEFAULT 0,
			last_leak DOUBLE PRECISION NOT NULL,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);
		CREATE INDEX IF NOT EXISTS %s_updated_at ON %s(updated_at);
	`, s.config.TableName, s.config.TableName, s.config.TableName)

	_, err := s.pool.Exec(ctx, query)
	return err
}

// HealthCheck verifies connectivity to PostgreSQL.
func (s *PostgresStore) HealthCheck(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return s.pool.Ping(ctx)
}

// Close gracefully shuts down the connection pool.
func (s *PostgresStore) Close() error {
	s.pool.Close()
	return nil
}

// Attempt checks whether the request identified by uid is allowed.
func (s *PostgresStore) Attempt(ctx context.Context, uid string) (Result, error) {
	if uid == "" {
		return Result{}, ErrEmptyUID
	}

	key := s.config.KeyPrefix + ":uid:" + uid
	lockID := hashStringToInt64(key)

	ctx, cancel := context.WithTimeout(ctx, s.config.Timeout)
	defer cancel()

	// Use a transaction for atomicity
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("capacitor: begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// Acquire advisory lock (transaction-scoped, auto-releases on commit)
	_, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", lockID)
	if err != nil {
		return Result{}, fmt.Errorf("capacitor: advisory lock: %w", err)
	}

	// Execute the leaky bucket logic
	now := float64(time.Now().UnixMilli()) / 1000.0

	// Upsert and get updated state in one query
	result, err := tx.Exec(ctx, fmt.Sprintf(`
		INSERT INTO %s (key, level, last_leak, updated_at)
		VALUES ($1, 1, $2, NOW())
		ON CONFLICT (key) DO UPDATE SET
			level = GREATEST(0, level - ($2 - last_leak) * $3) + 1,
			last_leak = $2,
			updated_at = NOW()
		WHERE level - ($2 - last_leak) * $3 + 1 <= $4
	`, s.config.TableName),
		key,
		now,
		s.config.LeakRate,
		s.config.Capacity,
	)
	if err != nil {
		return Result{}, fmt.Errorf("capacitor: execute bucket: %w", err)
	}

	allowed := result.RowsAffected() > 0

	// Get remaining count
	var level float64
	err = tx.QueryRow(ctx, fmt.Sprintf("SELECT level FROM %s WHERE key = $1", s.config.TableName), key).Scan(&level)
	if err != nil {
		return Result{}, fmt.Errorf("capacitor: get level: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return Result{}, fmt.Errorf("capacitor: commit: %w", err)
	}

	remaining := int64(math.Max(0, math.Floor(float64(s.config.Capacity)-level)))

	return Result{
		Allowed:    allowed,
		Remaining: remaining,
		Limit:      s.config.Capacity,
	}, nil
}

// hashStringToInt64 converts a string to a 64-bit integer for use as an advisory lock key.
// This mimics PostgreSQL's hashtext() function.
func hashStringToInt64(s string) int64 {
	h := fnv.New64a()
	h.Write([]byte(s))
	// Take modulo to fit in PostgreSQL's advisory lock range (int32)
	return int64(h.Sum64() % (1 << 31))
}
