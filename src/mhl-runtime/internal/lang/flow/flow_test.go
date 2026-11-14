package flow

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/lang/parser"
)

func build(t *testing.T, src string) *Graph {
	t.Helper()
	prog, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	g, err := Build(prog, src)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	return g
}

func slice(src string, s Span) string { return src[s[0]:s[1]] }

// edgeList renders a workflow's edges as "from->to:kind[label]" for
// order-sensitive comparison.
func edgeList(w Workflow) []string {
	var out []string
	for _, e := range w.Edges {
		s := fmt.Sprintf("%s->%s:%s", e.From, e.To, e.Kind)
		if e.Label != "" {
			s += "[" + e.Label + "]"
		}
		out = append(out, s)
	}
	return out
}

func TestBuildEdges(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want []string
	}{
		{
			name: "fallthrough",
			src: `pipeline P {
    step A { log("a") }
    step B { log("b") }
}`,
			want: []string{"$start->A:next", "A->B:next", "B->$end:next"},
		},
		{
			name: "guarded goto keeps fallthrough",
			src: `workflow W {
    input ok: bool = false
    step A { if (!ok) goto A }
    step B { }
}`,
			want: []string{"$start->A:next", "A->A:goto[!ok]", "A->B:next", "B->$end:next"},
		},
		{
			name: "goto match with fail arm diverts",
			src: `workflow W {
    input kind: string = "a"
    step Pick {
        goto match kind {
            "a" -> A
            _ -> fail("unknown: " + kind)
        }
    }
    step A { complete() }
}`,
			want: []string{`$start->Pick:next`, `Pick->A:match["a"]`, `Pick->$fail:match[_]`, `A->$end:end[complete]`},
		},
		{
			name: "route expands per arm",
			src: `workflow W {
    input kind: string = "a"
    route Go(k: string) {
        "a" -> A
        _ -> B
    }
    step Pick { goto Go(kind) }
    step A { break "done" }
    step B { if (kind == "x") { fail("x") } else { pause("wait") } }
}`,
			want: []string{`$start->Pick:next`, `Pick->A:route["a"]`, `Pick->B:route[_]`, `A->$end:end[break]`,
				`B->$fail:fail[kind == "x"]`},
		},
		{
			name: "parallel group is one top-level node",
			src: `pipeline P {
    parallel G timeout 2m {
        step L { }
        step R { }
    }
    step After { }
}`,
			want: []string{"$start->G:next", "G->After:next", "After->$end:next"},
		},
		{
			name: "entry step starts a partial",
			src: `partial workflow W {
    step A { }
    entry step B { goto A }
}`,
			want: []string{"$start->B:next", "A->B:next", "B->A:goto"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := build(t, tc.src)
			if len(g.Workflows) != 1 {
				t.Fatalf("workflows = %d, want 1", len(g.Workflows))
			}
			got := edgeList(g.Workflows[0])
			if strings.Join(got, "\n") != strings.Join(tc.want, "\n") {
				t.Errorf("edges:\n got  %q\n want %q", got, tc.want)
			}
		})
	}
}

func TestBuildSpans(t *testing.T) {
	src := `// leading comment
agent Reviewer { engine: "cli/claude" }

workflow Review {
    description: "Reviews a thing — with ünicode"
    input topic: string = "x"
    var verdict = ""
    step Analyze timeout 5m {
        self.verdict = Reviewer.run(prompt: "look at ${topic} {not a brace}")
        if (verdict == "") goto Analyze
    }
    parallel Fan {
        step Left { }
        step Right { }
    }
}
`
	g := build(t, src)
	w := g.Workflows[0]
	checks := map[string][2]string{
		"workflow span":   {slice(src, w.Span)[:len("workflow Review {")], "workflow Review {"},
		"workflow header": {slice(src, w.HeaderSpan), "workflow Review"},
		"workflow name":   {slice(src, w.NameSpan), "Review"},
		"body open":       {src[w.BodyOpen : w.BodyOpen+1], "{"},
		"body close":      {src[w.BodyClose : w.BodyClose+1], "}"},
		"input span":      {slice(src, w.Inputs[0].Span), `input topic: string = "x"`},
		"input default":   {w.Inputs[0].Default, `"x"`},
		"var span":        {slice(src, w.Vars[0].Span), `var verdict = ""`},
		"description":     {w.Description, "Reviews a thing — with ünicode"},
		"prop span":       {slice(src, w.Props[0].Span), `description: "Reviews a thing — with ünicode"`},
	}
	step := w.Nodes[0]
	checks["step header"] = [2]string{slice(src, step.HeaderSpan), "step Analyze timeout 5m"}
	checks["step name"] = [2]string{slice(src, step.NameSpan), "Analyze"}
	checks["step timeout"] = [2]string{slice(src, *step.TimeoutSpan), "timeout 5m"}
	checks["step body close"] = [2]string{src[step.BodyClose:step.BodyClose+1] + src[step.BodyClose-5:step.BodyClose], "}" + "\n    "}
	checks["step calls"] = [2]string{strings.Join(step.Calls, ","), "Reviewer.run"}
	checks["step span end"] = [2]string{src[step.Span[1]-1 : step.Span[1]], "}"}
	var gotoEdge *Edge
	for i := range w.Edges {
		if w.Edges[i].Kind == "goto" {
			gotoEdge = &w.Edges[i]
		}
	}
	if gotoEdge == nil || gotoEdge.Span == nil {
		t.Fatal("no goto edge with a span")
	}
	checks["goto target"] = [2]string{slice(src, *gotoEdge.Span), "Analyze"}
	fan := w.Nodes[1]
	checks["parallel header"] = [2]string{slice(src, fan.HeaderSpan), "parallel Fan"}
	checks["parallel children"] = [2]string{strings.Join(fan.Children, ","), "Left,Right"}
	checks["branch parent"] = [2]string{w.Nodes[2].Parent, "Fan"}
	checks["decl span"] = [2]string{slice(src, g.Declarations[0].Span), `agent Reviewer { engine: "cli/claude" }`}
	for name, c := range checks {
		if c[0] != c[1] {
			t.Errorf("%s = %q, want %q", name, c[0], c[1])
		}
	}
}

// TestBuildSamples graphs every real .mh program under sample/ — none may
// fail, and every edge must point at a node (or a pseudo node) and every
// edge span must slice out the target's name.
func TestBuildSamples(t *testing.T) {
	root := filepath.Join("..", "..", "..", "..", "..", "sample")
	var files []string
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, ".mh") {
			files = append(files, p)
		}
		return nil
	})
	if len(files) == 0 {
		t.Skip("no samples found")
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		src := string(data)
		prog, err := parser.Parse(src)
		if err != nil {
			continue // a sample may be a deliberate parse-error fixture
		}
		g, err := Build(prog, src)
		if err != nil {
			t.Errorf("%s: %v", f, err)
			continue
		}
		for _, w := range g.Workflows {
			ids := map[string]bool{StartID: true, EndID: true, FailID: true}
			for _, x := range w.Externals {
				ids[x] = true
			}
			for _, n := range w.Nodes {
				ids[n.ID] = true
				if got := slice(src, n.NameSpan); got != n.Name {
					t.Errorf("%s: %s name span = %q", f, n.ID, got)
				}
			}
			for _, e := range w.Edges {
				if !ids[e.From] || !ids[e.To] {
					t.Errorf("%s: dangling edge %s -> %s", f, e.From, e.To)
				}
				if e.Span != nil && slice(src, *e.Span) != e.To {
					t.Errorf("%s: edge span %q != %q", f, slice(src, *e.Span), e.To)
				}
			}
		}
	}
}

func TestGotoTargetsSkipsFailArms(t *testing.T) {
	prog, err := parser.Parse(`workflow W {
    input k: string = "a"
    route R(x: string) { "a" -> A  _ -> fail("no") }
    step S { if (k == "z") { goto A } else { goto R(k) } }
    step A { }
}`)
	if err != nil {
		t.Fatal(err)
	}
	p := prog.Decls[0].Pipeline
	got := GotoTargets(p.Body[2].Step.Body, Routes(p))
	if strings.Join(got, ",") != "A,A" {
		t.Errorf("GotoTargets = %v, want [A A]", got)
	}
}

func TestBuildToolMethods(t *testing.T) {
	src := `tool Paths {
    const prefix = "projects/"
    root(id: string, n: number = 2): string -> prefix + id
    internal check(id) {
        return id
    }
    shaped(): { a: string } -> { a: "x" }
    arrow_block(xs: string[]) -> {
        return xs.size()
    }
}

workflow W {
    step A { log(Paths.root("a")) }
}
`
	g := build(t, src)
	var tool *Declaration
	for i := range g.Declarations {
		if g.Declarations[i].Name == "Paths" {
			tool = &g.Declarations[i]
		}
	}
	if tool == nil {
		t.Fatal("tool Paths not reported")
	}
	if got := slice(src, *tool.NameSpan); got != "Paths" {
		t.Errorf("nameSpan = %q", got)
	}
	if src[tool.BodyOpen] != '{' || src[tool.BodyClose] != '}' || tool.BodyClose != tool.Span[1]-1 {
		t.Errorf("tool body braces = %d..%d", tool.BodyOpen, tool.BodyClose)
	}
	if len(tool.Refs) != 1 || slice(src, tool.Refs[0]) != "Paths" || tool.Refs[0][0] < g.Workflows[0].Span[0] {
		t.Errorf("refs = %v", tool.Refs)
	}

	type want struct {
		header, body, kind, returns string
		internal                    bool
	}
	cases := map[string]want{
		"root":        {`root(id: string, n: number = 2): string`, `prefix + id`, "expr", "string", false},
		"check":       {`internal check(id)`, "{\n        return id\n    }", "block", "", true},
		"shaped":      {`shaped(): { a: string }`, `{ a: "x" }`, "expr", "{ a: string }", false},
		"arrow_block": {`arrow_block(xs: string[])`, "{\n        return xs.size()\n    }", "block", "", false},
	}
	if len(tool.Methods) != len(cases) {
		t.Fatalf("methods = %d, want %d", len(tool.Methods), len(cases))
	}
	for _, m := range tool.Methods {
		w := cases[m.Name]
		if got := slice(src, m.HeaderSpan); got != w.header {
			t.Errorf("%s header = %q, want %q", m.Name, got, w.header)
		}
		if got := slice(src, m.BodySpan); got != w.body || m.BodyKind != w.kind {
			t.Errorf("%s body = %s %q, want %s %q", m.Name, m.BodyKind, got, w.kind, w.body)
		}
		if m.Returns != w.returns || m.Internal != w.internal {
			t.Errorf("%s returns/internal = %q/%v", m.Name, m.Returns, m.Internal)
		}
		if got := slice(src, m.NameSpan); got != m.Name {
			t.Errorf("%s nameSpan = %q", m.Name, got)
		}
		if m.Span[0] != m.HeaderSpan[0] || m.Span[1] != m.BodySpan[1] {
			t.Errorf("%s span %v does not cover header %v + body %v", m.Name, m.Span, m.HeaderSpan, m.BodySpan)
		}
	}
	root := tool.Methods[0]
	if len(root.Args) != 2 || root.Args[1].Name != "n" || root.Args[1].Type != "number" || root.Args[1].Default != "2" ||
		slice(src, root.Args[1].Span) != "n: number = 2" {
		t.Errorf("root args = %+v", root.Args)
	}
}

func TestBuildCallSites(t *testing.T) {
	src := `agent Writer { command: "echo" }
tool T {
    m(x: string): string -> x
}
workflow W {
    step A {
        var a = T.m("x")
        var b = Writer.run(
            prompt: "hi ${a}",
            schema: {type: "object"}
        )
        log(json.stringify(b))
    }
}
`
	g := build(t, src)
	for _, d := range g.Declarations {
		if d.Name == "Brief" && (d.Kind != "schema" || d.Detail != "schemas/brief.schema.json") {
			t.Errorf("schema decl = %+v, want detail = its from path", d)
		}
	}
	sites := g.Workflows[0].Nodes[0].CallSites
	if len(sites) != 2 {
		t.Fatalf("call sites = %+v", sites)
	}
	if sites[0].Receiver != "T" || sites[0].Method != "m" || sites[0].Text != `T.m("x")` || slice(src, sites[0].Span) != `T.m("x")` {
		t.Errorf("site 0 = %+v", sites[0])
	}
	want := `Writer.run( prompt: "hi ${a}", schema: {type: "object"} )`
	if sites[1].Text != want || !strings.HasSuffix(slice(src, sites[1].Span), "\n        )") {
		t.Errorf("site 1 = %+v", sites[1])
	}
}

func TestBuildImportLocals(t *testing.T) {
	g := build(t, `import { Tools, Reviewer as R } from "./lib.mh"
import "./all.mh"
pipeline P { step A { log("a") } }`)
	if len(g.Imports) != 2 {
		t.Fatalf("imports = %d, want 2", len(g.Imports))
	}
	named, whole := g.Imports[0], g.Imports[1]
	if got := strings.Join(named.Names, ","); got != "Tools,Reviewer" {
		t.Errorf("names = %q, want source names", got)
	}
	if got := strings.Join(named.Locals, ","); got != "Tools,R" {
		t.Errorf("locals = %q, want the alias where one is given", got)
	}
	if whole.From != "./all.mh" || len(whole.Names) != 0 || whole.Locals == nil || len(whole.Locals) != 0 {
		t.Errorf("whole import = %+v, want empty (non-nil) names and locals", whole)
	}
}

func TestBuildCallArgsAndPromptDecl(t *testing.T) {
	src := `agent Ask { command: "echo" }
prompt Review(code: string, lang: string, words: number = 50) from "./review.prompt.md"
schema Brief from "schemas/brief.schema.json"
workflow W {
    input x: string = "a"
    step A {
        var r = Ask.run(prompt: Review(code: x, lang: "go"), schema: { a: x, b: [1, 2] })
        var s = Ask.run(x)
    }
}`
	g := build(t, src)
	var review *Declaration
	for i := range g.Declarations {
		if g.Declarations[i].Name == "Review" {
			review = &g.Declarations[i]
		}
	}
	if review == nil || review.Detail != "./review.prompt.md" || strings.Join(review.Params, ",") != "code,lang" {
		t.Fatalf("prompt decl = %+v, want detail = from path and params code,lang (not the defaulted words)", review)
	}
	sites := g.Workflows[0].Nodes[0].CallSites
	if len(sites) != 2 {
		t.Fatalf("call sites = %d, want 2", len(sites))
	}
	var got []string
	for _, a := range sites[0].Args {
		got = append(got, a.Name+"="+slice(src, a.Span))
	}
	if want := `prompt=Review(code: x, lang: "go")|schema={ a: x, b: [1, 2] }`; strings.Join(got, "|") != want {
		t.Errorf("args = %q, want %q", strings.Join(got, "|"), want)
	}
	if a := sites[1].Args; len(a) != 0 {
		t.Errorf("positional args = %+v, want none named", a)
	}
}

func TestBuildAgentShape(t *testing.T) {
	g := build(t, `agent A { command: "claude"
    args: ["-p", "x", "--json-schema", "${schema}"]
    stdin: "${prompt}" }
agent B { command: "copilot"
    stdin: "${prompt} follow ${schema}" }
agent C { command: "codex"
    args: ["exec", "-"]
    stdin: "${prompt}" }
agent D { command: env("CMD", "x")
    args: ["a", ...xs] }
pipeline P { step S { log("s") } }`)
	want := map[string][2]string{"A": {"claude", "args"}, "B": {"copilot", "stdin"}, "C": {"codex", "none"}, "D": {"", "none"}}
	for _, d := range g.Declarations {
		w, ok := want[d.Name]
		if !ok {
			continue
		}
		if d.Detail != w[0] || d.Schema != w[1] {
			t.Errorf("%s: detail=%q schema=%q, want %q %q", d.Name, d.Detail, d.Schema, w[0], w[1])
		}
	}
}
