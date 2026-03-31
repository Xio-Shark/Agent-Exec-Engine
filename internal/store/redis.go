package store

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisStore implements Store using Redis.
type RedisStore struct {
	client *redis.Client
}

// RedisOptions configures the Redis connection.
type RedisOptions struct {
	URL      string
	Password string
	DB       int
	PoolSize int
}

// NewRedisStore creates a Redis-backed store.
func NewRedisStore(opts RedisOptions) (*RedisStore, error) {
	// Parse URL to extract host:port
	addr := "localhost:6379"
	if opts.URL != "" {
		parsed, err := url.Parse(opts.URL)
		if err != nil {
			return nil, fmt.Errorf("parse redis URL: %w", err)
		}
		if parsed.Host != "" {
			addr = parsed.Host
		}
		// Extract password from URL if not provided separately
		if opts.Password == "" && parsed.User != nil {
			opts.Password, _ = parsed.User.Password()
		}
		// Extract DB from URL path if not provided
		if opts.DB == 0 && parsed.Path != "" && parsed.Path != "/" {
			if db, err := strconv.Atoi(parsed.Path[1:]); err == nil {
				opts.DB = db
			}
		}
	}

	poolSize := opts.PoolSize
	if poolSize <= 0 {
		poolSize = 10
	}

	client := redis.NewClient(&redis.Options{
		Addr:         addr,
		Password:     opts.Password,
		DB:           opts.DB,
		PoolSize:     poolSize,
		MinIdleConns: 2,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
	})

	// Verify connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		client.Close()
		return nil, fmt.Errorf("redis ping: %w", err)
	}

	return &RedisStore{client: client}, nil
}

func (r *RedisStore) Set(ctx context.Context, key string, value []byte, ttlSeconds int) error {
	var expiration time.Duration
	if ttlSeconds > 0 {
		expiration = time.Duration(ttlSeconds) * time.Second
	}
	return r.client.Set(ctx, key, value, expiration).Err()
}

func (r *RedisStore) Get(ctx context.Context, key string) ([]byte, error) {
	val, err := r.client.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return nil, &ErrNotFound{Key: key}
	}
	if err != nil {
		return nil, err
	}
	return val, nil
}

func (r *RedisStore) Delete(ctx context.Context, key string) error {
	return r.client.Del(ctx, key).Err()
}

func (r *RedisStore) Ping(ctx context.Context) error {
	return r.client.Ping(ctx).Err()
}

func (r *RedisStore) Close() error {
	return r.client.Close()
}

// Client returns the underlying redis.Client for advanced operations.
// Use sparingly — prefer the Store interface methods.
func (r *RedisStore) Client() *redis.Client {
	return r.client
}
