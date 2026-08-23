package cli_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yanjustino/mhl-runtime/internal/cli"
)

// TestRunKVMemorySetThenGetSucceeds proves the kv store wiring end-to-end:
// a set followed by a get on the same declared memory completes without
// error. The runtime never prints get's return value (this interpreter has
// no variable environment to hold it) — round-trip correctness of the
// underlying store itself is covered directly, with the retrieved value
// logged for visibility, in internal/features/memory/kv_test.go.
func TestRunKVMemorySetThenGetSucceeds(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	if err := os.WriteFile(main, []byte(`
memory session_mem {
    type: "kv"
    store: "memory"
}

pipeline P {
    step S {
        session_mem.set("attempt", "1")
        session_mem.get("attempt", "0")
    }
}
`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	var buf bytes.Buffer
	if err := cli.Run([]string{"run", main}, &buf); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(buf.String(), "executed 1 step(s)") {
		t.Errorf("unexpected output: %s", buf.String())
	}
}

func TestRunKVMemorySetWrongArgCount(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	if err := os.WriteFile(main, []byte(`
memory session_mem {
    type: "kv"
    store: "memory"
}

pipeline P {
    step S {
        session_mem.set("attempt")
    }
}
`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	var buf bytes.Buffer
	err := cli.Run([]string{"run", main}, &buf)
	if err == nil {
		t.Fatal("expected an error for set() with the wrong number of arguments")
	}
	if !strings.Contains(err.Error(), `set requires (key, value)`) {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRunKVMemoryUnsupportedStore(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	if err := os.WriteFile(main, []byte(`
memory session_mem {
    type: "kv"
    store: "redis"
}

pipeline P {
    step S {
        session_mem.set("k", "v")
    }
}
`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	var buf bytes.Buffer
	err := cli.Run([]string{"run", main}, &buf)
	if err == nil {
		t.Fatal("expected an error for an unsupported kv store")
	}
	if !strings.Contains(err.Error(), `store "redis" is not supported yet`) {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRunAppendLogMemoryWritesFile(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	logPath := filepath.Join(dir, "logs", "audit.log")
	src := `
memory audit_log {
    type: "append_log"
    path: "` + logPath + `"
}

pipeline P {
    step S1 {
        audit_log.append("first entry")
    }
    step S2 {
        audit_log.append("second entry")
    }
}
`
	if err := os.WriteFile(main, []byte(src), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	var buf bytes.Buffer
	if err := cli.Run([]string{"run", main}, &buf); err != nil {
		t.Fatalf("run: %v", err)
	}
	content, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("reading log file: %v", err)
	}
	if string(content) != "first entry\nsecond entry\n" {
		t.Errorf("log content = %q", string(content))
	}
}

func TestRunAppendLogMemoryMissingPath(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	if err := os.WriteFile(main, []byte(`
memory audit_log {
    type: "append_log"
}

pipeline P {
    step S {
        audit_log.append("entry")
    }
}
`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	var buf bytes.Buffer
	err := cli.Run([]string{"run", main}, &buf)
	if err == nil {
		t.Fatal("expected an error for an append_log memory with no path")
	}
	if !strings.Contains(err.Error(), `memory "audit_log" has no path`) {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRunMemoryNotFound(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	if err := os.WriteFile(main, []byte(`
pipeline P {
    step S {
        ghost_mem.set("k", "v")
    }
}
`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	var buf bytes.Buffer
	err := cli.Run([]string{"run", main}, &buf)
	if err == nil {
		t.Fatal("expected an error for a reference to an undeclared memory")
	}
	if !strings.Contains(err.Error(), `memory "ghost_mem" not found`) {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRunMemoryUnsupportedType(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	if err := os.WriteFile(main, []byte(`
memory project_rag {
    type: "vector"
    provider: "chroma"
}

pipeline P {
    step S {
        project_rag.get("k")
    }
}
`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	var buf bytes.Buffer
	err := cli.Run([]string{"run", main}, &buf)
	if err == nil {
		t.Fatal("expected an error for an unsupported memory type")
	}
	if !strings.Contains(err.Error(), `type "vector" is not supported yet`) {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRunMemoryWrongMethodForType(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	if err := os.WriteFile(main, []byte(`
memory audit_log {
    type: "append_log"
    path: "audit.log"
}

pipeline P {
    step S {
        audit_log.set("k", "v")
    }
}
`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	var buf bytes.Buffer
	err := cli.Run([]string{"run", main}, &buf)
	if err == nil {
		t.Fatal("expected an error for calling .set() on an append_log memory")
	}
	if !strings.Contains(err.Error(), `append_log memory has no method "set"`) {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRunJSONMemorySetThenGetPersistsToDisk(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	statePath := filepath.Join(dir, "state.json")
	src := `
memory session_mem {
    type: "json"
    path: "` + statePath + `"
}

pipeline P {
    step S {
        session_mem.set("attempt", "1")
        session_mem.get("attempt", "0")
    }
}
`
	if err := os.WriteFile(main, []byte(src), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	var buf bytes.Buffer
	if err := cli.Run([]string{"run", main}, &buf); err != nil {
		t.Fatalf("run: %v", err)
	}

	raw, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("reading state file: %v", err)
	}
	var decoded map[string]string
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("state file is not valid JSON: %v", err)
	}
	t.Logf("retrieved value: %v", decoded["attempt"])
	if decoded["attempt"] != "1" {
		t.Errorf("decoded[attempt] = %q, want %q", decoded["attempt"], "1")
	}
}

// TestRunJSONMemoryPersistsAcrossSeparateRuns proves the key differentiator
// from the ephemeral kv memory: two separate cli.Run invocations against
// the same path accumulate state, because the second run loads what the
// first one wrote instead of starting fresh.
func TestRunJSONMemoryPersistsAcrossSeparateRuns(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")

	firstMain := filepath.Join(dir, "first.mh")
	firstSrc := `
memory session_mem {
    type: "json"
    path: "` + statePath + `"
}

pipeline P {
    step S {
        session_mem.set("first_key", "first_value")
    }
}
`
	if err := os.WriteFile(firstMain, []byte(firstSrc), 0o644); err != nil {
		t.Fatalf("write first: %v", err)
	}
	var buf1 bytes.Buffer
	if err := cli.Run([]string{"run", firstMain}, &buf1); err != nil {
		t.Fatalf("first run: %v", err)
	}

	secondMain := filepath.Join(dir, "second.mh")
	secondSrc := `
memory session_mem {
    type: "json"
    path: "` + statePath + `"
}

pipeline P {
    step S {
        session_mem.set("second_key", "second_value")
    }
}
`
	if err := os.WriteFile(secondMain, []byte(secondSrc), 0o644); err != nil {
		t.Fatalf("write second: %v", err)
	}
	var buf2 bytes.Buffer
	if err := cli.Run([]string{"run", secondMain}, &buf2); err != nil {
		t.Fatalf("second run: %v", err)
	}

	raw, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("reading state file: %v", err)
	}
	var decoded map[string]string
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("state file is not valid JSON: %v", err)
	}
	if decoded["first_key"] != "first_value" {
		t.Errorf("first_key = %q, want %q (should survive the second run)", decoded["first_key"], "first_value")
	}
	if decoded["second_key"] != "second_value" {
		t.Errorf("second_key = %q, want %q", decoded["second_key"], "second_value")
	}
}

func TestRunJSONMemoryMissingPath(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	if err := os.WriteFile(main, []byte(`
memory session_mem {
    type: "json"
}

pipeline P {
    step S {
        session_mem.set("k", "v")
    }
}
`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	var buf bytes.Buffer
	err := cli.Run([]string{"run", main}, &buf)
	if err == nil {
		t.Fatal("expected an error for a json memory with no path")
	}
	if !strings.Contains(err.Error(), `memory "session_mem" has no path`) {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRunJSONMemorySetWrongArgCount(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	statePath := filepath.Join(dir, "state.json")
	src := `
memory session_mem {
    type: "json"
    path: "` + statePath + `"
}

pipeline P {
    step S {
        session_mem.set("attempt")
    }
}
`
	if err := os.WriteFile(main, []byte(src), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	var buf bytes.Buffer
	err := cli.Run([]string{"run", main}, &buf)
	if err == nil {
		t.Fatal("expected an error for set() with the wrong number of arguments")
	}
	if !strings.Contains(err.Error(), `set requires (key, value)`) {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRunJSONMemoryRemoveDeletesKeyOnDisk(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	statePath := filepath.Join(dir, "state.json")
	src := `
memory session_mem {
    type: "json"
    path: "` + statePath + `"
}

pipeline P {
    step S {
        session_mem.set("attempt", "1")
        session_mem.set("keep", "yes")
        session_mem.remove("attempt")
    }
}
`
	if err := os.WriteFile(main, []byte(src), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	var buf bytes.Buffer
	if err := cli.Run([]string{"run", main}, &buf); err != nil {
		t.Fatalf("run: %v", err)
	}

	raw, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("reading state file: %v", err)
	}
	var decoded map[string]string
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("state file is not valid JSON: %v", err)
	}
	if _, ok := decoded["attempt"]; ok {
		t.Errorf("decoded still has %q after remove, want it gone", "attempt")
	}
	if decoded["keep"] != "yes" {
		t.Errorf("decoded[keep] = %q, want %q (untouched by remove)", decoded["keep"], "yes")
	}
}

func TestRunJSONMemoryRemoveWrongArgCount(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	statePath := filepath.Join(dir, "state.json")
	src := `
memory session_mem {
    type: "json"
    path: "` + statePath + `"
}

pipeline P {
    step S {
        session_mem.remove("attempt", "extra")
    }
}
`
	if err := os.WriteFile(main, []byte(src), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	var buf bytes.Buffer
	err := cli.Run([]string{"run", main}, &buf)
	if err == nil {
		t.Fatal("expected an error for remove() with the wrong number of arguments")
	}
	if !strings.Contains(err.Error(), `remove requires (key)`) {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRunJSONLMemoryAppendWritesFile(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	logPath := filepath.Join(dir, "audit.jsonl")
	src := `
memory audit_log {
    type: "jsonl"
    path: "` + logPath + `"
}

pipeline P {
    step S {
        audit_log.append("first entry")
    }
}
`
	if err := os.WriteFile(main, []byte(src), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	var buf bytes.Buffer
	if err := cli.Run([]string{"run", main}, &buf); err != nil {
		t.Fatalf("run: %v", err)
	}

	f, err := os.Open(logPath)
	if err != nil {
		t.Fatalf("opening log file: %v", err)
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	if !scanner.Scan() {
		t.Fatal("expected at least one line in the jsonl file")
	}
	var entry struct {
		Value string `json:"value"`
		TS    string `json:"ts"`
	}
	if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
		t.Fatalf("line is not valid JSON: %v", err)
	}
	if entry.Value != "first entry" {
		t.Errorf("Value = %q", entry.Value)
	}
}

func TestRunJSONLMemoryMissingPath(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	if err := os.WriteFile(main, []byte(`
memory audit_log {
    type: "jsonl"
}

pipeline P {
    step S {
        audit_log.append("entry")
    }
}
`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	var buf bytes.Buffer
	err := cli.Run([]string{"run", main}, &buf)
	if err == nil {
		t.Fatal("expected an error for a jsonl memory with no path")
	}
	if !strings.Contains(err.Error(), `memory "audit_log" has no path`) {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRunJSONLMemoryWrongMethod(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	logPath := filepath.Join(dir, "audit.jsonl")
	src := `
memory audit_log {
    type: "jsonl"
    path: "` + logPath + `"
}

pipeline P {
    step S {
        audit_log.set("k", "v")
    }
}
`
	if err := os.WriteFile(main, []byte(src), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	var buf bytes.Buffer
	err := cli.Run([]string{"run", main}, &buf)
	if err == nil {
		t.Fatal("expected an error for calling .set() on a jsonl memory")
	}
	if !strings.Contains(err.Error(), `jsonl memory has no method "set"`) {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRunJSONMemorySetArrayValue(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	statePath := filepath.Join(dir, "state.json")
	src := `
memory session_mem {
    type: "json"
    path: "` + statePath + `"
}

pipeline P {
    step S {
        session_mem.set("tags", ["a", "b", "c"])
    }
}
`
	if err := os.WriteFile(main, []byte(src), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	var buf bytes.Buffer
	if err := cli.Run([]string{"run", main}, &buf); err != nil {
		t.Fatalf("run: %v", err)
	}

	raw, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("reading state file: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("state file is not valid JSON: %v", err)
	}
	tags, ok := decoded["tags"].([]any)
	if !ok || len(tags) != 3 || tags[0] != "a" || tags[1] != "b" || tags[2] != "c" {
		t.Errorf("decoded[tags] = %#v", decoded["tags"])
	}
}

func TestRunJSONMemorySetObjectValue(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	statePath := filepath.Join(dir, "state.json")
	src := `
memory session_mem {
    type: "json"
    path: "` + statePath + `"
}

pipeline P {
    step S {
        session_mem.set("cfg", {retries: 3, enabled: true})
    }
}
`
	if err := os.WriteFile(main, []byte(src), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	var buf bytes.Buffer
	if err := cli.Run([]string{"run", main}, &buf); err != nil {
		t.Fatalf("run: %v", err)
	}

	raw, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("reading state file: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("state file is not valid JSON: %v", err)
	}
	cfg, ok := decoded["cfg"].(map[string]any)
	if !ok || cfg["retries"] != 3.0 || cfg["enabled"] != true {
		t.Errorf("decoded[cfg] = %#v", decoded["cfg"])
	}
}

// TestRunJSONMemoryGetArrayObjectValue exercises .get() on keys holding
// array/object values (both an existing key and a missing key with a
// structured default). The runtime never prints get's return value; the
// decoded values read back from disk are logged here for visibility, and
// reading (get) must never mutate the file on disk: the state file after
// the run must still contain exactly what set() wrote, with no trace of
// the "missing" key's default value having been persisted.
func TestRunJSONMemoryGetArrayObjectValue(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	statePath := filepath.Join(dir, "state.json")
	src := `
memory session_mem {
    type: "json"
    path: "` + statePath + `"
}

pipeline P {
    step S {
        session_mem.set("tags", ["a", "b", "c"])
        session_mem.set("cfg", {retries: 3, enabled: true})
        session_mem.get("tags")
        session_mem.get("cfg")
        session_mem.get("missing", ["default"])
    }
}
`
	if err := os.WriteFile(main, []byte(src), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	var buf bytes.Buffer
	if err := cli.Run([]string{"run", main}, &buf); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(buf.String(), "executed 1 step(s)") {
		t.Errorf("unexpected output: %s", buf.String())
	}

	raw, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("reading state file: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("state file is not valid JSON: %v", err)
	}
	t.Logf("retrieved tags: %#v", decoded["tags"])
	t.Logf("retrieved cfg: %#v", decoded["cfg"])
	tags, ok := decoded["tags"].([]any)
	if !ok || len(tags) != 3 {
		t.Errorf("decoded[tags] = %#v", decoded["tags"])
	}
	cfg, ok := decoded["cfg"].(map[string]any)
	if !ok || cfg["retries"] != 3.0 {
		t.Errorf("decoded[cfg] = %#v", decoded["cfg"])
	}
	if _, present := decoded["missing"]; present {
		t.Errorf("get() must not persist the missing key's default value, got: %#v", decoded["missing"])
	}
}

// TestRunKVMemoryGetPathNavigatesNestedField exercises the "::"-delimited
// path navigation added to get() (e.g. session_mem.get("cfg::retries")) —
// see internal/features/memory.splitPathKey/resolvePath. The runtime still discards
// get's return value, so this only proves the path syntax parses and
// executes without error through the full pipeline; round-trip correctness
// of the navigation itself is covered directly in internal/features/memory/path_test.go.
func TestRunKVMemoryGetPathNavigatesNestedField(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	if err := os.WriteFile(main, []byte(`
memory session_mem {
    type: "kv"
    store: "memory"
}

pipeline P {
    step S {
        session_mem.set("cfg", {retries: 3, enabled: true})
        session_mem.set("tags", ["a", "b", "c"])
        session_mem.get("cfg::retries")
        session_mem.get("tags::1")
        session_mem.get("cfg::missing", "unset")
    }
}
`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	var buf bytes.Buffer
	if err := cli.Run([]string{"run", main}, &buf); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(buf.String(), "executed 1 step(s)") {
		t.Errorf("unexpected output: %s", buf.String())
	}
}

// TestRunJSONMemoryGetPathNavigatesNestedField is the json-memory
// counterpart of TestRunKVMemoryGetPathNavigatesNestedField.
func TestRunJSONMemoryGetPathNavigatesNestedField(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	statePath := filepath.Join(dir, "state.json")
	src := `
memory session_mem {
    type: "json"
    path: "` + statePath + `"
}

pipeline P {
    step S {
        session_mem.set("cfg", {retries: 3, enabled: true})
        session_mem.set("tags", ["a", "b", "c"])
        session_mem.get("cfg::retries")
        session_mem.get("tags::1")
        session_mem.get("cfg::missing", "unset")
    }
}
`
	if err := os.WriteFile(main, []byte(src), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	var buf bytes.Buffer
	if err := cli.Run([]string{"run", main}, &buf); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(buf.String(), "executed 1 step(s)") {
		t.Errorf("unexpected output: %s", buf.String())
	}

	// get() must never write back to disk, "::" path or not.
	raw, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("reading state file: %v", err)
	}
	if strings.Contains(string(raw), "missing") {
		t.Errorf("get() must not persist a missing path's default value: %s", raw)
	}
}

func TestRunKVMemorySetArrayObjectValue(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	if err := os.WriteFile(main, []byte(`
memory session_mem {
    type: "kv"
    store: "memory"
}

pipeline P {
    step S {
        session_mem.set("tags", ["a", "b"])
        session_mem.set("cfg", {retries: 3})
    }
}
`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	var buf bytes.Buffer
	if err := cli.Run([]string{"run", main}, &buf); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(buf.String(), "executed 1 step(s)") {
		t.Errorf("unexpected output: %s", buf.String())
	}
}

func TestRunAppendLogMemoryRejectsStructuredValue(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	logPath := filepath.Join(dir, "audit.log")
	src := `
memory audit_log {
    type: "append_log"
    path: "` + logPath + `"
}

pipeline P {
    step S {
        audit_log.append(["a", "b"])
    }
}
`
	if err := os.WriteFile(main, []byte(src), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	var buf bytes.Buffer
	err := cli.Run([]string{"run", main}, &buf)
	if err == nil {
		t.Fatal("expected an error for appending a structured value to an append_log memory")
	}
	if !strings.Contains(err.Error(), `append_log entries must be plain text`) {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRunJSONLMemoryAppendObjectValue(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	logPath := filepath.Join(dir, "audit.jsonl")
	src := `
memory audit_log {
    type: "jsonl"
    path: "` + logPath + `"
}

pipeline P {
    step S {
        audit_log.append({event: "started", retries: 3})
    }
}
`
	if err := os.WriteFile(main, []byte(src), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	var buf bytes.Buffer
	if err := cli.Run([]string{"run", main}, &buf); err != nil {
		t.Fatalf("run: %v", err)
	}

	f, err := os.Open(logPath)
	if err != nil {
		t.Fatalf("opening log file: %v", err)
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	if !scanner.Scan() {
		t.Fatal("expected at least one line in the jsonl file")
	}
	var entry struct {
		Value map[string]any `json:"value"`
		TS    string         `json:"ts"`
	}
	if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
		t.Fatalf("line is not valid JSON: %v", err)
	}
	if entry.Value["event"] != "started" || entry.Value["retries"] != 3.0 {
		t.Errorf("Value = %#v", entry.Value)
	}
}

// TestRunMemoryValueMaxDepthAllowed confirms exactly 10 levels of
// array nesting is accepted.
func TestRunMemoryValueMaxDepthAllowed(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	nested := strings.Repeat("[", 10) + "1" + strings.Repeat("]", 10)
	src := `
memory session_mem {
    type: "kv"
    store: "memory"
}

pipeline P {
    step S {
        session_mem.set("deep", ` + nested + `)
    }
}
`
	if err := os.WriteFile(main, []byte(src), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	var buf bytes.Buffer
	if err := cli.Run([]string{"run", main}, &buf); err != nil {
		t.Fatalf("run: %v", err)
	}
}

// TestRunMemoryValueExceedsMaxDepth confirms an 11th level of nesting is
// rejected with a clear error rather than silently truncated or crashing.
func TestRunMemoryValueExceedsMaxDepth(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	nested := strings.Repeat("[", 11) + "1" + strings.Repeat("]", 11)
	src := `
memory session_mem {
    type: "kv"
    store: "memory"
}

pipeline P {
    step S {
        session_mem.set("deep", ` + nested + `)
    }
}
`
	if err := os.WriteFile(main, []byte(src), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	var buf bytes.Buffer
	err := cli.Run([]string{"run", main}, &buf)
	if err == nil {
		t.Fatal("expected an error for a value nested more than 10 levels deep")
	}
	if !strings.Contains(err.Error(), "value nesting exceeds the maximum depth of 10") {
		t.Errorf("unexpected error: %v", err)
	}
}
