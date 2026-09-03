package lsp

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefinitionTopLevelNameSameFile(t *testing.T) {
	src, pos := posAtMarker(t, `agent Reviewer { engine: "cli/claude" }

pipeline Main {
    step Run {
        var out = Rev§iewer.run("hi")
    }
}
`)
	locs := definitionAt("/proj/main.mh", src, pos)
	if len(locs) != 1 {
		t.Fatalf("want 1 location, got %d: %+v", len(locs), locs)
	}
	if got := locs[0].URI; got != "file:///proj/main.mh" {
		t.Errorf("uri = %q, want file:///proj/main.mh", got)
	}
	if got := locs[0].Range.Start; got.Line != 0 || got.Character != 6 {
		t.Errorf("start = %+v, want {Line:0 Character:6}", got)
	}
	if got := locs[0].Range.End.Character; got != 14 {
		t.Errorf("end character = %d, want 14", got)
	}
}

func TestDefinitionResolvesEachDeclarationKind(t *testing.T) {
	src := `agent Rev { engine: "cli/claude" }
memory Store { type: "kv" }
tool files { read(path) -> fs.read(path) }
prompt Ask(q: string) { "${q}" }
type Ids = string[]
enum Status { Draft, Live }
extension mcp GitHub { transport: "http" }

pipeline Flow {
    input ids: Ids
    step S {
        var a = Rev
        var b = Store
        var c = files
        var d = Ask
        var e = GitHub
        var f = Status
    }
}
`
	cases := []struct {
		name     string
		wantLine int
	}{
		{"Rev", 0},
		{"Store", 1},
		{"files", 2},
		{"Ask", 3},
		{"Ids", 4},
		{"Status", 5},
		{"GitHub", 6},
	}
	for _, c := range cases {
		// Cursor on the reference (2nd whole-word occurrence), not the decl.
		pos := positionOfNthWord(t, src, c.name, 2)
		locs := definitionAt("/p/main.mh", src, pos)
		if len(locs) != 1 {
			t.Fatalf("%s: want 1 location, got %+v", c.name, locs)
		}
		if locs[0].Range.Start.Line != c.wantLine {
			t.Errorf("%s: start line = %d, want %d", c.name, locs[0].Range.Start.Line, c.wantLine)
		}
	}
}

func TestDefinitionToolMethod(t *testing.T) {
	src, pos := posAtMarker(t, `tool files {
    read_file(path) -> fs.read(path)
}

pipeline P {
    step S { var x = files.read_§file("a") }
}
`)
	locs := definitionAt("/p/main.mh", src, pos)
	if len(locs) != 1 {
		t.Fatalf("want 1 location, got %+v", locs)
	}
	if locs[0].Range.Start.Line != 1 {
		t.Errorf("start line = %d, want 1 (the method declaration)", locs[0].Range.Start.Line)
	}
	if locs[0].Range.Start.Character != 4 {
		t.Errorf("start character = %d, want 4", locs[0].Range.Start.Character)
	}
}

func TestDefinitionEnumVariant(t *testing.T) {
	src, pos := posAtMarker(t, `enum Status { Draft, Published }

pipeline P {
    step S { var x = Status.Pub§lished }
}
`)
	locs := definitionAt("/p/main.mh", src, pos)
	if len(locs) != 1 {
		t.Fatalf("want 1 location, got %+v", locs)
	}
	if locs[0].Range.Start.Line != 0 {
		t.Errorf("start line = %d, want 0", locs[0].Range.Start.Line)
	}
}

func TestDefinitionCrossFile(t *testing.T) {
	dir := t.TempDir()
	other := filepath.Join(dir, "agents.mh")
	if err := os.WriteFile(other, []byte("agent Helper {\n  engine: \"cli/claude\"\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	main := filepath.Join(dir, "main.mh")
	src, pos := posAtMarker(t, "pipeline Main {\n  step S {\n    var x = Hel§per.run(\"hi\")\n  }\n}\n")
	if err := os.WriteFile(main, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	locs := definitionAt(main, src, pos)
	if len(locs) != 1 {
		t.Fatalf("want 1 location, got %+v", locs)
	}
	if locs[0].URI != pathToURI(other) {
		t.Errorf("uri = %q, want %q", locs[0].URI, pathToURI(other))
	}
	if locs[0].Range.Start.Line != 0 {
		t.Errorf("start line = %d, want 0", locs[0].Range.Start.Line)
	}
}

func TestDefinitionImportPath(t *testing.T) {
	dir := t.TempDir()
	lib := filepath.Join(dir, "lib.mh")
	if err := os.WriteFile(lib, []byte("agent A {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	main := filepath.Join(dir, "main.mh")
	src, pos := posAtMarker(t, `import { A } from "./li§b.mh"`)
	if err := os.WriteFile(main, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	locs := definitionAt(main, src, pos)
	if len(locs) != 1 {
		t.Fatalf("want 1 location, got %+v", locs)
	}
	if locs[0].URI != pathToURI(lib) {
		t.Errorf("uri = %q, want %q", locs[0].URI, pathToURI(lib))
	}
	if locs[0].Range != (rangeT{}) {
		t.Errorf("range = %+v, want zero (whole-file jump)", locs[0].Range)
	}
}

func TestDefinitionImportPathUnresolved(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	src, pos := posAtMarker(t, `import { A } from "./mis§sing.mh"`)
	if err := os.WriteFile(main, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	if locs := definitionAt(main, src, pos); locs != nil {
		t.Fatalf("want nil for unresolved import, got %+v", locs)
	}
}

func TestDefinitionUnknownIdentifier(t *testing.T) {
	src, pos := posAtMarker(t, "pipeline P {\n  step S { var x = Nope§thing.run() }\n}\n")
	if locs := definitionAt(t.TempDir()+"/main.mh", src, pos); locs != nil {
		t.Fatalf("want nil, got %+v", locs)
	}
}

func TestDefinitionOnWhitespaceReturnsNil(t *testing.T) {
	src, pos := posAtMarker(t, "agent A {}\n§\npipeline P {}\n")
	if locs := definitionAt("/p/main.mh", src, pos); locs != nil {
		t.Fatalf("want nil on blank line, got %+v", locs)
	}
}

// positionOfNthWord returns the position of the start of the n-th (1-based)
// whole-word occurrence of word in src.
func positionOfNthWord(t *testing.T, src, word string, n int) position {
	t.Helper()
	count := 0
	for i := 0; i+len(word) <= len(src); i++ {
		if src[i:i+len(word)] != word {
			continue
		}
		if i > 0 && isIdentByte(src[i-1]) {
			continue
		}
		if i+len(word) < len(src) && isIdentByte(src[i+len(word)]) {
			continue
		}
		count++
		if count == n {
			return offsetToPos(src, i)
		}
	}
	t.Fatalf("word %q occurrence #%d not found", word, n)
	return position{}
}
