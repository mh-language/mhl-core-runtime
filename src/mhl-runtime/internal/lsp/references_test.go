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
	refs := referencesAt(path, src, position{Line: 0, Character: 7}, false, nil, "")
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
	refs := referencesAt(defs, "export agent Reviewer {}\n", position{Line: 0, Character: 14}, false, nil, "")
	if len(refs) != 1 || refs[0].URI != pathToURI(main) {
		t.Fatalf("references = %+v, want aliased use in %s", refs, main)
	}
}

// TestReferencesAtFollowsImportedAliasAcrossFiles's own dir (t.TempDir()'s
// parent) has no ".git"/"go.mod", so if a declaration sits in a
// subdirectory the marker fallback alone stops at that subdirectory and
// never sees a caller one level up — the exact shape of a fresh, non-git
// mhl project laid out as "main.mh" + "modules/foo.mh". The editor-reported
// workspaceRoot (initialize's rootUri/workspaceFolders) is what fixes that.
func TestReferencesAtUsesWorkspaceRootAcrossSubdirectoriesWithoutProjectMarkers(t *testing.T) {
	root := t.TempDir()
	defs := filepath.Join(root, "modules", "defs.mh")
	main := filepath.Join(root, "main.mh")
	if err := os.MkdirAll(filepath.Dir(defs), 0o700); err != nil {
		t.Fatal(err)
	}
	defsSrc := "export tool Meupipe {\n  meuMetodo() -> { return 1 }\n}\n"
	if err := os.WriteFile(defs, []byte(defsSrc), 0o600); err != nil {
		t.Fatal(err)
	}
	mainSrc := "import { Meupipe } from \"modules/defs.mh\"\npipeline P { step S { Meupipe.meuMetodo() } }\n"
	if err := os.WriteFile(main, []byte(mainSrc), 0o600); err != nil {
		t.Fatal(err)
	}

	if refs := referencesAt(defs, defsSrc, position{Line: 0, Character: 14}, false, nil, ""); len(refs) != 0 {
		t.Fatalf("without workspaceRoot, references = %+v, want none (caller is outside modules/)", refs)
	}
	// Two hits in main.mh: the bare `import { Meupipe }` binding itself
	// (unlike an aliased `X as Y` import, the unaliased name in the import
	// list also resolves back to the declaration) and the `Meupipe.meuMetodo()`
	// call.
	refs := referencesAt(defs, defsSrc, position{Line: 0, Character: 14}, false, nil, root)
	if len(refs) != 2 {
		t.Fatalf("with workspaceRoot, references = %+v, want 2 hits in %s", refs, main)
	}
	for _, r := range refs {
		if r.URI != pathToURI(main) {
			t.Errorf("reference %+v not in %s", r, main)
		}
	}
}

func TestCodeLensesIncludeTopLevelAndToolMethodCounts(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main.mh")
	src := "tool Search {\n  find(query: string) -> string { return query }\n}\npipeline P { step S { var x = Search.find(\"hi\") } }\n"
	lenses := codeLenses(path, src, nil, "")
	if len(lenses) != 3 {
		t.Fatalf("got %d lenses (%+v), want tool, method and pipeline", len(lenses), lenses)
	}
	if lenses[0].Command.Title != "1 reference" || lenses[1].Command.Title != "1 reference" {
		t.Errorf("unexpected counts: %+v", lenses)
	}
}
