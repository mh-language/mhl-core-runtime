package main

import (
	"context"
	"sort"
	"strconv"
	"strings"
)

// redisStore is the `store`-kind KV surface on top of the tiny RESP2 client:
// opaque string keys, JSON values, an optional key prefix. Durable state for
// `mhl serve mcp --http` (sessions, run/* checkpoints, run/* locks) — no TTL.
// It also implements the "cas" capability: put_if_absent (SET NX) and
// compare_and_swap (a Lua EVAL), the atomic primitives `mhl serve`'s
// cross-replica run lock is built on.

type storeConfig struct {
	clientConfig
	KeyPrefix string
}

type redisStore struct {
	cli    *client
	prefix string
}

func newRedisStore(ctx context.Context, cfg storeConfig) (*redisStore, error) {
	cli, err := newClient(ctx, cfg.clientConfig)
	if err != nil {
		return nil, err
	}
	return &redisStore{cli: cli, prefix: cfg.KeyPrefix}, nil
}

func (s *redisStore) close() {
	if s.cli != nil {
		s.cli.close()
	}
}

func (s *redisStore) k(key string) string { return s.prefix + key }

// get returns the raw stored bytes at key (the caller decodes), or nil when
// the key is absent.
func (s *redisStore) get(ctx context.Context, key string) ([]byte, bool, error) {
	reply, err := s.cli.do(ctx, "GET", s.k(key))
	if err != nil {
		return nil, false, err
	}
	b, ok := reply.([]byte)
	if !ok || b == nil {
		return nil, false, nil
	}
	return b, true, nil
}

// put stores value at key, overwriting.
func (s *redisStore) put(ctx context.Context, key string, value []byte) error {
	_, err := s.cli.do(ctx, "SET", s.k(key), string(value))
	return err
}

func (s *redisStore) del(ctx context.Context, key string) error {
	_, err := s.cli.do(ctx, "DEL", s.k(key))
	return err
}

// list returns every key with the given logical prefix, sorted (SCAN MATCH in
// a cursor loop; the prefix is glob-escaped so run/<id>/… keys match
// literally).
func (s *redisStore) list(ctx context.Context, logicalPrefix string) ([]string, error) {
	pattern := globEscape(s.prefix+logicalPrefix) + "*"
	cursor := "0"
	out := []string{}
	for {
		reply, err := s.cli.do(ctx, "SCAN", cursor, "MATCH", pattern, "COUNT", "250")
		if err != nil {
			return nil, err
		}
		page, ok := reply.([]any)
		if !ok || len(page) != 2 {
			return nil, redisError("SCAN: unexpected reply shape")
		}
		cursor = asString(page[0])
		keys, _ := page[1].([]any)
		for _, k := range keys {
			out = append(out, strings.TrimPrefix(asString(k), s.prefix))
		}
		if cursor == "0" {
			break
		}
	}
	sort.Strings(out)
	return out, nil
}

// putIfAbsent stores value at key only when no key exists (SET ... NX).
// acquired is false (nil error) when the key is already present.
func (s *redisStore) putIfAbsent(ctx context.Context, key string, value []byte) (bool, error) {
	reply, err := s.cli.do(ctx, "SET", s.k(key), string(value), "NX")
	if err != nil {
		return false, err
	}
	// +OK on success; nil ($-1) when NX prevented the write.
	if str, ok := reply.(string); ok && str == "OK" {
		return true, nil
	}
	return false, nil
}

// casScript: swap only when the current value is byte-equal to ARGV[1].
const casScript = `if redis.call('GET', KEYS[1]) == ARGV[1] then redis.call('SET', KEYS[1], ARGV[2]); return 1 else return 0 end`

// compareAndSwap replaces value at key with newValue only when the current
// value equals expected (a single atomic Lua EVAL). swapped is false (nil
// error) on a mismatch or a missing key.
func (s *redisStore) compareAndSwap(ctx context.Context, key string, expected, newValue []byte) (bool, error) {
	reply, err := s.cli.do(ctx, "EVAL", casScript, "1", s.k(key), string(expected), string(newValue))
	if err != nil {
		return false, err
	}
	return toInt(reply) == 1, nil
}

// globEscape neutralises Redis glob metacharacters in a literal prefix so
// SCAN MATCH treats it verbatim.
func globEscape(s string) string {
	return strings.NewReplacer(
		`\`, `\\`, `*`, `\*`, `?`, `\?`, `[`, `\[`, `]`, `\]`,
	).Replace(s)
}

func asString(v any) string {
	switch t := v.(type) {
	case []byte:
		return string(t)
	case string:
		return t
	}
	return ""
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
