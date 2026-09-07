package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

// redisCache is the `cache`-kind surface on top of the tiny Redis client:
// JSON-encoded values, an optional key prefix, and a default TTL applied to
// every set() that does not name its own.

type cacheConfig struct {
	clientConfig
	KeyPrefix  string
	DefaultTTL time.Duration
}

type redisCache struct {
	cli    *client
	prefix string
	defTTL time.Duration
}

func newRedisCache(ctx context.Context, cfg cacheConfig) (*redisCache, error) {
	cli, err := newClient(ctx, cfg.clientConfig)
	if err != nil {
		return nil, err
	}
	return &redisCache{cli: cli, prefix: cfg.KeyPrefix, defTTL: cfg.DefaultTTL}, nil
}

func (c *redisCache) close() {
	if c.cli != nil {
		c.cli.close()
	}
}

func (c *redisCache) k(key string) string { return c.prefix + key }

// get returns the JSON-decoded value at key, or nil when the key is absent.
// A value not written as JSON (e.g. a bare integer left by incr, or a string
// set by another client) is returned as-is.
func (c *redisCache) get(ctx context.Context, key string) (any, error) {
	reply, err := c.cli.do(ctx, "GET", c.k(key))
	if err != nil {
		return nil, err
	}
	b, ok := reply.([]byte)
	if !ok || b == nil {
		return nil, nil
	}
	var v any
	if json.Unmarshal(b, &v) == nil {
		return v, nil
	}
	return string(b), nil
}

// set stores value (JSON-encoded) at key. ttl overrides the declaration's
// default TTL for this call: a number is seconds, a string is a Go duration
// ("30s", "5m"); 0 or "" means "no expiry" only when there is no default.
func (c *redisCache) set(ctx context.Context, key string, value any, ttl any, ttlGiven bool) error {
	b, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("redis: encoding value for %q: %w", key, err)
	}
	args := []string{"SET", c.k(key), string(b)}

	d := c.defTTL
	if ttlGiven {
		d, err = parseTTL(ttl)
		if err != nil {
			return err
		}
	}
	if d > 0 {
		args = append(args, "PX", strconv.FormatInt(d.Milliseconds(), 10))
	}
	_, err = c.cli.do(ctx, args...)
	return err
}

func (c *redisCache) del(ctx context.Context, key string) error {
	_, err := c.cli.do(ctx, "DEL", c.k(key))
	return err
}

func (c *redisCache) has(ctx context.Context, key string) (bool, error) {
	reply, err := c.cli.do(ctx, "EXISTS", c.k(key))
	if err != nil {
		return false, err
	}
	return toInt(reply) > 0, nil
}

func (c *redisCache) incr(ctx context.Context, key string) (int64, error) {
	reply, err := c.cli.do(ctx, "INCR", c.k(key))
	if err != nil {
		return 0, err
	}
	return toInt(reply), nil
}

func (c *redisCache) incrBy(ctx context.Context, key string, n int64) (int64, error) {
	reply, err := c.cli.do(ctx, "INCRBY", c.k(key), strconv.FormatInt(n, 10))
	if err != nil {
		return 0, err
	}
	return toInt(reply), nil
}

func (c *redisCache) expire(ctx context.Context, key string, ttl any) (bool, error) {
	d, err := parseTTL(ttl)
	if err != nil {
		return false, err
	}
	secs := int64(d.Seconds())
	if secs < 1 {
		secs = 1
	}
	reply, err := c.cli.do(ctx, "EXPIRE", c.k(key), strconv.FormatInt(secs, 10))
	if err != nil {
		return false, err
	}
	return toInt(reply) == 1, nil
}

// ttl returns the remaining seconds: -1 = no expiry, -2 = missing key.
func (c *redisCache) ttl(ctx context.Context, key string) (int64, error) {
	reply, err := c.cli.do(ctx, "TTL", c.k(key))
	if err != nil {
		return 0, err
	}
	return toInt(reply), nil
}

func toInt(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int:
		return int64(n)
	case float64:
		return int64(n)
	case []byte:
		i, _ := strconv.ParseInt(string(n), 10, 64)
		return i
	case string:
		i, _ := strconv.ParseInt(n, 10, 64)
		return i
	}
	return 0
}

// parseTTL accepts a Go duration string ("30s", "5m", "1h"), or a number of
// seconds (float/int/JSON number, or a plain numeric string).
func parseTTL(v any) (time.Duration, error) {
	switch t := v.(type) {
	case nil:
		return 0, nil
	case float64:
		return time.Duration(t * float64(time.Second)), nil
	case int:
		return time.Duration(t) * time.Second, nil
	case int64:
		return time.Duration(t) * time.Second, nil
	case json.Number:
		f, _ := t.Float64()
		return time.Duration(f * float64(time.Second)), nil
	case string:
		if t == "" {
			return 0, nil
		}
		if d, err := time.ParseDuration(t); err == nil {
			return d, nil
		}
		if f, err := strconv.ParseFloat(t, 64); err == nil {
			return time.Duration(f * float64(time.Second)), nil
		}
		return 0, fmt.Errorf("redis: bad ttl %q (use \"30s\" / \"5m\" or a number of seconds)", t)
	}
	return 0, fmt.Errorf("redis: bad ttl %v", v)
}
