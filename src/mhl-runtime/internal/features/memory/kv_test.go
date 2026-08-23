package memory_test

import (
	"testing"

	"github.com/yanjustino/mhl-runtime/internal/features/memory"
)

func TestKVStoreSetThenGet(t *testing.T) {
	s := memory.NewKVStore()
	s.Set("session_mem", "current_diff", "diff content")
	got := s.Get("session_mem", "current_diff", "default")
	t.Logf("retrieved value: %v", got)
	if got != "diff content" {
		t.Errorf("Get = %q, want %q", got, "diff content")
	}
}

func TestKVStoreGetMissingReturnsDefault(t *testing.T) {
	s := memory.NewKVStore()
	if got := s.Get("session_mem", "attempt", "0"); got != "0" {
		t.Errorf("Get = %q, want default %q", got, "0")
	}
}

func TestKVStoreOverwrite(t *testing.T) {
	s := memory.NewKVStore()
	s.Set("session_mem", "attempt", "1")
	s.Set("session_mem", "attempt", "2")
	if got := s.Get("session_mem", "attempt", ""); got != "2" {
		t.Errorf("Get = %q, want %q", got, "2")
	}
}

func TestKVStoreNamespacedByMemoryName(t *testing.T) {
	s := memory.NewKVStore()
	s.Set("session_mem", "key", "from session_mem")
	s.Set("other_mem", "key", "from other_mem")
	if got := s.Get("session_mem", "key", ""); got != "from session_mem" {
		t.Errorf("session_mem: got %q", got)
	}
	if got := s.Get("other_mem", "key", ""); got != "from other_mem" {
		t.Errorf("other_mem: got %q", got)
	}
}
