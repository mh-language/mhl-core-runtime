package main

import (
	"bufio"
	"context"
	"os"
	"reflect"
	"strings"
	"testing"
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

func TestGlobEscape(t *testing.T) {
	if got := globEscape(`run/a*b?[x]\`); got != `run/a\*b\?\[x\]\\` {
		t.Fatalf("globEscape = %q", got)
	}
	if globEscape("run/") != "run/" {
		t.Fatal("globEscape mangled a plain prefix")
	}
}

// TestLiveRedis runs only when MHL_REDIS_TEST_ADDR is set (host:port). It
// exercises the store surface end to end, including the cas capability.
func TestLiveRedis(t *testing.T) {
	addr := os.Getenv("MHL_REDIS_TEST_ADDR")
	if addr == "" {
		t.Skip("set MHL_REDIS_TEST_ADDR (host:port) to run the live Redis test")
	}
	ctx := context.Background()
	s, err := newRedisStore(ctx, storeConfig{clientConfig: clientConfig{Addr: addr}, KeyPrefix: "t/"})
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()
	for _, k := range []string{"run/1", "run/2", "run/lk/lock"} {
		_ = s.del(ctx, k)
	}

	if _, ok, err := s.get(ctx, "run/1"); err != nil || ok {
		t.Fatalf("get miss: ok=%v err=%v", ok, err)
	}
	if err := s.put(ctx, "run/1", []byte(`{"step":"gate"}`)); err != nil {
		t.Fatal(err)
	}
	if err := s.put(ctx, "run/1", []byte(`{"step":"review"}`)); err != nil { // overwrite
		t.Fatal(err)
	}
	raw, ok, err := s.get(ctx, "run/1")
	if err != nil || !ok || string(raw) != `{"step":"review"}` {
		t.Fatalf("get hit: %q ok=%v err=%v", raw, ok, err)
	}
	_ = s.put(ctx, "run/2", []byte(`1`))
	keys, err := s.list(ctx, "run/")
	if err != nil || !reflect.DeepEqual(keys, []string{"run/1", "run/2"}) {
		t.Fatalf("list(run/) = %v err=%v", keys, err)
	}

	// cas
	a := []byte(`{"holder":"A"}`)
	b := []byte(`{"holder":"B"}`)
	if acq, err := s.putIfAbsent(ctx, "run/lk/lock", a); err != nil || !acq {
		t.Fatalf("put_if_absent free: acq=%v err=%v", acq, err)
	}
	if acq, err := s.putIfAbsent(ctx, "run/lk/lock", b); err != nil || acq {
		t.Fatalf("put_if_absent held must not acquire: acq=%v err=%v", acq, err)
	}
	cur, _, _ := s.get(ctx, "run/lk/lock")
	if sw, err := s.compareAndSwap(ctx, "run/lk/lock", cur, b); err != nil || !sw {
		t.Fatalf("cas on current: sw=%v err=%v", sw, err)
	}
	if sw, err := s.compareAndSwap(ctx, "run/lk/lock", a, []byte(`{"holder":"C"}`)); err != nil || sw {
		t.Fatalf("cas on stale must not swap: sw=%v err=%v", sw, err)
	}
	if sw, err := s.compareAndSwap(ctx, "run/nope/lock", a, b); err != nil || sw {
		t.Fatalf("cas on missing key: sw=%v err=%v", sw, err)
	}

	for _, k := range []string{"run/1", "run/2", "run/lk/lock"} {
		_ = s.del(ctx, k)
	}
	if err := s.del(ctx, "run/1"); err != nil {
		t.Fatalf("delete not idempotent: %v", err)
	}
}
