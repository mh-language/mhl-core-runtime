package mcpserver

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mh-language/mhl-core-runtime/internal/engine/runtime"
)

func TestOwnerFromSession(t *testing.T) {
	if got := ownerFromSession(""); got != "" {
		t.Errorf("empty session id must yield the anonymous owner, got %q", got)
	}
	a, b := ownerFromSession("sess-abc"), ownerFromSession("sess-abc")
	if a != b {
		t.Errorf("not deterministic: %q vs %q", a, b)
	}
	if a == ownerFromSession("sess-xyz") {
		t.Error("different session ids collided to the same owner")
	}
	if string(a) == "sess-abc" {
		t.Error("owner must be a hash, not the raw session id")
	}
}

func TestMemSessionStoreSweep(t *testing.T) {
	s := newMemSessionStore()
	s.Put(&session{id: "keep"})
	s.Put(&session{id: "drop"})

	// Age "drop" past the window; leave "keep" fresh.
	s.m["drop"].lastUsed = time.Now().Add(-2 * time.Hour)
	s.SweepIdle(time.Hour)

	if _, ok := s.Get("keep"); !ok {
		t.Error("fresh session was swept")
	}
	if _, ok := s.Get("drop"); ok {
		t.Error("idle session survived the sweep")
	}
	if !s.Delete("keep") || s.Delete("keep") {
		t.Error("Delete should report presence exactly once")
	}
}

func TestMemRunRegistryList(t *testing.T) {
	r := newMemRunRegistry()
	r.Put(&asyncRun{id: "a"})
	r.Put(&asyncRun{id: "b"})
	if got := r.List(); len(got) != 2 {
		t.Fatalf("List len = %d, want 2", len(got))
	}
	r.Delete("a")
	if _, ok := r.Get("a"); ok {
		t.Error("deleted run still resolvable")
	}
	if got := r.List(); len(got) != 1 || got[0].id != "b" {
		t.Errorf("List after delete = %+v", got)
	}
}

// TestDiskCheckpointStoreOwnership: a "" stateDir mints an owned temp dir that
// Close removes; a supplied dir is created and left in place.
func TestDiskCheckpointStoreOwnership(t *testing.T) {
	owned, err := newDiskCheckpointStore("")
	if err != nil {
		t.Fatal(err)
	}
	if !owned.owns {
		t.Fatal("empty stateDir should be owned")
	}
	dir := owned.BaseDir()
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("temp dir not created: %v", err)
	}
	if err := owned.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("owned temp dir survived Close: %v", err)
	}

	given := filepath.Join(t.TempDir(), "state")
	kept, err := newDiskCheckpointStore(given)
	if err != nil {
		t.Fatal(err)
	}
	if kept.owns {
		t.Error("supplied stateDir must not be owned")
	}
	if err := kept.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(given); err != nil {
		t.Errorf("supplied --state-dir removed on Close: %v", err)
	}
}

// TestDiskCheckpointStoreLoadExists: Load parses the run's checkpoint file and
// Exists reports on it; result.json and a missing dir do not count.
func TestDiskCheckpointStoreLoadExists(t *testing.T) {
	d, err := newDiskCheckpointStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if d.Exists("nope") || func() bool { _, ok := d.Load("nope"); return ok }() {
		t.Error("missing run reported as present")
	}

	sd := d.StateDir("run1")
	if err := os.MkdirAll(sd, 0o755); err != nil {
		t.Fatal(err)
	}
	cp := runtime.Checkpoint{Pipeline: "Approval", NextStep: "Gate", CompletedSteps: []string{"Prepare"}}
	b, _ := json.Marshal(cp)
	if err := os.WriteFile(filepath.Join(sd, "Approval.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sd, "result.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	if !d.Exists("run1") {
		t.Error("checkpoint file not detected by Exists")
	}
	got, ok := d.Load("run1")
	if !ok || got.Pipeline != "Approval" || got.NextStep != "Gate" {
		t.Errorf("Load = %+v ok=%v", got, ok)
	}

	if err := d.Remove("run1"); err != nil {
		t.Fatal(err)
	}
	if d.Exists("run1") {
		t.Error("Remove left the state dir behind")
	}
}
