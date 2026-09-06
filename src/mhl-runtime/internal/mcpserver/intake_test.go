package mcpserver

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// With a cas-capable store, run/start persists a durable intake record — tool,
// inputs, owner — before the run launches, so an accepted run survives a crash
// before its first step. Nothing consumes the record yet (the claim loop is a
// later slice); this fixes that it is written.
func TestRunStartPersistsDurableIntakeRecord(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "w.mh"),
		[]byte("pipeline P {\n  input who: string\n  step S { log(who) }\n}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	kv := newFakeLockingKV()
	_, h, err := buildHTTP(context.Background(), HTTPConfig{Dir: dir, Store: kv}, io.Discard)
	if err != nil {
		t.Fatalf("buildHTTP: %v", err)
	}
	t.Cleanup(func() { h.runsCancel(); _ = h.cps.Close() })

	sess := &session{id: "s1"}
	res := decodeResult(t, h.runStart(sess, mkMsg("run/start", map[string]any{
		"name":      "P",
		"arguments": map[string]any{"who": "alice"},
	})))
	runID, _ := res["runId"].(string)
	if runID == "" {
		t.Fatalf("run/start gave no runId: %v", res)
	}

	// The intake record exists the moment run/start returns (written before launch).
	raw, found, err := kv.Get(context.Background(), intakeKey(runID))
	if err != nil || !found {
		t.Fatalf("no intake record at %s (found=%v err=%v)", intakeKey(runID), found, err)
	}
	var rec intakeRec
	if err := json.Unmarshal(raw, &rec); err != nil {
		t.Fatalf("intake record: %v", err)
	}
	if rec.Tool != "P" {
		t.Errorf("intake Tool = %q, want P", rec.Tool)
	}
	if rec.Args["who"] != "alice" {
		t.Errorf("intake Args = %v, want who=alice", rec.Args)
	}
	if rec.Owner != h.ownerOf(sess) {
		t.Errorf("intake Owner = %q, want %q", rec.Owner, h.ownerOf(sess))
	}
	if rec.CreatedAt.IsZero() {
		t.Error("intake CreatedAt is zero")
	}

	// readIntake round-trips it.
	got, ok := h.readIntake(context.Background(), runID)
	if !ok || got.Tool != "P" || got.Args["who"] != "alice" {
		t.Errorf("readIntake = %+v ok=%v", got, ok)
	}
}

// Without a cas store, run/start writes no intake record — the in-memory path is
// unchanged.
func TestRunStartNoIntakeRecordWithoutCASStore(t *testing.T) {
	h := buildTestHTTP(t, HTTPConfig{})
	if h.claimKV != nil {
		t.Fatal("claimKV set without a cas store")
	}
	sess := &session{id: "s1"}
	var toolName string
	for _, w := range h.srv.tools {
		toolName = w.Name
		break
	}
	res := decodeResult(t, h.runStart(sess, mkMsg("run/start", map[string]any{"name": toolName})))
	if _, ok := res["runId"].(string); !ok {
		t.Fatalf("run/start gave no runId: %v", res)
	}
	if _, ok := h.readIntake(context.Background(), res["runId"].(string)); ok {
		t.Error("readIntake returned a record with no cas store")
	}
}
