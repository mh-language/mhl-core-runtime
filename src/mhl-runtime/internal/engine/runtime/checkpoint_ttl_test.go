package runtime

import (
	"testing"
	"time"
)

// Every checkpoint expires after CheckpointTTL; one saved with no TTL (before
// the TTL was fixed, or by a zero-config Pipeline) expires after it too, and
// one with no save time is never treated as expired.
func TestCheckpointExpired(t *testing.T) {
	saved := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	day := 24 * time.Hour
	cases := []struct {
		name string
		cp   Checkpoint
		now  time.Time
		want bool
	}{
		{"fixed ttl, inside", Checkpoint{SavedAt: saved, TTLSeconds: int64(CheckpointTTL.Seconds())}, saved.Add(29 * day), false},
		{"fixed ttl, past", Checkpoint{SavedAt: saved, TTLSeconds: int64(CheckpointTTL.Seconds())}, saved.Add(31 * day), true},
		{"no ttl means the fixed one, inside", Checkpoint{SavedAt: saved}, saved.Add(29 * day), false},
		{"no ttl means the fixed one, past", Checkpoint{SavedAt: saved}, saved.Add(31 * day), true},
		{"explicit ttl still honoured", Checkpoint{SavedAt: saved, TTLSeconds: int64(day.Seconds())}, saved.Add(2 * day), true},
		{"no save time", Checkpoint{}, saved.Add(365 * day), false},
	}
	for _, tc := range cases {
		if got := tc.cp.Expired(tc.now); got != tc.want {
			t.Errorf("%s: Expired = %v, want %v", tc.name, got, tc.want)
		}
	}
}
