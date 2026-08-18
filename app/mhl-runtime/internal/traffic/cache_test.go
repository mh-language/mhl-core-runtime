package traffic_test

import (
	"testing"
	"time"

	"github.com/yanjustino/mhl-runtime/internal/traffic"
)

func TestRequestKeyIsDeterministic(t *testing.T) {
	a := traffic.RequestKey("claude", "hello", map[string]any{"temperature": 0.2, "top_p": 1})
	b := traffic.RequestKey("claude", "hello", map[string]any{"top_p": 1, "temperature": 0.2})
	if a != b || len(a) != 64 {
		t.Fatalf("keys differ: %q %q", a, b)
	}
}

func TestCacheHitsAndExpiresMCPTTL(t *testing.T) {
	c := traffic.NewCache("")
	key := traffic.RequestKey("tool", "call", nil)
	if err := c.SetMCP(key, "fresh", time.Hour, 10); err != nil {
		t.Fatal(err)
	}
	if value, ok := c.Get(key); !ok || value != "fresh" {
		t.Fatalf("initial cache = %v, %v", value, ok)
	}
	time.Sleep(15 * time.Millisecond)
	if _, ok := c.Get(key); ok {
		t.Fatal("expired MCP result was served")
	}
}

func TestDiskCacheRoundTrip(t *testing.T) {
	key := traffic.RequestKey("agent", "prompt", map[string]any{"x": 1})
	c := traffic.NewCache(t.TempDir())
	if err := c.Set(key, map[string]any{"answer": "ok"}, time.Minute); err != nil {
		t.Fatal(err)
	}
	c2 := traffic.NewCache(c.Dir)
	if value, ok := c2.Get(key); !ok || value.(map[string]any)["answer"] != "ok" {
		t.Fatalf("disk cache = %#v, %v", value, ok)
	}
}
