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

// TestDiskSessionStoreSharesAcrossInstances: a second store rooted at the same
// dir resolves a session the first one wrote, sweeps an aged one, and reports
// Delete presence exactly once — the cross-replica session seam ITEM-02 needs.
func TestDiskSessionStoreSharesAcrossInstances(t *testing.T) {
	root := t.TempDir()
	a := newDiskSessionStore(root)
	b := newDiskSessionStore(root)

	a.Put(&session{id: "s1", principal: "alice", initialized: true, protocol: "2025-06-18"})
	got, ok := b.Get("s1")
	if !ok || got.principal != "alice" || !got.initialized || got.protocol != "2025-06-18" || got.id != "s1" {
		t.Fatalf("b.Get(s1) = %+v ok=%v", got, ok)
	}
	if _, ok := b.Get("nope"); ok {
		t.Error("Get of an unknown id should miss")
	}
	if b.Len() != 1 {
		t.Errorf("Len = %d, want 1", b.Len())
	}

	// Age it past the window and sweep from the other instance.
	sf := filepath.Join(root, runtime.StateDirName, "sessions", "s1.json")
	raw, err := os.ReadFile(sf)
	if err != nil {
		t.Fatalf("session file not written where expected: %v", err)
	}
	var rec map[string]any
	if err := json.Unmarshal(raw, &rec); err != nil {
		t.Fatal(err)
	}
	rec["last_used"] = time.Now().Add(-2 * time.Hour)
	nb, _ := json.Marshal(rec)
	_ = os.WriteFile(sf, nb, 0o600)
	b.SweepIdle(time.Hour)
	if _, ok := a.Get("s1"); ok {
		t.Error("aged session survived SweepIdle")
	}

	a.Put(&session{id: "s2"})
	if !b.Delete("s2") || b.Delete("s2") {
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

// TestDiskCheckpointStoreLiveStatusAndCancel: the status record and cancel flag
// round-trip, do not trip Exists/Load, and Shared tracks who owns the dir.
func TestDiskCheckpointStoreLiveStatusAndCancel(t *testing.T) {
	d, err := newDiskCheckpointStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if !d.Shared() {
		t.Error("a supplied --state-dir must report Shared() == true")
	}
	owned, _ := newDiskCheckpointStore("")
	t.Cleanup(func() { _ = owned.Close() })
	if owned.Shared() {
		t.Error("an owned temp dir must report Shared() == false")
	}

	if _, ok := d.ReadStatus("r1"); ok {
		t.Error("ReadStatus on an unknown run should be ok=false")
	}
	if d.CancelRequested("r1") {
		t.Error("CancelRequested on an unknown run should be false")
	}

	rec := RunStatusRec{Tool: "Slow3", State: "working", Step: "B", StepIndex: 2, StepTotal: 3,
		Reached: []string{"A", "B"}, StartedAt: time.Now(), UpdatedAt: time.Now()}
	if err := d.WriteStatus("r1", rec); err != nil {
		t.Fatal(err)
	}
	got, ok := d.ReadStatus("r1")
	if !ok || got.State != "working" || got.Step != "B" || len(got.Reached) != 2 {
		t.Fatalf("ReadStatus = %+v ok=%v", got, ok)
	}
	// The status file must not look like a resumable checkpoint.
	if d.Exists("r1") {
		t.Error("a run-status file must not make Exists() true")
	}
	if _, ok := d.Load("r1"); ok {
		t.Error("a run-status file must not be Load()-able as a checkpoint")
	}

	if err := d.RequestCancel("r1"); err != nil {
		t.Fatal(err)
	}
	if !d.CancelRequested("r1") {
		t.Fatal("CancelRequested should be true after RequestCancel")
	}
	if d.Exists("r1") {
		t.Error("a cancel flag must not make Exists() true")
	}
	if err := d.Remove("r1"); err != nil {
		t.Fatal(err)
	}
	if _, ok := d.ReadStatus("r1"); ok {
		t.Error("Remove should drop the status record too")
	}
	if d.CancelRequested("r1") {
		t.Error("Remove should drop the cancel flag too")
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

	sd := d.stateDir("run1")
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
