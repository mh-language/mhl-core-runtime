package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type graphDoc struct {
	OK          bool `json:"ok"`
	Diagnostics []struct {
		Line    int    `json:"line"`
		Message string `json:"message"`
	} `json:"diagnostics"`
	Declarations []struct {
		Kind string `json:"kind"`
		Name string `json:"name"`
	} `json:"declarations"`
	Workflows []struct {
		Name  string `json:"name"`
		Nodes []struct {
			ID    string   `json:"id"`
			Calls []string `json:"calls"`
		} `json:"nodes"`
		Edges []struct {
			From, To, Kind string
		} `json:"edges"`
	} `json:"workflows"`
}

func runGraphDoc(t *testing.T, args []string, stdin string) graphDoc {
	t.Helper()
	if stdin != "" {
		old := graphStdin
		graphStdin = strings.NewReader(stdin)
		t.Cleanup(func() { graphStdin = old })
	}
	var buf bytes.Buffer
	if err := Run(append([]string{"graph"}, args...), &buf); err != nil {
		t.Fatalf("mhl graph: %v", err)
	}
	var doc graphDoc
	if err := json.Unmarshal(buf.Bytes(), &doc); err != nil {
		t.Fatalf("stdout is not one JSON object: %v\n%s", err, buf.String())
	}
	return doc
}

const graphSrc = `agent Writer { engine: "cli/claude" }

workflow Draft {
    input topic: string = "x"
    var text = ""
    step Write { self.text = Writer.run(prompt: topic) }
    step Check { if (text == "") goto Write }
}
`

func TestGraphFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "draft.mh")
	if err := os.WriteFile(path, []byte(graphSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	doc := runGraphDoc(t, []string{path}, "")
	if !doc.OK || len(doc.Workflows) != 1 {
		t.Fatalf("doc = %+v", doc)
	}
	w := doc.Workflows[0]
	if w.Name != "Draft" || len(w.Nodes) != 2 || w.Nodes[0].Calls[0] != "Writer.run" {
		t.Errorf("workflow = %+v", w)
	}
	var kinds []string
	for _, e := range w.Edges {
		kinds = append(kinds, e.From+">"+e.To+":"+e.Kind)
	}
	want := "$start>Write:next Write>Check:next Check>Write:goto Check>$end:next"
	if strings.Join(kinds, " ") != want {
		t.Errorf("edges = %v, want %s", kinds, want)
	}
	if len(doc.Declarations) != 1 || doc.Declarations[0].Kind != "agent" {
		t.Errorf("declarations = %+v", doc.Declarations)
	}
}

// --stdin graphs an unsaved buffer; the path need not exist.
func TestGraphStdin(t *testing.T) {
	path := filepath.Join(t.TempDir(), "unsaved.mh")
	doc := runGraphDoc(t, []string{path, "--stdin"}, graphSrc)
	if !doc.OK || doc.Workflows[0].Name != "Draft" {
		t.Fatalf("doc = %+v", doc)
	}
}

// A source that does not parse is reported, not a command failure.
func TestGraphParseError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "broken.mh")
	doc := runGraphDoc(t, []string{path, "--stdin"}, "workflow W {\n    step A {\n}\n")
	if doc.OK || len(doc.Diagnostics) == 0 || len(doc.Workflows) != 0 {
		t.Fatalf("doc = %+v", doc)
	}
}

func TestGraphUsage(t *testing.T) {
	for _, args := range [][]string{{}, {"--bogus", "x.mh"}, {"a.mh", "b.mh"}} {
		if err := Run(append([]string{"graph"}, args...), &bytes.Buffer{}); err == nil {
			t.Errorf("mhl graph %v: want error", args)
		}
	}
}

func TestGraphMergedProjectsEveryPartialFragment(t *testing.T) {
	dir := t.TempDir()
	write := func(name, src string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	main := write("main.mh", `import "part.mh"
partial workflow W {
    entry step A { log("a") }
}
`)
	part := write("part.mh", `partial workflow W {
    step B { goto A }
}
`)
	ids := func(doc graphDoc) []string {
		var out []string
		for _, n := range doc.Workflows[0].Nodes {
			out = append(out, n.ID)
		}
		return out
	}
	if got := ids(runGraphDoc(t, []string{main}, "")); strings.Join(got, ",") != "A" {
		t.Fatalf("without --merged: nodes %v", got)
	}
	doc := runGraphDoc(t, []string{main, "--merged"}, "")
	if got := ids(doc); strings.Join(got, ",") != "A,B" {
		t.Fatalf("--merged: nodes %v", got)
	}
	// A fragment graphed from its importer's closure, its buffer from stdin.
	doc = runGraphDoc(t, []string{part, "--stdin", "--merged", "--root", main}, "partial workflow W {\n    step B2 { goto A }\n}\n")
	if got := ids(doc); strings.Join(got, ",") != "A,B2" {
		t.Fatalf("--root: nodes %v (diagnostics %v)", got, doc.Diagnostics)
	}
	// A root that does not import the file is a diagnostic, not a failure.
	other := write("other.mh", "workflow O { step S { log(1) } }\n")
	doc = runGraphDoc(t, []string{part, "--merged", "--root", other}, "")
	if !doc.OK || len(doc.Diagnostics) == 0 || !strings.Contains(doc.Diagnostics[0].Message, "does not import") {
		t.Fatalf("diagnostics = %v", doc.Diagnostics)
	}
}
