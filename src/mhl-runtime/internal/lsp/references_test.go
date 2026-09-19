package lsp

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReferencesAtExcludesDeclarationAndFalseTextMatches(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main.mh")
	src := "agent Reviewer {}\n// Reviewer is only a comment\npipeline P {\n  step S {\n    var label = \"Reviewer\"\n    var result = Reviewer.run(\"hi\")\n  }\n}\n"
	refs := referencesAt(path, src, position{Line: 0, Character: 7}, false, nil)
	if len(refs) != 1 {
		t.Fatalf("references = %+v, want exactly the invocation", refs)
	}
	if refs[0].Range.Start.Line != 5 {
		t.Errorf("reference line = %d, want 5", refs[0].Range.Start.Line)
	}
}

func TestReferencesAtFollowsImportedAliasAcrossFiles(t *testing.T) {
	dir := t.TempDir()
	defs := filepath.Join(dir, "defs.mh")
	main := filepath.Join(dir, "main.mh")
	if err := os.WriteFile(defs, []byte("export agent Reviewer {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	src := "import { Reviewer as Judge } from \"defs.mh\"\npipeline P { step S { var x = Judge.run(\"hi\") } }\n"
	if err := os.WriteFile(main, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	refs := referencesAt(defs, "export agent Reviewer {}\n", position{Line: 0, Character: 14}, false, nil)
	if len(refs) != 1 || refs[0].URI != pathToURI(main) {
		t.Fatalf("references = %+v, want aliased use in %s", refs, main)
	}
}

func TestCodeLensesIncludeTopLevelAndToolMethodCounts(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main.mh")
	src := "tool Search {\n  find(query: string) -> string { return query }\n}\npipeline P { step S { var x = Search.find(\"hi\") } }\n"
	lenses := codeLenses(path, src, nil)
	if len(lenses) != 3 {
		t.Fatalf("got %d lenses (%+v), want tool, method and pipeline", len(lenses), lenses)
	}
	if lenses[0].Command.Title != "1 reference" || lenses[1].Command.Title != "1 reference" {
		t.Errorf("unexpected counts: %+v", lenses)
	}
}
