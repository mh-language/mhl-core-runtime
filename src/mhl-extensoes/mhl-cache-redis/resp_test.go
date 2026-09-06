package main

import (
	"bufio"
	"context"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestEncodeCommand(t *testing.T) {
	got := string(encodeCommand([]string{"SET", "k", "hello world"}))
	want := "*3\r\n$3\r\nSET\r\n$1\r\nk\r\n$11\r\nhello world\r\n"
	if got != want {
		t.Fatalf("encodeCommand =\n%q\nwant\n%q", got, want)
	}
}

func TestReadReply(t *testing.T) {
	cases := []struct {
		wire string
		want any
	}{
		{"+OK\r\n", "OK"},
		{":42\r\n", int64(42)},
		{"$5\r\nhello\r\n", []byte("hello")},
		{"$-1\r\n", nil},
		{"*-1\r\n", nil},
		{"*2\r\n$1\r\na\r\n:7\r\n", []any{[]byte("a"), int64(7)}},
	}
	for _, c := range cases {
		got, err := readReply(bufio.NewReader(strings.NewReader(c.wire)))
		if err != nil {
			t.Errorf("%q: %v", c.wire, err)
			continue
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%q: got %#v, want %#v", c.wire, got, c.want)
		}
	}

	if _, err := readReply(bufio.NewReader(strings.NewReader("-WRONGTYPE nope\r\n"))); err == nil {
		t.Fatal("error reply did not surface as an error")
	}
}

func TestParseTTL(t *testing.T) {
	cases := []struct {
		in   any
		want time.Duration
	}{
		{nil, 0},
		{"", 0},
		{"30s", 30 * time.Second},
		{"5m", 5 * time.Minute},
		{float64(90), 90 * time.Second},
		{2, 2 * time.Second},
		{"120", 120 * time.Second},
	}
	for _, c := range cases {
		got, err := parseTTL(c.in)
		if err != nil || got != c.want {
			t.Errorf("parseTTL(%v) = %v, %v; want %v", c.in, got, err, c.want)
		}
	}
	if _, err := parseTTL("nonsense"); err == nil {
		t.Fatal("parseTTL accepted garbage")
	}
}

func TestApplyURL(t *testing.T) {
	var cc clientConfig
	if err := applyURL(&cc, "rediss://alice:s3cr3t@cache.example:6380/2"); err != nil {
		t.Fatal(err)
	}
	if cc.Addr != "cache.example:6380" || cc.Username != "alice" || cc.Password != "s3cr3t" || cc.DB != 2 || !cc.TLS {
		t.Fatalf("applyURL: %+v", cc)
	}
	if err := applyURL(&clientConfig{}, "http://x"); err == nil {
		t.Fatal("applyURL accepted a non-redis scheme")
	}
}

func TestRedacted(t *testing.T) {
	if redacted("user:pass@h:6379") != "h:6379" || redacted("h:6379") != "h:6379" {
		t.Fatal("redacted mishandled an addr")
	}
}

// TestLiveRedis runs only when MHL_REDIS_TEST_ADDR is set (the CENARIO suite
// and `make smoke` cover this without CI).
func TestLiveRedis(t *testing.T) {
	addr := os.Getenv("MHL_REDIS_TEST_ADDR")
	if addr == "" {
		t.Skip("set MHL_REDIS_TEST_ADDR (host:port) to run the live Redis test")
	}
	ctx := context.Background()
	c, err := newRedisCache(ctx, cacheConfig{clientConfig: clientConfig{Addr: addr}, KeyPrefix: "t:"})
	if err != nil {
		t.Fatal(err)
	}
	defer c.close()
	_ = c.del(ctx, "k")
	_ = c.del(ctx, "n")

	if v, err := c.get(ctx, "k"); err != nil || v != nil {
		t.Fatalf("miss: %v %v", v, err)
	}
	if err := c.set(ctx, "k", map[string]any{"a": 1}, nil, false); err != nil {
		t.Fatal(err)
	}
	v, err := c.get(ctx, "k")
	if err != nil || v.(map[string]any)["a"].(float64) != 1 {
		t.Fatalf("hit: %v %v", v, err)
	}
	if ok, _ := c.has(ctx, "k"); !ok {
		t.Fatal("has should be true")
	}
	if n, err := c.incr(ctx, "n"); err != nil || n != 1 {
		t.Fatalf("incr: %d %v", n, err)
	}
	if n, err := c.incrBy(ctx, "n", 9); err != nil || n != 10 {
		t.Fatalf("incrBy: %d %v", n, err)
	}
	if err := c.set(ctx, "k", "x", "2s", true); err != nil {
		t.Fatal(err)
	}
	if secs, err := c.ttl(ctx, "k"); err != nil || secs < 1 || secs > 2 {
		t.Fatalf("ttl: %d %v", secs, err)
	}
	_ = c.del(ctx, "k")
	if ok, _ := c.has(ctx, "k"); ok {
		t.Fatal("has should be false after delete")
	}
	_ = c.del(ctx, "n")
}
