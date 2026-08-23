package memory_test

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/yanjustino/mhl-runtime/internal/features/memory"
)

type jsonlEntry struct {
	Value string `json:"value"`
	TS    string `json:"ts"`
}

func TestAppendJSONLCreatesFileAndWritesLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")

	if err := memory.AppendJSONL(path, "first entry"); err != nil {
		t.Fatalf("AppendJSONL: %v", err)
	}

	lines := readLines(t, path)
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d: %v", len(lines), lines)
	}
	var entry jsonlEntry
	if err := json.Unmarshal([]byte(lines[0]), &entry); err != nil {
		t.Fatalf("line is not valid JSON: %v", err)
	}
	if entry.Value != "first entry" {
		t.Errorf("Value = %q", entry.Value)
	}
	if _, err := time.Parse(time.RFC3339, entry.TS); err != nil {
		t.Errorf("TS = %q is not a valid RFC3339 timestamp: %v", entry.TS, err)
	}
}

func TestAppendJSONLAddsIndependentLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")

	if err := memory.AppendJSONL(path, "line 1"); err != nil {
		t.Fatalf("AppendJSONL: %v", err)
	}
	if err := memory.AppendJSONL(path, "line 2"); err != nil {
		t.Fatalf("AppendJSONL: %v", err)
	}

	lines := readLines(t, path)
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d: %v", len(lines), lines)
	}
	var first, second jsonlEntry
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatalf("line 1 is not valid JSON: %v", err)
	}
	if err := json.Unmarshal([]byte(lines[1]), &second); err != nil {
		t.Fatalf("line 2 is not valid JSON: %v", err)
	}
	if first.Value != "line 1" || second.Value != "line 2" {
		t.Errorf("got values %q, %q", first.Value, second.Value)
	}
}

func TestAppendJSONLAcceptsArrayValue(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")

	if err := memory.AppendJSONL(path, []any{"a", "b"}); err != nil {
		t.Fatalf("AppendJSONL: %v", err)
	}
	lines := readLines(t, path)
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d: %v", len(lines), lines)
	}
	var entry struct {
		Value []any  `json:"value"`
		TS    string `json:"ts"`
	}
	if err := json.Unmarshal([]byte(lines[0]), &entry); err != nil {
		t.Fatalf("line is not valid JSON: %v", err)
	}
	if len(entry.Value) != 2 || entry.Value[0] != "a" || entry.Value[1] != "b" {
		t.Errorf("Value = %#v", entry.Value)
	}
}

func TestAppendJSONLAcceptsObjectValue(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")

	if err := memory.AppendJSONL(path, map[string]any{"event": "started"}); err != nil {
		t.Fatalf("AppendJSONL: %v", err)
	}
	lines := readLines(t, path)
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d: %v", len(lines), lines)
	}
	var entry struct {
		Value map[string]any `json:"value"`
		TS    string         `json:"ts"`
	}
	if err := json.Unmarshal([]byte(lines[0]), &entry); err != nil {
		t.Fatalf("line is not valid JSON: %v", err)
	}
	if entry.Value["event"] != "started" {
		t.Errorf("Value = %#v", entry.Value)
	}
}

func TestAppendJSONLCreatesMissingParentDirs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "logs", "nested", "audit.jsonl")

	if err := memory.AppendJSONL(path, "entry"); err != nil {
		t.Fatalf("AppendJSONL: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("expected file to exist: %v", err)
	}
}

func readLines(t *testing.T, path string) []string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()
	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan %s: %v", path, err)
	}
	return lines
}
