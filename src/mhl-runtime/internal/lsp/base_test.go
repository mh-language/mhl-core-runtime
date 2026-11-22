package lsp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// In a stage fragment, `base.` completes the base fragment's state, and
// hover / go-to-definition on `base.x` lead to its declaration there.
func TestBaseRefCompletionHoverDefinition(t *testing.T) {
	dir := t.TempDir()
	base := "import \"story/use.mh\"\npartial workflow W {\n    /// The project's folder.\n    input dir: string\n    var folder = \"\"\n    entry step A { goto Use }\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "main.mh"), []byte(base), 0o644); err != nil {
		t.Fatal(err)
	}
	frag, pos := posAtMarker(t, "partial workflow W stage Use {\n    step B {\n        log(base.fol§der)\n    }\n}\n")
	path := filepath.Join(dir, "story", "use.mh")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(frag), 0o644); err != nil {
		t.Fatal(err)
	}

	h := hoverAt(path, frag, pos)
	if h == nil || !strings.Contains(h.Contents.Value, "var folder") || !strings.Contains(h.Contents.Value, "main.mh:5") {
		t.Fatalf("hover = %v", h)
	}
	locs := definitionAt(path, frag, pos, nil)
	if len(locs) != 1 || !strings.HasSuffix(locs[0].URI, "/main.mh") || locs[0].Range.Start.Line != 4 {
		t.Fatalf("definition = %+v", locs)
	}

	typing, at := posAtMarker(t, "partial workflow W stage Use {\n    step B {\n        log(base.§)\n    }\n}\n")
	labels := map[string]string{}
	for _, it := range baseCompletionAt(path, typing, at) {
		labels[it.Label] = it.Detail
	}
	if labels["dir"] != "input · main.mh" || labels["folder"] != "var · main.mh" {
		t.Fatalf("completion = %v", labels)
	}
}
