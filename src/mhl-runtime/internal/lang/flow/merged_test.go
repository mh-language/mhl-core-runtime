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

func TestGateDecisionsOnTheNode(t *testing.T) {
	g := build(t, `type Review = { ok: bool }
workflow W {
    step Ask {
        var d = pause("review", decision: Review)
        if (!d.ok) { var again = pause("sure?", decision: bool) }
    }
    step Plain { pause("x") }
}`)
	nodes := g.Workflows[0].Nodes
	if got := nodes[0].Decisions; !slices.Equal(got, []string{"Review", "bool"}) {
		t.Fatalf("Ask decisions = %v", got)
	}
	if nodes[0].FallsThrough != true || slices.Contains(nodes[0].Exits, "pause") {
		t.Fatalf("a gate returns its answer, it is not an exit: %+v", nodes[0])
	}
	if nodes[1].Decisions != nil {
		t.Fatalf("a plain pause has no decision: %v", nodes[1].Decisions)
	}
}

func TestStagesOfEveryFragment(t *testing.T) {
	home := `import "spec.mh"
partial workflow W {
    input n: number = 0
    entry step Intake { goto Spec }
}`
	spec := `partial workflow W stage Spec {
    input depth: number
    step A { log("a") }
    parallel P { step P1 { log(1) } step P2 { log(2) } }
    step B { if (n < 1) { goto Spec } }
}`
	w, err := BuildMerged([]Fragment{fragment(t, "", home), fragment(t, "/abs/spec.mh", spec)})
	if err != nil {
		t.Fatal(err)
	}
	if len(w.Stages) != 1 || w.Stages[0].Name != "Spec" || w.Stages[0].File != "/abs/spec.mh" || !slices.Equal(w.Stages[0].Members, []string{"A", "P", "B"}) {
		t.Fatalf("stages = %+v", w.Stages)
	}
	if got := slice(spec, w.Stages[0].NameSpan); got != "Spec" {
		t.Fatalf("stage name span reads %q", got)
	}
	stage := map[string]string{}
	for _, n := range w.Nodes {
		stage[n.ID] = n.Stage
	}
	if stage["Intake"] != "" || stage["A"] != "Spec" || stage["P"] != "Spec" || stage["P1"] != "" || stage["B"] != "Spec" {
		t.Fatalf("node stages = %v", stage)
	}
	// An input declared in the stage's fragment belongs to the stage.
	inputStage := map[string]string{}
	for _, in := range w.Inputs {
		inputStage[in.Name] = in.Stage
	}
	if inputStage["n"] != "" || inputStage["depth"] != "Spec" {
		t.Fatalf("input stages = %v", inputStage)
	}
	// `goto Spec` targets the stage, which is not an external.
	if got := edgeList(w); !slices.Contains(got, "Intake->Spec:goto") || !slices.Contains(got, "B->Spec:goto[n < 1]") || len(w.Externals) != 0 {
		t.Fatalf("edges %v, externals %v", got, w.Externals)
	}
}

func TestHeaderStageCoversTheWholeFragment(t *testing.T) {
	src := `partial workflow Story stage Story {
    step A { log(1) }
    parallel P { step P1 { log(1) } step P2 { log(2) } }
}`
	w := build(t, src).Workflows[0]
	if len(w.Stages) != 1 {
		t.Fatalf("stages = %+v", w.Stages)
	}
	st := w.Stages[0]
	// The stage shares the workflow's name here: its NameSpan is the one after `stage`.
	if !slices.Equal(st.Members, []string{"A", "P"}) || st.NameSpan[0] < w.NameSpan[1] || slice(src, st.NameSpan) != "Story" {
		t.Fatalf("header stage = %+v (workflow name at %v)", st, w.NameSpan)
	}
	if st.BodyOpen != w.BodyOpen || st.BodyClose != w.BodyClose {
		t.Fatalf("a header stage's body is the fragment's: %+v vs %d..%d", st, w.BodyOpen, w.BodyClose)
	}
	for _, n := range w.Nodes {
		if n.Parent == "" && n.Stage != "Story" {
			t.Fatalf("%s is not in the header stage", n.ID)
		}
	}
}
