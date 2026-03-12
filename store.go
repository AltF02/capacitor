package capacitor

import (
	"context"
)

// Store defines the interface for rate limiter backends.
// Both Valkey and PostgreSQL implementations must satisfy this interface.
type Store interface {
	// Attempt checks whether the request identified by uid is allowed.
	Attempt(ctx context.Context, uid string) (Result, error)

	// HealthCheck verifies connectivity to the store.
	HealthCheck(ctx context.Context) error

	// Close gracefully shuts down the store connection.
	Close() error
}
