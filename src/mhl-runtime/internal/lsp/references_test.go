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

func TestReferenceFilesSkipsNodeModulesAndDotDirectories(t *testing.T) {
	root := t.TempDir()
	main := filepath.Join(root, "main.mh")
	mainSrc := "pipeline P {}\n"
	if err := os.WriteFile(main, []byte(mainSrc), 0o600); err != nil {
		t.Fatal(err)
	}
	noise := filepath.Join(root, "node_modules", "pkg", "noise.mh")
	if err := os.MkdirAll(filepath.Dir(noise), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(noise, []byte("pipeline Noise {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	hidden := filepath.Join(root, ".git", "hidden.mh")
	if err := os.MkdirAll(filepath.Dir(hidden), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hidden, []byte("pipeline Hidden {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	files := referenceFiles(main, mainSrc, nil, root)
	if _, ok := files[filepath.Clean(noise)]; ok {
		t.Errorf("referenceFiles scanned node_modules: %v", files)
	}
	if _, ok := files[filepath.Clean(hidden)]; ok {
		t.Errorf("referenceFiles scanned a dot-directory: %v", files)
	}
	if _, ok := files[filepath.Clean(main)]; !ok {
		t.Errorf("referenceFiles missed main.mh: %v", files)
	}
}

// TestCodeLensesSkipsControlFlowStatementsInsideToolMethods is a real
// regression: the tool-method regex only checked "identifier at line start
// followed by (", a shape `if (...)`, `while (...)` etc. match just as well
// as a real `name(params) -> {` declaration, since declarationLocations
// scans a tool's whole body flat with no notion of nesting. `if`/`while`
// ended up with their own bogus "0 references" CodeLens.
func TestCodeLensesSkipsControlFlowStatementsInsideToolMethods(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main.mh")
	src := "tool Files {\n" +
		"    read(path: string): string -> {\n" +
		"        if (fs.exists(path)) return fs.read(path)\n" +
		"        while (false) {}\n" +
		"        return \"\"\n" +
		"    }\n" +
		"}\n"
	lenses := codeLenses(path, src, nil, "")
	if len(lenses) != 2 {
		t.Fatalf("got %d lenses (%+v), want exactly the tool and read() decls", len(lenses), lenses)
	}
	for _, lens := range lenses {
		if word, _, ok := identAt(lineAt(src, lens.Range.Start.Line), lens.Range.Start.Character+1); !ok || word == "if" || word == "while" {
			t.Errorf("lens %+v landed on a control-flow statement, not a declaration", lens)
		}
	}
}

// TestReferencesAtOnToolMethodDeclarationFindsCallSite is a real regression:
// clicking a tool method's own "N references" CodeLens (its declaration
// line) resolved to nothing, because declLocRe only ever captures the
// enclosing tool's own name — the ordinary top-level-name lookup
// definitionAt otherwise relies on never has an entry for the method name
// itself, only for the tool. A `Receiver.method()` call site resolves fine;
// standing directly on the declaration is what broke.
func TestReferencesAtOnToolMethodDeclarationFindsCallSite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main.mh")
	src := "export tool WorkItemActions {\n" +
		"    usage(project_id: string): any -> {\n" +
		"        return project_id\n" +
		"    }\n" +
		"}\n" +
		"\n" +
		"pipeline P {\n" +
		"    step S {\n" +
		"        var x = WorkItemActions.usage(\"a\")\n" +
		"    }\n" +
		"}\n"
	// Cursor on "usage" in its own declaration (line 1, right after the
	// leading 4 spaces).
	refs := referencesAt(path, src, position{Line: 1, Character: 6}, false, nil, "")
	if len(refs) != 1 {
		t.Fatalf("references = %+v, want exactly the call site", refs)
	}
	if refs[0].Range.Start.Line != 8 {
		t.Errorf("reference line = %d, want 8 (the call site)", refs[0].Range.Start.Line)
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
