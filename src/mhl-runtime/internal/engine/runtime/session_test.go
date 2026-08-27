package runtime_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mh-language/mhl-core-runtime/internal/engine/runtime"
)

func TestNewSessionIDIsUniqueAndOpaque(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		id := runtime.NewSessionID()
		if id == "" || len(id) < 16 {
			t.Fatalf("suspicious session id %q", id)
		}
		if seen[id] {
			t.Fatalf("duplicate session id %q", id)
		}
		seen[id] = true
	}
}

func TestStoreSessionIsolatesCheckpoints(t *testing.T) {
	root := t.TempDir()
	base := runtime.NewStore(root)

	s1 := base.Session("sess-1")
	s2 := base.Session("sess-2")

	save := func(s *runtime.Store, next string) {
		if err := s.Save(&runtime.Checkpoint{Pipeline: "P", LastStep: "A", NextStep: next}); err != nil {
			t.Fatalf("save: %v", err)
		}
	}
	save(s1, "B")
	save(s2, "C")

	cp1, ok1, _ := s1.Load("P")
	cp2, ok2, _ := s2.Load("P")
	if !ok1 || !ok2 || cp1.NextStep != "B" || cp2.NextStep != "C" {
		t.Fatalf("sessions bled into each other: %+v / %+v", cp1, cp2)
	}
	// Each session got its own directory; neither wrote a top-level file.
	for _, id := range []string{"sess-1", "sess-2"} {
		if _, err := os.Stat(filepath.Join(root, runtime.StateDirName, id, "P.json")); err != nil {
			t.Errorf("missing %s/P.json: %v", id, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, runtime.StateDirName, "P.json")); !os.IsNotExist(err) {
		t.Errorf("a session-scoped Save must not write the legacy top-level P.json")
	}
	// The .latest pointer names the most recent writer.
	data, err := os.ReadFile(filepath.Join(root, runtime.StateDirName, "P.latest"))
	if err != nil {
		t.Fatalf("reading .latest: %v", err)
	}
	if got := strings.TrimSpace(string(data)); got != "sess-2" {
		t.Errorf("latest pointer = %q, want sess-2", got)
	}
}

func TestSessionEmptyIDReturnsUnscopedStore(t *testing.T) {
	base := runtime.NewStore(t.TempDir())
	if base.Session("") != base {
		t.Error("Session(\"\") should return the store unchanged")
	}
}

func TestResolveSession(t *testing.T) {
	root := t.TempDir()
	base := runtime.NewStore(root)
	stateDir := filepath.Join(root, runtime.StateDirName)

	// Fresh run: a brand-new id every time.
	a := runtime.ResolveSession(base, "P", "", false)
	b := runtime.ResolveSession(base, "P", "", false)
	if a == "" || b == "" || a == b {
		t.Fatalf("fresh runs should get distinct non-empty ids, got %q / %q", a, b)
	}

	// --session wins on a fresh run and on a resume.
	if got := runtime.ResolveSession(base, "P", "pinned", false); got != "pinned" {
		t.Errorf("--session on fresh run = %q, want pinned", got)
	}
	if got := runtime.ResolveSession(base, "P", "pinned", true); got != "pinned" {
		t.Errorf("--session on resume = %q, want pinned", got)
	}

	// --resume with no state at all: falls through to a fresh id.
	if got := runtime.ResolveSession(base, "P", "", true); got == "" {
		t.Error("resume with nothing to resume should still yield a fresh id")
	}

	// --resume follows the .latest pointer.
	if err := base.Session("live-1").Save(&runtime.Checkpoint{Pipeline: "P", NextStep: "B"}); err != nil {
		t.Fatalf("save: %v", err)
	}
	if got := runtime.ResolveSession(base, "P", "", true); got != "live-1" {
		t.Errorf("resume via .latest = %q, want live-1", got)
	}

	// --resume grace: a legacy top-level checkpoint yields the empty id, which
	// keeps the runner unscoped so Runner.Run's own Load finds it.
	root2 := t.TempDir()
	base2 := runtime.NewStore(root2)
	if err := os.MkdirAll(filepath.Join(root2, runtime.StateDirName), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root2, runtime.StateDirName, "P.json"), []byte(`{"pipeline":"P","next_step":"B"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := runtime.ResolveSession(base2, "P", "", true); got != "" {
		t.Errorf("resume of a legacy checkpoint = %q, want empty id", got)
	}
	_ = stateDir
}

func TestPriorVarsPrefersResultThenCheckpoint(t *testing.T) {
	root := t.TempDir()
	base := runtime.NewStore(root)

	// Nothing recorded yet.
	if v, err := runtime.PriorVars(base, "P", "latest"); err != nil || v != nil {
		t.Fatalf("PriorVars with no state = %v, %v", v, err)
	}

	// A crashed run leaves a checkpoint; PriorVars falls back to its vars.
	s := base.Session("s1")
	if err := s.Save(&runtime.Checkpoint{Pipeline: "P", NextStep: "B", Variables: map[string]any{"k": "from-checkpoint"}}); err != nil {
		t.Fatalf("save: %v", err)
	}
	v, err := runtime.PriorVars(base, "P", "latest")
	if err != nil || v["k"] != "from-checkpoint" {
		t.Fatalf("PriorVars from checkpoint = %v, %v", v, err)
	}

	// A completed run writes result.json, which wins over the checkpoint.
	if err := s.WriteResult("P", map[string]any{"k": "from-result"}); err != nil {
		t.Fatalf("WriteResult: %v", err)
	}
	v, err = runtime.PriorVars(base, "P", "latest")
	if err != nil || v["k"] != "from-result" {
		t.Fatalf("PriorVars from result = %v, %v", v, err)
	}

	// An explicit source pins a session regardless of .latest.
	v, err = runtime.PriorVars(base, "P", "session:s1")
	if err != nil || v["k"] != "from-result" {
		t.Fatalf("PriorVars from session:s1 = %v, %v", v, err)
	}
}

func TestPruneExpiredRemovesStaleSessionDirs(t *testing.T) {
	root := t.TempDir()
	base := runtime.NewStore(root)
	stateDir := filepath.Join(root, runtime.StateDirName)

	// A live session: fresh checkpoint, recent mtime — must be kept.
	if err := base.Session("live").Save(&runtime.Checkpoint{Pipeline: "P", NextStep: "B", TTLSeconds: int64((7 * 24 * time.Hour).Seconds())}); err != nil {
		t.Fatalf("save: %v", err)
	}

	// A stale session: only a result.json, back-dated well past the grace.
	stale := filepath.Join(stateDir, "stale")
	if err := os.MkdirAll(stale, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stale, "result.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-30 * 24 * time.Hour)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}

	if err := runtime.PruneExpired(stateDir); err != nil {
		t.Fatalf("PruneExpired: %v", err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Error("expected the stale session dir to be pruned")
	}
	if _, err := os.Stat(filepath.Join(stateDir, "live", "P.json")); err != nil {
		t.Errorf("live session dir was pruned: %v", err)
	}
}
