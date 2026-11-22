package parser_test

import (
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/lang/parser"
)

func TestInputDocsFromTripleSlashLines(t *testing.T) {
	src := `workflow W {
    /// project_dir: path to the project folder,
    /// which must exist.
    input project_dir: string
    // a plain note, not documentation
    input brief: string

    /// The number of tries.
    // an ordinary comment in between breaks the block
    input tries: number = 3
    //// four slashes are not a doc
    input mode: string = "x"
    /// Kept: blank lines above don't matter.
    input last: bool = false
    step S { log(project_dir) }
}
agent A {
    command: "echo"
    /// The repository to work in.
    input repo: string = "."
}`
	prog, err := parser.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	docs := map[string]string{}
	for _, m := range prog.Decls[0].Pipeline.Body {
		if m.Input != nil {
			docs[m.Input.Name] = m.Input.Doc
		}
	}
	want := map[string]string{
		"project_dir": "path to the project folder, which must exist.",
		"brief":       "",
		"tries":       "",
		"mode":        "",
		"last":        "Kept: blank lines above don't matter.",
	}
	for name, doc := range want {
		if docs[name] != doc {
			t.Errorf("%s: Doc = %q, want %q", name, docs[name], doc)
		}
	}
	if got := prog.Decls[1].Agent.Inputs[0].Doc; got != "The repository to work in." {
		t.Errorf("agent input Doc = %q", got)
	}
}
