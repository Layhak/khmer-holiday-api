// Package rediscache provides the production Redis implementation of the API's
// small response-cache interface.
package rediscache

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// Cache wraps go-redis's concurrency-safe connection pool.
type Cache struct {
	client *redis.Client
}

// Open parses a redis:// or rediss:// URL and confirms the server is reachable.
func Open(ctx context.Context, rawURL string) (*Cache, error) {
	options, err := optionsFromURL(rawURL)
	if err != nil {
		return nil, err
	}
	client := redis.NewClient(options)

	pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := client.Ping(pingCtx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("ping Redis: %w", err)
	}
	return &Cache{client: client}, nil
}

func optionsFromURL(rawURL string) (*redis.Options, error) {
	options, err := redis.ParseURL(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid Redis URL")
	}
	options.DialTimeout = 500 * time.Millisecond
	options.ReadTimeout = 250 * time.Millisecond
	options.WriteTimeout = 250 * time.Millisecond
	options.PoolTimeout = 500 * time.Millisecond
	options.MaxRetries = 1
	options.MinRetryBackoff = 10 * time.Millisecond
	options.MaxRetryBackoff = 50 * time.Millisecond
	return options, nil
}

// Get returns a defensive copy of a cached response.
func (c *Cache) Get(ctx context.Context, key string) ([]byte, bool, error) {
	value, err := c.client.Get(ctx, key).Bytes()
	switch {
	case errors.Is(err, redis.Nil):
		return nil, false, nil
	case err != nil:
		return nil, false, err
	default:
		return value, true, nil
	}
}

// Set stores a response with a mandatory bounded TTL supplied by the API.
func (c *Cache) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if ttl <= 0 {
		return nil
	}
	return c.client.Set(ctx, key, value, ttl).Err()
}

// Close releases the Redis connection pool.
func (c *Cache) Close() error { return c.client.Close() }
