package nativeops_test

import (
	"regexp"
	"testing"
	"time"

	"github.com/mh-language/mhl-core-runtime/internal/features/nativeops"
)

var canonicalUUID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func TestUUIDv4Shape(t *testing.T) {
	got, err := nativeops.UUIDv4()
	if err != nil {
		t.Fatalf("UUIDv4: %v", err)
	}
	if !canonicalUUID.MatchString(got) {
		t.Fatalf("UUIDv4() = %q, not canonical form", got)
	}
	if got[14] != '4' {
		t.Errorf("UUIDv4() version nibble = %c, want 4 (%q)", got[14], got)
	}
	if v := got[19]; v != '8' && v != '9' && v != 'a' && v != 'b' {
		t.Errorf("UUIDv4() variant nibble = %c, want 8/9/a/b (%q)", v, got)
	}
}

func TestUUIDv7Shape(t *testing.T) {
	got, err := nativeops.UUIDv7()
	if err != nil {
		t.Fatalf("UUIDv7: %v", err)
	}
	if !canonicalUUID.MatchString(got) {
		t.Fatalf("UUIDv7() = %q, not canonical form", got)
	}
	if got[14] != '7' {
		t.Errorf("UUIDv7() version nibble = %c, want 7 (%q)", got[14], got)
	}
	if v := got[19]; v != '8' && v != '9' && v != 'a' && v != 'b' {
		t.Errorf("UUIDv7() variant nibble = %c, want 8/9/a/b (%q)", v, got)
	}
}

func TestUUIDsAreUnique(t *testing.T) {
	seen := make(map[string]bool, 2000)
	for i := 0; i < 1000; i++ {
		for _, gen := range []func() (string, error){nativeops.UUIDv4, nativeops.UUIDv7} {
			id, err := gen()
			if err != nil {
				t.Fatalf("generate: %v", err)
			}
			if seen[id] {
				t.Fatalf("duplicate UUID %q after %d iterations", id, i)
			}
			seen[id] = true
		}
	}
}

// TestUUIDv7IsTimeOrdered checks that v7 values minted across a millisecond
// boundary sort lexicographically in creation order — the property that
// motivates v7 over v4 for database keys.
func TestUUIDv7IsTimeOrdered(t *testing.T) {
	first, err := nativeops.UUIDv7()
	if err != nil {
		t.Fatalf("UUIDv7: %v", err)
	}
	time.Sleep(2 * time.Millisecond)
	second, err := nativeops.UUIDv7()
	if err != nil {
		t.Fatalf("UUIDv7: %v", err)
	}
	if first >= second {
		t.Errorf("expected %q < %q (time-ordered)", first, second)
	}
}
