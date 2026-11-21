package lsp

import (
	"os"
	"path/filepath"
	"testing"
)

// Sibling-file lookups (goto targets, definition) reach a .mhl fragment, and
// work from a .mhl buffer: ".mhl" is an alias of ".mh".
func TestGotoTargetReachesMhlSibling(t *testing.T) {
	dir := t.TempDir()
	sibling := filepath.Join(dir, "sibling.mhl")
	if err := os.WriteFile(sibling, []byte("partial workflow Discovery {\n    step BriefGenerate { log(\"brief\") }\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	main := filepath.Join(dir, "main.mhl")
	src, pos := posAtMarker(t, `partial workflow Discovery {
    entry step Dispatch {
        goto BriefGe§nerate
    }
}
`)
	if err := os.WriteFile(main, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	locs := definitionAt(main, src, pos, nil)
	if len(locs) != 1 || locs[0].URI != pathToURI(sibling) {
		t.Fatalf("definition = %+v, want one location in %s", locs, sibling)
	}
	if items := completionAt(main, src, pos); !hasLabel(items, "BriefGenerate") {
		t.Errorf("expected BriefGenerate from sibling.mhl in goto completion, got: %+v", items)
	}
}
