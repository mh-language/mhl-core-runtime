package lsp

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProviderCompletionInsideFrom(t *testing.T) {
	dir := t.TempDir()
	writeLockedExtension(t, dir, "mhl.data.redis", "data", "incr")
	writeLockedExtension(t, dir, "mhl.data.postgres", "data", "query")
	writeLockedExtension(t, dir, "mhl.state.sqlite", "state", "get")
	lock := `{"extensions":{` +
		`"mhl.data.redis":{"version":"1.0.0","sha256":""},` +
		`"mhl.data.postgres":{"version":"1.0.0","sha256":""},` +
		`"mhl.state.sqlite":{"version":"1.0.0","sha256":""}}}`
	if err := os.WriteFile(filepath.Join(dir, ".mhl", "extensions.lock"), []byte(lock), 0o644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "main.mh")

	tests := []struct {
		name      string
		line      string
		wantIDs   []string
		wantStart int // textEdit start column
	}{
		{name: "just opened quote", line: `extension data Cache from "`, wantIDs: []string{"mhl.data.postgres", "mhl.data.redis"}, wantStart: 27},
		{name: "partial dotted id", line: `extension data Cache from "mhl.`, wantIDs: []string{"mhl.data.postgres", "mhl.data.redis"}, wantStart: 27},
		{name: "other kind", line: `extension state S from "`, wantIDs: []string{"mhl.state.sqlite"}, wantStart: 24},
		{name: "kind nobody serves", line: `extension blob B from "`, wantIDs: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			items := completionAt(path, tt.line, position{Line: 0, Character: len(tt.line)})
			if len(items) != len(tt.wantIDs) {
				t.Fatalf("got %d items %+v, want %v", len(items), items, tt.wantIDs)
			}
			for i, it := range items {
				if it.Label != tt.wantIDs[i] {
					t.Fatalf("item %d = %q, want %q", i, it.Label, tt.wantIDs[i])
				}
				if it.TextEdit == nil || it.TextEdit.NewText != it.Label ||
					it.TextEdit.Range.Start.Character != tt.wantStart || it.TextEdit.Range.End.Character != len(tt.line) {
					t.Fatalf("item %q textEdit = %+v, want [%d,%d) -> label", it.Label, it.TextEdit, tt.wantStart, len(tt.line))
				}
			}
		})
	}
}

// The `"` trigger character opening any other string must not pop up the
// general keyword list.
func TestCompletionJustOpenedStringIsEmpty(t *testing.T) {
	for _, line := range []string{`    var x = "`, `log("`} {
		if items := completionAt("", line, position{Line: 0, Character: len(line)}); len(items) != 0 {
			t.Fatalf("%q: got %d items, want none", line, len(items))
		}
	}
	// A closed string followed by more code still completes normally.
	line := `var x = "a" + `
	if items := completionAt("", line, position{Line: 0, Character: len(line)}); len(items) == 0 {
		t.Fatalf("%q: expected the general completion list", line)
	}
}
