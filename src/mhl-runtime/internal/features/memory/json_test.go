package memory_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/features/memory"
)

func TestJSONStoreSetThenGet(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	s := memory.NewJSONStore()

	if err := s.Set(path, "attempt", "1"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, err := s.Get(path, "attempt", "0")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "1" {
		t.Errorf("Get = %q, want %q", got, "1")
	}
}

func TestJSONStoreGetMissingReturnsDefault(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	s := memory.NewJSONStore()

	got, err := s.Get(path, "attempt", "0")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "0" {
		t.Errorf("Get = %q, want default %q", got, "0")
	}
}

func TestJSONStoreMultipleKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	s := memory.NewJSONStore()

	if err := s.Set(path, "a", "1"); err != nil {
		t.Fatalf("Set a: %v", err)
	}
	if err := s.Set(path, "b", "2"); err != nil {
		t.Fatalf("Set b: %v", err)
	}
	if got, _ := s.Get(path, "a", ""); got != "1" {
		t.Errorf("a = %q", got)
	}
	if got, _ := s.Get(path, "b", ""); got != "2" {
		t.Errorf("b = %q", got)
	}
}

// TestJSONStorePersistsAcrossInstances is the key differentiator from the
// ephemeral KVStore: a fresh JSONStore instance pointed at the same path
// must see data written by a previous instance.
func TestJSONStorePersistsAcrossInstances(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	first := memory.NewJSONStore()
	if err := first.Set(path, "attempt", "1"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	second := memory.NewJSONStore()
	got, err := second.Get(path, "attempt", "0")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "1" {
		t.Errorf("Get on a fresh instance = %q, want %q (persisted from the first instance)", got, "1")
	}
}

func TestJSONStoreFileContentIsValidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	s := memory.NewJSONStore()

	if err := s.Set(path, "attempt", "1"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var decoded map[string]string
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("file content is not valid JSON: %v", err)
	}
	if decoded["attempt"] != "1" {
		t.Errorf("decoded[attempt] = %q", decoded["attempt"])
	}
}

func TestJSONStoreSetThenGetArrayValue(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	s := memory.NewJSONStore()

	tags := []any{"a", "b", "c"}
	if err := s.Set(path, "tags", tags); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, err := s.Get(path, "tags", nil)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	gotSlice, ok := got.([]any)
	if !ok || len(gotSlice) != 3 || gotSlice[0] != "a" || gotSlice[1] != "b" || gotSlice[2] != "c" {
		t.Errorf("Get = %#v, want %#v", got, tags)
	}
}

func TestJSONStoreSetThenGetObjectValue(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	s := memory.NewJSONStore()

	cfg := map[string]any{"retries": 3.0, "enabled": true}
	if err := s.Set(path, "cfg", cfg); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, err := s.Get(path, "cfg", nil)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	gotMap, ok := got.(map[string]any)
	if !ok || gotMap["retries"] != 3.0 || gotMap["enabled"] != true {
		t.Errorf("Get = %#v, want %#v", got, cfg)
	}
}

// TestJSONStoreLoadsPreExistingStructuredFile confirms the positive side
// effect noted in the design analysis: a .json file that already exists on
// disk with array/object values (e.g. written by another tool, or hand
// edited) now loads successfully instead of failing to parse.
func TestJSONStoreLoadsPreExistingStructuredFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	if err := os.WriteFile(path, []byte(`{"tags": ["x", "y"], "cfg": {"retries": 3}}`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	s := memory.NewJSONStore()
	got, err := s.Get(path, "tags", nil)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	gotSlice, ok := got.([]any)
	if !ok || len(gotSlice) != 2 || gotSlice[0] != "x" || gotSlice[1] != "y" {
		t.Errorf("tags = %#v", got)
	}
}

func TestJSONStoreGetPathNavigatesIntoObjectField(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	s := memory.NewJSONStore()

	cfg := map[string]any{"retries": 3.0, "enabled": true}
	if err := s.Set(path, "cfg", cfg); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, err := s.Get(path, "cfg::retries", nil)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != 3.0 {
		t.Errorf("Get(cfg::retries) = %v, want 3.0", got)
	}
}

func TestJSONStoreGetPathNavigatesIntoArrayIndex(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	s := memory.NewJSONStore()

	if err := s.Set(path, "tags", []any{"a", "b", "c"}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, err := s.Get(path, "tags::1", nil)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "b" {
		t.Errorf("Get(tags::1) = %v, want %q", got, "b")
	}
}

func TestJSONStoreGetPathMissingSegmentReturnsDefault(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	s := memory.NewJSONStore()

	if err := s.Set(path, "cfg", map[string]any{"retries": 3.0}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, err := s.Get(path, "cfg::timeout", "unset")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "unset" {
		t.Errorf("Get(cfg::timeout) = %v, want default %q", got, "unset")
	}
}

func TestJSONStoreGetPathPersistsAcrossInstances(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	first := memory.NewJSONStore()
	if err := first.Set(path, "cfg", map[string]any{"retries": 3.0}); err != nil {
		t.Fatalf("Set: %v", err)
	}

	second := memory.NewJSONStore()
	got, err := second.Get(path, "cfg::retries", nil)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != 3.0 {
		t.Errorf("Get(cfg::retries) on a fresh instance = %v, want 3.0", got)
	}
}

func TestJSONStoreRemoveTopLevelKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	s := memory.NewJSONStore()

	if err := s.Set(path, "a", "1"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := s.Set(path, "b", "2"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	removed, err := s.Remove(path, "a")
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if !removed {
		t.Error("Remove(a) = false, want true")
	}
	if got, _ := s.Get(path, "a", nil); got != nil {
		t.Errorf("Get(a) after Remove = %v, want nil", got)
	}
	if got, _ := s.Get(path, "b", nil); got != "2" {
		t.Errorf("Get(b) after Remove(a) = %v, want %q (untouched)", got, "2")
	}
}

func TestJSONStoreRemoveMissingKeyReturnsFalse(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	s := memory.NewJSONStore()

	removed, err := s.Remove(path, "nope")
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if removed {
		t.Error("Remove(nope) = true, want false for a key that was never set")
	}
}

func TestJSONStoreRemoveNestedObjectField(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	s := memory.NewJSONStore()

	cfg := map[string]any{"retries": 3.0, "enabled": true}
	if err := s.Set(path, "cfg", cfg); err != nil {
		t.Fatalf("Set: %v", err)
	}
	removed, err := s.Remove(path, "cfg::retries")
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if !removed {
		t.Error("Remove(cfg::retries) = false, want true")
	}
	if got, _ := s.Get(path, "cfg::retries", "unset"); got != "unset" {
		t.Errorf("Get(cfg::retries) after Remove = %v, want default", got)
	}
	if got, _ := s.Get(path, "cfg::enabled", nil); got != true {
		t.Errorf("Get(cfg::enabled) after Remove(cfg::retries) = %v, want true (untouched)", got)
	}
}

func TestJSONStoreRemoveNestedArrayIndex(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	s := memory.NewJSONStore()

	if err := s.Set(path, "tags", []any{"a", "b", "c"}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	removed, err := s.Remove(path, "tags::1")
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if !removed {
		t.Error("Remove(tags::1) = false, want true")
	}
	got, err := s.Get(path, "tags", nil)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	gotSlice, ok := got.([]any)
	if !ok || len(gotSlice) != 2 || gotSlice[0] != "a" || gotSlice[1] != "c" {
		t.Errorf("Get(tags) after Remove(tags::1) = %#v, want [a c]", got)
	}
}

func TestJSONStoreRemovePersistsToDisk(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	first := memory.NewJSONStore()
	if err := first.Set(path, "a", "1"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if _, err := first.Remove(path, "a"); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	second := memory.NewJSONStore()
	got, err := second.Get(path, "a", nil)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != nil {
		t.Errorf("Get(a) on a fresh instance = %v, want nil (removal persisted)", got)
	}
}

func TestJSONStoreMalformedExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	if err := os.WriteFile(path, []byte("not json"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	s := memory.NewJSONStore()
	_, err := s.Get(path, "attempt", "0")
	if err == nil {
		t.Fatal("expected an error for a malformed existing JSON file")
	}
}
