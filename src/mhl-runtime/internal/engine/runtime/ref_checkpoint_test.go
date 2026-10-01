package runtime_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/engine/runtime"
	"github.com/mh-language/mhl-core-runtime/internal/engine/value"
)

func TestStoreRoundTripPreservesRefIdentityAndCycles(t *testing.T) {
	store := runtime.NewStore(t.TempDir())
	shared := value.NewRef(map[string]any{"n": 1.0})
	cyc := value.NewRef(map[string]any{"name": "c"})
	cyc.Fields["self"] = cyc
	vars := map[string]any{
		"a":     shared,
		"b":     map[string]any{"deep": []any{shared}},
		"cyc":   cyc,
		"plain": map[string]any{"x": 1.0},
	}
	if err := runtime.CheckpointableVars(vars); err != nil {
		t.Fatalf("refs (cycles too) are checkpointable: %v", err)
	}
	if err := store.Save(&runtime.Checkpoint{Pipeline: "p", Variables: vars}); err != nil {
		t.Fatal(err)
	}
	cp, ok, err := store.Load("p")
	if err != nil || !ok {
		t.Fatalf("load: ok=%v err=%v", ok, err)
	}
	a, _ := cp.Variables["a"].(*value.Ref)
	b := cp.Variables["b"].(map[string]any)["deep"].([]any)[0].(*value.Ref)
	if a == nil || a != b || a.ID != shared.ID {
		t.Fatalf("one id must load as one object: a=%#v b=%#v", a, b)
	}
	c := cp.Variables["cyc"].(*value.Ref)
	if c.Fields["self"].(*value.Ref) != c {
		t.Fatal("a ref cycle must load as a cycle")
	}
	if _, isRef := cp.Variables["plain"].(*value.Ref); isRef {
		t.Fatal("a plain object must load as a plain object")
	}
}

// A checkpoint written before refs existed (state_schema 1, no markers)
// still loads, unchanged.
func TestStoreLoadsAVersion1Checkpoint(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, runtime.StateDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	legacy := `{"pipeline":"p","state_schema":1,"variables":{"o":{"x":1},"s":"v"}}`
	if err := os.WriteFile(filepath.Join(dir, "p.json"), []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	cp, ok, err := runtime.NewStore(root).Load("p")
	if err != nil || !ok {
		t.Fatalf("load: ok=%v err=%v", ok, err)
	}
	if err := cp.CompatibleWith(""); err != nil {
		t.Fatalf("a version-1 checkpoint is compatible: %v", err)
	}
	if o, ok := cp.Variables["o"].(map[string]any); !ok || o["x"] != 1.0 || cp.Variables["s"] != "v" {
		t.Fatalf("legacy variables = %#v", cp.Variables)
	}
}
