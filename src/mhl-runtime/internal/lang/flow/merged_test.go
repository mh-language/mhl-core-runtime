package flow

import (
	"slices"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/lang/parser"
)

func fragment(t *testing.T, file, src string) Fragment {
	t.Helper()
	prog, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("parse %s: %v", file, err)
	}
	for _, d := range prog.Decls {
		if d.Pipeline != nil {
			return Fragment{File: file, Src: src, Prog: prog, Decl: d}
		}
	}
	t.Fatalf("%s declares no workflow", file)
	return Fragment{}
}

// A fragment graphed alone and without the `entry` step is not where a run
// starts, so it gets no start edge.
func TestPartialFragmentWithoutEntryHasNoStartEdge(t *testing.T) {
	g := build(t, `partial workflow W {
    step B { log("b") }
    step C { goto A }
}`)
	got := edgeList(g.Workflows[0])
	if slices.Contains(got, "$start->B:next") {
		t.Fatalf("unexpected start edge: %v", got)
	}
	if want := []string{"B->C:next", "C->A:goto"}; !slices.Equal(got, want) {
		t.Fatalf("edges = %v, want %v", got, want)
	}

	entry := build(t, `partial workflow W {
    entry step A { goto C }
}`)
	if got := edgeList(entry.Workflows[0]); !slices.Contains(got, "$start->A:next") {
		t.Fatalf("the entry fragment keeps its start edge: %v", got)
	}
}

func TestBuildMergedJoinsFragmentsInRunOrder(t *testing.T) {
	home := `import "b.mh"
partial workflow W {
    description: "whole"
    input x: string = "1"
    entry step A { log(x) }
}`
	other := `partial workflow W {
    var y = 0
    step B { if (x == "") goto A }
    step C { complete() }
}`
	w, err := BuildMerged([]Fragment{fragment(t, "", home), fragment(t, "/abs/b.mh", other)})
	if err != nil {
		t.Fatal(err)
	}
	if !w.Merged || w.Name != "W" || w.Description != "whole" {
		t.Fatalf("header = %+v", w)
	}
	// A falls through into the next fragment's first step, as at run time.
	want := []string{"$start->A:next", "A->B:next", "B->A:goto[x == \"\"]", "B->C:next", "C->$end:end[complete]"}
	if got := edgeList(w); !slices.Equal(got, want) {
		t.Fatalf("edges = %v, want %v", got, want)
	}
	if len(w.Externals) != 0 {
		t.Fatalf("externals = %v, want none", w.Externals)
	}
	files := map[string]string{}
	for _, n := range w.Nodes {
		files[n.ID] = n.File
	}
	if files["A"] != "" || files["B"] != "/abs/b.mh" || files["C"] != "/abs/b.mh" {
		t.Fatalf("node files = %v", files)
	}
	if w.Inputs[0].File != "" || w.Vars[0].File != "/abs/b.mh" {
		t.Fatalf("member files: input %q var %q", w.Inputs[0].File, w.Vars[0].File)
	}
	// Each node's spans index its own fragment's source.
	for _, n := range w.Nodes {
		src := home
		if n.File != "" {
			src = other
		}
		if got := slice(src, n.NameSpan); got != n.Name {
			t.Errorf("%s: name span reads %q", n.ID, got)
		}
	}
	// The workflow's own spans are the home fragment's, even when it is not first.
	w2, err := BuildMerged([]Fragment{fragment(t, "/abs/a.mh", home), fragment(t, "", other)})
	if err != nil {
		t.Fatal(err)
	}
	if got := slice(other, w2.NameSpan); got != "W" || w2.BodyOpen <= w2.NameSpan[0] {
		t.Fatalf("home header span reads %q", got)
	}
}
