package mcpserver

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/engine/runtime"
	"github.com/mh-language/mhl-core-runtime/internal/execsvc"
)

// runOnReplica runs src once the way execRun does with an `extension state`
// — its checkpoints in kv — from its own BaseDir, standing in for one
// replica of a deployment that shares nothing but the store.
func runOnReplica(t *testing.T, kv KVStore, src string, inputs map[string]any, resume bool) (string, *execsvc.Result, error) {
	t.Helper()
	dir := t.TempDir()
	file := filepath.Join(dir, "wf.mh")
	if err := os.WriteFile(file, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	ext := newExtStateStore(kv, "r1", nil)
	var out bytes.Buffer
	res, err := execsvc.Run(execsvc.Request{
		Source: file, BaseDir: dir, Session: "r1", Resume: resume, Inputs: inputs, Out: &out,
		StateStore: ext, LoopStateStore: extLoopStateStore{ext},
	})
	if entries, _ := os.ReadDir(filepath.Join(dir, runtime.StateDirName, "r1")); len(entries) > 0 {
		t.Errorf("run state was written to the replica's disk: %v", entries)
	}
	return out.String(), res, err
}

// A `mem` goes where the checkpoint goes: with an extension state, a run
// that stops on one replica resumes on another with the mem of its last
// completed step — and a write the failed step made is rolled back with it,
// not applied twice.
func TestExtStateCarriesMemAcrossReplicas(t *testing.T) {
	const src = `
workflow Counter {
    input crash: bool = false
    mem visits = 0
    output: { visits: visits }
    step Bump {
        visits = visits + 1
        if (crash) { fail("simulated crash after the write") }
    }
    step Again { visits = visits + 1 }
}
`
	kv := newFakeKV()
	if _, _, err := runOnReplica(t, kv, src, map[string]any{"crash": true}, false); err == nil {
		t.Fatal("first leg: expected the simulated crash")
	}
	_, res, err := runOnReplica(t, kv, src, map[string]any{"crash": false}, true)
	if err != nil {
		t.Fatalf("resumed leg: %v", err)
	}
	if res.Vars["visits"] != float64(2) {
		t.Errorf("visits = %v, want 2 (Bump's failed write rolled back, then Bump and Again once each)", res.Vars["visits"])
	}
}

// A loop's iteration checkpoint — and the mem it carries between iterations
// — goes to the extension state too, so the loop resumes on another replica
// at the iteration it stopped in, with that iteration's starting mem.
func TestExtStateCarriesLoopMemAcrossReplicas(t *testing.T) {
	const src = `
loop workflow Counter max 3 {
    input crash_at: number = 0
    mem n = 0
    step Bump {
        n = n + 1
        log("n=${n}")
        if (n == crash_at) { fail("simulated crash") }
    }
}
`
	kv := newFakeKV()
	first, _, err := runOnReplica(t, kv, src, map[string]any{"crash_at": float64(2)}, false)
	if err == nil {
		t.Fatalf("first leg: expected the simulated crash, got:\n%s", first)
	}
	second, res, err := runOnReplica(t, kv, src, map[string]any{"crash_at": float64(0)}, true)
	if err != nil {
		t.Fatalf("resumed leg: %v\n%s", err, second)
	}
	if strings.Contains(second, "n=1") || !strings.Contains(second, "n=2") || !strings.Contains(second, "n=3") {
		t.Errorf("resumed leg should continue from n=1 (iterations 2 and 3), got:\n%s", second)
	}
	if res.TerminalReason != "max_iterations" {
		t.Errorf("TerminalReason = %q, want max_iterations", res.TerminalReason)
	}
}
