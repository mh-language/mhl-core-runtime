package memory_test

import (
	"testing"

	"github.com/yanjustino/mhl-runtime/internal/features/memory"
)

func TestKVStoreGetPathNavigatesIntoObjectField(t *testing.T) {
	s := memory.NewKVStore()
	s.Set("session_mem", "cfg", map[string]any{"retries": 3.0, "enabled": true})

	got := s.Get("session_mem", "cfg::retries", nil)
	if got != 3.0 {
		t.Errorf("Get(cfg::retries) = %v, want 3.0", got)
	}
}

func TestKVStoreGetPathNavigatesIntoArrayIndex(t *testing.T) {
	s := memory.NewKVStore()
	s.Set("session_mem", "tags", []any{"a", "b", "c"})

	got := s.Get("session_mem", "tags::1", nil)
	if got != "b" {
		t.Errorf("Get(tags::1) = %v, want %q", got, "b")
	}
}

func TestKVStoreGetPathNestedObjectThenArray(t *testing.T) {
	s := memory.NewKVStore()
	s.Set("session_mem", "cfg", map[string]any{
		"hosts": []any{"a.example.com", "b.example.com"},
	})

	got := s.Get("session_mem", "cfg::hosts::0", nil)
	if got != "a.example.com" {
		t.Errorf("Get(cfg::hosts::0) = %v, want %q", got, "a.example.com")
	}
}

func TestKVStoreGetPathMissingFieldReturnsDefault(t *testing.T) {
	s := memory.NewKVStore()
	s.Set("session_mem", "cfg", map[string]any{"retries": 3.0})

	got := s.Get("session_mem", "cfg::timeout", "unset")
	if got != "unset" {
		t.Errorf("Get(cfg::timeout) = %v, want default %q", got, "unset")
	}
}

func TestKVStoreGetPathIndexOutOfRangeReturnsDefault(t *testing.T) {
	s := memory.NewKVStore()
	s.Set("session_mem", "tags", []any{"a", "b"})

	got := s.Get("session_mem", "tags::5", "unset")
	if got != "unset" {
		t.Errorf("Get(tags::5) = %v, want default %q", got, "unset")
	}
}

func TestKVStoreGetPathIntoScalarReturnsDefault(t *testing.T) {
	s := memory.NewKVStore()
	s.Set("session_mem", "attempt", "1")

	got := s.Get("session_mem", "attempt::anything", "unset")
	if got != "unset" {
		t.Errorf("Get(attempt::anything) = %v, want default %q", got, "unset")
	}
}

func TestKVStoreGetWithoutDelimiterIsUnaffected(t *testing.T) {
	s := memory.NewKVStore()
	s.Set("session_mem", "attempt", "1")

	if got := s.Get("session_mem", "attempt", "0"); got != "1" {
		t.Errorf("Get(attempt) = %v, want %q", got, "1")
	}
}
