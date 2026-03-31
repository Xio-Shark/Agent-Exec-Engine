package store

import "context"

// Store abstracts the persistence layer.
// All implementations must be safe for concurrent use.
type Store interface {
	// Set stores a key-value pair with optional TTL (0 = no expiry).
	Set(ctx context.Context, key string, value []byte, ttlSeconds int) error

	// Get retrieves a value by key. Returns ErrNotFound if key doesn't exist.
	Get(ctx context.Context, key string) ([]byte, error)

	// Delete removes a key. No error if key doesn't exist.
	Delete(ctx context.Context, key string) error

	// Ping checks if the store is reachable.
	Ping(ctx context.Context) error

	// Close releases resources.
	Close() error
}

// ErrNotFound is returned when a key is not found in the store.
type ErrNotFound struct {
	Key string
}

func (e *ErrNotFound) Error() string {
	return "key not found: " + e.Key
}
