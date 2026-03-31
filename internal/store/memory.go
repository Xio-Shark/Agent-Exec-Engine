package store

import (
	"context"
	"sync"
	"time"
)

// MemoryStore implements Store for tests.
type MemoryStore struct {
	mu          sync.RWMutex
	data        map[string][]byte
	ttlByKey    map[string]int
	expiryByKey map[string]time.Time
}

// NewMemoryStore creates an in-memory store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		data:        make(map[string][]byte),
		ttlByKey:    make(map[string]int),
		expiryByKey: make(map[string]time.Time),
	}
}

func (m *MemoryStore) Set(ctx context.Context, key string, value []byte, ttlSeconds int) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	copied := append([]byte(nil), value...)
	m.data[key] = copied
	if ttlSeconds > 0 {
		m.ttlByKey[key] = ttlSeconds
		m.expiryByKey[key] = time.Now().Add(time.Duration(ttlSeconds) * time.Second)
	} else {
		delete(m.ttlByKey, key)
		delete(m.expiryByKey, key)
	}
	return nil
}

func (m *MemoryStore) Get(ctx context.Context, key string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if expiry, ok := m.expiryByKey[key]; ok && time.Now().After(expiry) {
		delete(m.data, key)
		delete(m.ttlByKey, key)
		delete(m.expiryByKey, key)
		return nil, &ErrNotFound{Key: key}
	}

	value, ok := m.data[key]
	if !ok {
		return nil, &ErrNotFound{Key: key}
	}
	return append([]byte(nil), value...), nil
}

func (m *MemoryStore) Delete(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, key)
	delete(m.ttlByKey, key)
	delete(m.expiryByKey, key)
	return nil
}

func (m *MemoryStore) Ping(ctx context.Context) error {
	return ctx.Err()
}

func (m *MemoryStore) Close() error {
	return nil
}

// TTL returns the last TTL recorded for a key.
func (m *MemoryStore) TTL(key string) (int, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	ttl, ok := m.ttlByKey[key]
	return ttl, ok
}
