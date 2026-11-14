// Package flow projects a parsed .mh program onto the control-flow graph a
// visual editor draws: one node per step / parallel group, one edge per
// fall-through, `goto`, `goto match` arm, `route` arm and terminal
// (`break`, `complete()`, `fail(...)`). Every node and edge carries the byte
// span of the source it came from, so an editor can apply a structural edit
// (rename, delete, reconnect) as a precise text edit instead of
// re-printing the program — the parser drops comments, so re-printing
// would lose them. `mhl graph` serves this as JSON for any editor or visual tool.
package flow

import (
	"sort"
	"strings"

	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
	"github.com/mh-language/mhl-core-runtime/internal/lang/parser"
)

// Version is the JSON shape version of a Graph.
const Version = 1

// Pseudo node ids for the implicit start and terminal states.
const (
	StartID = "$start"
	EndID   = "$end"
	FailID  = "$fail"
)

// Span is a half-open byte range [start, end) into the source text.
type Span [2]int

// Graph is the projection of one source file.
type Graph struct {
	Version      int           `json:"version"`
	OK           bool          `json:"ok"`
	File         string        `json:"file"`
	Diagnostics  []Diagnostic  `json:"diagnostics"`
	Imports      []Import      `json:"imports"`
	Declarations []Declaration `json:"declarations"`
	Workflows    []Workflow    `json:"workflows"`
}

// Diagnostic is a parse or lint problem. File is empty for the source itself.
type Diagnostic struct {
	File    string `json:"file,omitempty"`
	Line    int    `json:"line"`
	Column  int    `json:"column"`
	Message string `json:"message"`
}

// Import is one `import` declaration.
type Import struct {
	From  string   `json:"from"`
	Names []string `json:"names"`
	// Locals is, parallel to Names, the name each item is bound to in this
	// file — its `as` alias when it has one — which is what call sites use.
	Locals []string `json:"locals"`
	Span   Span     `json:"span"`
}

// Declaration is a top-level agent/tool/memory/... a workflow can reference.
type Declaration struct {
	Kind     string   `json:"kind"`
	Name     string   `json:"name"`
	Span     Span     `json:"span"`
	Exported bool     `json:"exported,omitempty"`
	Detail   string   `json:"detail,omitempty"` // extension kind (mcp, a2a, ...); a prompt's or schema's `from` path; an agent's literal `command`
	Inputs   []string `json:"inputs,omitempty"` // agent per-call inputs
	Params   []string `json:"params,omitempty"` // prompt parameters a call must pass (no default)
	// Schema says where an agent puts a call's `schema:` — "args" (a
	// literal "${schema}" element), "stdin" (inside a literal `stdin:`) or
	// "none" (nowhere: the argument would be ignored); empty when `args`/
	// `stdin` are computed and it cannot be told statically.
	Schema  string   `json:"schema,omitempty"`
	Methods []Method `json:"methods,omitempty"` // tool methods, `internal` ones included
	// The rest is set for a tool only, so an editor can rewrite it in place:
	// its name, the braces around its members, and every `Name.` call site
	// in this file (a rename rewrites them too).
	NameSpan  *Span  `json:"nameSpan,omitempty"`
	BodyOpen  int    `json:"bodyOpen,omitempty"`
	BodyClose int    `json:"bodyClose,omitempty"`
	Refs      []Span `json:"refs,omitempty"`
}

// Method is one tool method: its signature, and the spans an editor needs
// to rewrite the signature (HeaderSpan, from `internal`/the name through the
// return type) or the body (BodySpan: the expression after `->`, or the
// block from `{` to `}` inclusive) without touching the other.
type Method struct {
	Name       string   `json:"name"`
	Params     []string `json:"params"` // parameter names, in order
	Args       []Arg    `json:"args"`   // the same parameters, in full
	Returns    string   `json:"returns,omitempty"`
	Internal   bool     `json:"internal,omitempty"`
	Span       Span     `json:"span"`
	HeaderSpan Span     `json:"headerSpan"`
	NameSpan   Span     `json:"nameSpan"`
	BodyKind   string   `json:"bodyKind"` // "expr" | "block"
	BodySpan   Span     `json:"bodySpan"`
}

// Arg is one tool method parameter, `name: Type = default`.
type Arg struct {
	Name    string `json:"name"`
	Type    string `json:"type,omitempty"`
	Default string `json:"default,omitempty"`
	Span    Span   `json:"span"`
}

// Workflow is one `pipeline` / `workflow` declaration.
type Workflow struct {
	Name        string  `json:"name"`
	Kind        string  `json:"kind"`
	Loop        bool    `json:"loop,omitempty"`
	Partial     bool    `json:"partial,omitempty"`
	Internal    bool    `json:"internal,omitempty"`
	Max         string  `json:"max,omitempty"`
	Description string  `json:"description,omitempty"`
	Param       *Param  `json:"param,omitempty"`
	Returns     string  `json:"returns,omitempty"`
	Span        Span    `json:"span"`
	HeaderSpan  Span    `json:"headerSpan"`
	NameSpan    Span    `json:"nameSpan"`
	BodyOpen    int     `json:"bodyOpen"`
	BodyClose   int     `json:"bodyClose"`
	Inputs      []Input `json:"inputs"`
	Vars        []Var   `json:"vars"`
	Props       []Prop  `json:"props"`
	Routes      []Route `json:"routes"`
	Nodes       []Node  `json:"nodes"`
	Edges       []Edge  `json:"edges"`
	// Externals names edge targets not declared in this file — a step of
	// another fragment of a `partial` workflow. An editor draws them as
	// placeholders.
	Externals []string `json:"externals"`
}

// Input is a workflow `input name: Type [= default]` (or the typed param).
type Input struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	Default string `json:"default,omitempty"`
	Span    Span   `json:"span"`
}

// Param is a typed signature's single `name: Type` parameter.
type Param struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// Var is a workflow-scoped `var` / `const` / `mem`.
type Var struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
	Span Span   `json:"span"`
}

// Prop is a body property (`description:`, `output:`, a hook, ...).
type Prop struct {
	Name string `json:"name"`
	Span Span   `json:"span"`
}

// Route is a `route Name(params) { arms }` dispatch table.
type Route struct {
	Name   string   `json:"name"`
	Params []string `json:"params"`
	Span   Span     `json:"span"`
	Arms   []Arm    `json:"arms"`
}

// Arm is one `pattern -> Target` / `pattern -> fail(...)` arm.
type Arm struct {
	Pattern    string `json:"pattern"`
	Target     string `json:"target,omitempty"`
	Fail       bool   `json:"fail,omitempty"`
	Span       Span   `json:"span"`
	TargetSpan *Span  `json:"targetSpan,omitempty"`
}

// Node is a step or a parallel group. Steps inside a group are nodes too,
// with Parent naming the group.
type Node struct {
	ID          string   `json:"id"`
	Kind        string   `json:"kind"` // "step" | "parallel"
	Name        string   `json:"name"`
	Parent      string   `json:"parent,omitempty"`
	Children    []string `json:"children,omitempty"`
	Entry       bool     `json:"entry,omitempty"`
	Timeout     string   `json:"timeout,omitempty"`
	Span        Span     `json:"span"`
	HeaderSpan  Span     `json:"headerSpan"`
	NameSpan    Span     `json:"nameSpan"`
	TimeoutSpan *Span    `json:"timeoutSpan,omitempty"`
	BodyOpen    int      `json:"bodyOpen"`
	BodyClose   int      `json:"bodyClose"`
	Calls       []string `json:"calls"`
	// CallSites are the same calls, one per occurrence in source order, each
	// with its text and span — an editor draws one action box per call.
	CallSites    []CallSite `json:"callSites"`
	Exits        []string   `json:"exits"`
	FallsThrough bool       `json:"fallsThrough"`
}

// CallSite is one `Receiver.method(...)` call in a step body whose receiver
// is a declared or imported name.
type CallSite struct {
	Receiver string `json:"receiver"`
	Method   string `json:"method"`
	// Text is the call's source, whitespace runs collapsed to one space.
	Text string `json:"text"`
	// Span runs from the receiver through the closing parenthesis.
	Span Span `json:"span"`
	// Args are the call's named arguments (`name: value`), in order, each
	// with its value's span — so an editor can replace one value in place.
	Args []CallArg `json:"args,omitempty"`
}

// CallArg is one named argument of a call site.
type CallArg struct {
	Name string `json:"name"`
	// Span covers the value expression, from its first to its last token.
	Span Span `json:"span"`
}

// Edge is one possible transfer of control.
type Edge struct {
	From  string `json:"from"`
	To    string `json:"to"`
	Kind  string `json:"kind"` // next | goto | match | route | end | fail
	Label string `json:"label,omitempty"`
	Via   string `json:"via,omitempty"` // route name, for kind "route"
	// Span is the target name's span in the source (rename rewrites it);
	// nil when the edge is implicit (fall-through, start, terminal).
	Span *Span `json:"span,omitempty"`
}

// Build projects prog — parsed from src — onto a Graph. OK, File and
// Diagnostics are left for the caller (which owns parsing and linting).
func Build(prog *ast.Program, src string) (*Graph, error) {
	toks, err := parser.Tokens(src)
	if err != nil {
		return nil, err
	}
	b := &builder{src: src, toks: toks, callables: map[string]bool{}}
	g := &Graph{Version: Version, Imports: []Import{}, Declarations: []Declaration{}, Workflows: []Workflow{}}
	for _, d := range prog.Decls {
		if d.Import != nil {
			imp := Import{From: d.Import.Path, Names: d.Import.Names(), Locals: []string{}, Span: declSpan(d)}
			for _, it := range d.Import.Items {
				name := it.Name
				if it.Alias != "" {
					name = it.Alias
				}
				imp.Locals = append(imp.Locals, name)
				b.callables[name] = true
			}
			g.Imports = append(g.Imports, imp)
			continue
		}
		if decl, ok := b.declarationOf(d); ok {
			b.callables[decl.Name] = true
			g.Declarations = append(g.Declarations, decl)
		}
	}
	for i := range g.Declarations {
		if d := &g.Declarations[i]; d.Kind == "tool" {
			d.Refs = b.refs(d.Name, *d.NameSpan)
		}
	}
	for _, d := range prog.Decls {
		if d.Pipeline != nil {
			g.Workflows = append(g.Workflows, b.workflow(d))
		}
	}
	return g, nil
}

func declSpan(d *ast.Declaration) Span { return Span{d.Pos.Offset, d.EndPos.Offset} }

func (b *builder) declarationOf(d *ast.Declaration) (Declaration, bool) {
	decl := Declaration{Span: declSpan(d), Exported: d.Export}
	switch {
	case d.Agent != nil:
		decl.Kind, decl.Name, decl.Inputs = "agent", d.Agent.Name, d.Agent.InputNames()
		decl.Detail, decl.Schema = agentShape(d.Agent)
	case d.Tool != nil:
		decl.Kind, decl.Name = "tool", d.Tool.Name
		b.tool(&decl, d.Tool)
	case d.Router != nil:
		decl.Kind, decl.Name = "router", d.Router.Name
	case d.Memory != nil:
		decl.Kind, decl.Name = "memory", d.Memory.Name
	case d.Extension != nil:
		decl.Kind, decl.Name, decl.Detail = "extension", d.Extension.Name, d.Extension.Kind
	case d.Prompt != nil:
		decl.Kind, decl.Name, decl.Detail = "prompt", d.Prompt.Name, d.Prompt.Source
		for _, p := range d.Prompt.Params {
			if p.Default == nil {
				decl.Params = append(decl.Params, p.Name)
			}
		}
	case d.Skill != nil:
		decl.Kind, decl.Name = "skill", d.Skill.Name
	case d.Schema != nil:
		decl.Kind, decl.Name, decl.Detail = "schema", d.Schema.Name, d.Schema.Source
	case d.Type != nil:
		decl.Kind, decl.Name = "type", d.Type.Name
	case d.Enum != nil:
		decl.Kind, decl.Name = "enum", d.Enum.Name
	case d.Alias != nil:
		decl.Kind, decl.Name, decl.Detail = "alias", d.Alias.Name, d.Alias.Target
	case d.Test != nil:
		decl.Kind, decl.Name = "test", d.Test.Name
	default:
		return decl, false
	}
	return decl, true
}

// tool fills a tool declaration's spans and methods.
func (b *builder) tool(decl *Declaration, t *ast.Tool) {
	ts := b.in(decl.Span)
	for i, tok := range ts {
		if tok.Kind == "Ident" && tok.Value == t.Name && decl.NameSpan == nil {
			s := tokSpan(tok)
			decl.NameSpan = &s
		}
		if tok.Kind == "Punct" && tok.Value == "{" {
			decl.BodyOpen = ts[i].Offset
			break
		}
	}
	decl.BodyClose = b.closeOf(decl.Span)
	decl.Methods = []Method{}
	for _, m := range t.Methods {
		decl.Methods = append(decl.Methods, b.method(m))
	}
}

func (b *builder) method(m *ast.ToolMethod) Method {
	s := Span{m.Pos.Offset, m.EndPos.Offset}
	out := Method{Name: m.Name, Internal: m.Internal, Returns: m.Returns.String(), Span: s,
		Params: []string{}, Args: []Arg{}}
	for _, p := range m.Params {
		ps := Span{p.Pos.Offset, p.EndPos.Offset}
		a := Arg{Name: p.Name, Type: p.Type.String(), Span: ps}
		if p.Default != nil {
			a.Default = b.text(Span{p.Default.Pos.Offset, ps[1]})
		}
		out.Params = append(out.Params, p.Name)
		out.Args = append(out.Args, a)
	}
	ts := b.in(s)
	depth := 0
	for i, t := range ts {
		if t.Kind == "Ident" && t.Value == m.Name && out.NameSpan == (Span{}) {
			out.NameSpan = tokSpan(t)
		}
		if t.Kind != "Punct" {
			continue
		}
		switch t.Value {
		case "(", "[":
			depth++
		case ")", "]":
			depth--
		}
		if depth != 0 || i == 0 {
			continue
		}
		// The header ends before the first depth-0 `->`, or before the `{`
		// of an arrow-less block (a `{` right after `:` is an object-shape
		// return type, not the body).
		if t.Value == "->" || (t.Value == "{" && ts[i-1].Value != ":") {
			out.HeaderSpan = Span{s[0], ts[i-1].End()}
			if m.Body != nil {
				out.BodyKind, out.BodySpan = "expr", Span{m.Body.Pos.Offset, s[1]}
			} else {
				open := t.Offset
				if t.Value == "->" && i+1 < len(ts) {
					open = ts[i+1].Offset
				}
				out.BodyKind, out.BodySpan = "block", Span{open, s[1]}
			}
			break
		}
	}
	return out
}

// refs lists the `name.` call sites of a declaration in the whole file,
// skipping its own name token.
func (b *builder) refs(name string, self Span) []Span {
	out := []Span{}
	for i, t := range b.toks {
		if t.Kind == "Ident" && t.Value == name && t.Offset != self[0] &&
			i+1 < len(b.toks) && b.toks[i+1].Kind == "Punct" && b.toks[i+1].Value == "." &&
			(i == 0 || b.toks[i-1].Value != ".") {
			out = append(out, tokSpan(t))
		}
	}
	return out
}

type builder struct {
	src       string
	toks      []parser.Token
	callables map[string]bool
}

// at returns the index of the first token starting at or after offset.
func (b *builder) at(offset int) int {
	return sort.Search(len(b.toks), func(i int) bool { return b.toks[i].Offset >= offset })
}

// in returns the tokens of span.
func (b *builder) in(s Span) []parser.Token {
	return b.toks[b.at(s[0]):b.at(s[1])]
}

func (b *builder) text(s Span) string { return strings.TrimSpace(b.src[s[0]:s[1]]) }

func tokSpan(t parser.Token) Span { return Span{t.Offset, t.End()} }

// bodyBrace finds the `{` opening the body of a header starting at tokens
// ts: the first depth-0 `{` that is not an object-shape type (`: { ... }`).
func bodyBrace(ts []parser.Token) int {
	depth := 0
	for i, t := range ts {
		if t.Kind != "Punct" {
			continue
		}
		switch t.Value {
		case "(", "[":
			depth++
		case ")", "]", "}":
			depth--
		case "{":
			if depth == 0 && (i == 0 || ts[i-1].Value != ":") {
				return i
			}
			depth++
		}
	}
	return -1
}

// closeOf returns the last token of span — the closing brace of a block.
func (b *builder) closeOf(s Span) int {
	ts := b.in(s)
	if len(ts) == 0 {
		return s[1]
	}
	return ts[len(ts)-1].Offset
}

func (b *builder) workflow(d *ast.Declaration) Workflow {
	p := d.Pipeline
	w := Workflow{
		Name: p.Name, Kind: p.Kind, Loop: p.Loop, Partial: p.Partial, Internal: p.Internal, Max: p.Max,
		Span:   declSpan(d),
		Inputs: []Input{}, Vars: []Var{}, Props: []Prop{}, Routes: []Route{}, Nodes: []Node{}, Edges: []Edge{},
	}
	whole := Span{p.Pos.Offset, p.EndPos.Offset}
	ts := b.in(whole)
	if i := bodyBrace(ts); i >= 0 {
		w.BodyOpen = ts[i].Offset
		w.HeaderSpan = Span{p.Pos.Offset, ts[i-1].End()}
	}
	w.BodyClose = b.closeOf(whole)
	for _, t := range ts {
		if t.Kind == "Ident" && t.Value == p.Name {
			w.NameSpan = tokSpan(t)
			break
		}
	}
	if p.Param != nil {
		w.Param = &Param{Name: p.Param.Name, Type: p.Param.Type.String()}
	}
	if p.Returns != nil {
		w.Returns = p.Returns.String()
	}

	var top []*Node
	for _, m := range p.Body {
		ms := Span{m.Pos.Offset, m.EndPos.Offset}
		switch {
		case m.Input != nil:
			in := Input{Name: m.Input.Name, Type: m.Input.Type.String(), Span: ms}
			if m.Input.Default != nil {
				for i, t := range b.in(ms) {
					if t.Value == "=" && t.Kind == "Punct" {
						in.Default = b.text(Span{b.in(ms)[i].End(), ms[1]})
						break
					}
				}
			}
			w.Inputs = append(w.Inputs, in)
		case m.Const != nil:
			w.Vars = append(w.Vars, Var{Name: m.Const.Name, Kind: "const", Span: ms})
		case m.Var != nil:
			w.Vars = append(w.Vars, Var{Name: m.Var.Name, Kind: "var", Span: ms})
		case m.Mem != nil:
			w.Vars = append(w.Vars, Var{Name: m.Mem.Name, Kind: "mem", Span: ms})
		case m.Prop != nil:
			w.Props = append(w.Props, Prop{Name: m.Prop.Name, Span: ms})
			if m.Prop.Name == "description" {
				w.Description, _ = ast.StringValue(m.Prop.Value)
			}
		case m.Route != nil:
			w.Routes = append(w.Routes, b.route(m.Route, ms))
		case m.Step != nil:
			n := b.step(m.Step, ms, "")
			w.Nodes = append(w.Nodes, n)
			top = append(top, &w.Nodes[len(w.Nodes)-1])
		case m.Parallel != nil:
			group := b.parallel(m.Parallel, ms)
			w.Nodes = append(w.Nodes, group)
			idx := len(w.Nodes) - 1
			for _, s := range m.Parallel.Steps {
				w.Nodes = append(w.Nodes, b.step(s, Span{s.Pos.Offset, s.EndPos.Offset}, m.Parallel.Name))
			}
			top = append(top, &w.Nodes[idx])
		}
	}
	// top holds pointers into w.Nodes taken before later appends could
	// reallocate it; rebuild them from ids now that the slice is final.
	byID := map[string]*Node{}
	for i := range w.Nodes {
		byID[w.Nodes[i].ID] = &w.Nodes[i]
	}
	for i, n := range top {
		top[i] = byID[n.ID]
	}
	w.Edges = b.edges(p, top)
	w.Externals = []string{}
	for _, e := range w.Edges {
		if byID[e.To] == nil && e.To != EndID && e.To != FailID && !contains(w.Externals, e.To) {
			w.Externals = append(w.Externals, e.To)
		}
	}
	return w
}

func (b *builder) route(r *ast.WorkflowRoute, s Span) Route {
	out := Route{Name: r.Name, Span: s, Params: []string{}, Arms: []Arm{}}
	for _, p := range r.Params {
		out.Params = append(out.Params, p.Name)
	}
	for _, a := range r.Arms {
		out.Arms = append(out.Arms, b.arm(a))
	}
	return out
}

func (b *builder) arm(a *ast.GotoMatchArm) Arm {
	s := Span{a.Pos.Offset, a.EndPos.Offset}
	out := Arm{Span: s, Target: a.Target, Fail: a.Fail != nil}
	ts := b.in(s)
	for i, t := range ts {
		if t.Kind == "Punct" && t.Value == "->" {
			out.Pattern = b.text(Span{s[0], t.Offset})
			if !out.Fail && i+1 < len(ts) {
				ts := tokSpan(ts[i+1])
				out.TargetSpan = &ts
			}
			break
		}
	}
	return out
}

func (b *builder) step(st *ast.Step, s Span, parent string) Node {
	n := Node{ID: st.Name, Kind: "step", Name: st.Name, Parent: parent, Entry: st.Entry, Timeout: st.Timeout, Span: s,
		FallsThrough: !AlwaysDiverts(st.Body)}
	ts := b.in(s)
	for i, t := range ts {
		switch {
		case t.Kind == "Ident" && t.Value == "step" && i+1 < len(ts) && n.NameSpan == (Span{}):
			n.NameSpan = tokSpan(ts[i+1])
		case t.Kind == "Ident" && t.Value == "timeout" && i+1 < len(ts) && n.TimeoutSpan == nil:
			ts := Span{t.Offset, ts[i+1].End()}
			n.TimeoutSpan = &ts
		}
		if t.Kind == "Punct" && t.Value == "{" {
			n.BodyOpen = t.Offset
			n.HeaderSpan = Span{s[0], ts[i-1].End()}
			break
		}
	}
	n.BodyClose = b.closeOf(s)
	n.Calls = b.calls(Span{n.BodyOpen, n.BodyClose})
	n.CallSites = b.callSites(Span{n.BodyOpen, n.BodyClose})
	n.Exits = exits(st.Body)
	return n
}

func (b *builder) parallel(pg *ast.ParallelGroup, s Span) Node {
	n := Node{ID: pg.Name, Kind: "parallel", Name: pg.Name, Timeout: pg.Timeout, Span: s, FallsThrough: true,
		Calls: []string{}, CallSites: []CallSite{}, Exits: []string{}}
	ts := b.in(s)
	for i, t := range ts {
		switch {
		case t.Kind == "Ident" && t.Value == "parallel" && i+1 < len(ts) && n.NameSpan == (Span{}):
			n.NameSpan = tokSpan(ts[i+1])
		case t.Kind == "Ident" && t.Value == "timeout" && i+1 < len(ts) && n.TimeoutSpan == nil:
			ts := Span{t.Offset, ts[i+1].End()}
			n.TimeoutSpan = &ts
		}
		if t.Kind == "Punct" && t.Value == "{" {
			n.BodyOpen = t.Offset
			n.HeaderSpan = Span{s[0], ts[i-1].End()}
			break
		}
	}
	n.BodyClose = b.closeOf(s)
	for _, st := range pg.Steps {
		n.Children = append(n.Children, st.Name)
	}
	return n
}

// calls lists `X.method` call sites in span whose receiver X is a declared
// (or imported) agent, tool, router, extension, ... in first-seen order.
func (b *builder) calls(s Span) []string {
	out := []string{}
	seen := map[string]bool{}
	ts := b.in(s)
	for i := 0; i+3 < len(ts); i++ {
		if ts[i].Kind == "Ident" && b.callables[ts[i].Value] && ts[i+1].Value == "." && ts[i+2].Kind == "Ident" && ts[i+3].Value == "(" &&
			(i == 0 || ts[i-1].Value != ".") {
			c := ts[i].Value + "." + ts[i+2].Value
			if !seen[c] {
				seen[c] = true
				out = append(out, c)
			}
		}
	}
	return out
}

// callSites lists every `X.method(...)` call in span whose receiver X is a
// declared or imported name, in source order, with its full extent.
func (b *builder) callSites(s Span) []CallSite {
	out := []CallSite{}
	ts := b.in(s)
	for i := 0; i+3 < len(ts); i++ {
		if !(ts[i].Kind == "Ident" && b.callables[ts[i].Value] && ts[i+1].Value == "." && ts[i+2].Kind == "Ident" && ts[i+3].Value == "(" &&
			(i == 0 || ts[i-1].Value != ".")) {
			continue
		}
		depth, end := 0, -1
		var args []CallArg
		// A named argument is `Ident :` right after the call's `(` or a
		// top-level `,` (so a ternary's `:` never counts); its value runs to
		// the next top-level `,` or the closing `)`.
		arg := -1
		closeArg := func(last int) {
			if arg >= 0 && last >= 0 {
				args[arg].Span = Span{args[arg].Span[0], ts[last].End()}
			}
			arg = -1
		}
		for j := i + 3; j < len(ts) && end < 0; j++ {
			if depth == 1 && ts[j-1].Kind == "Punct" && (ts[j-1].Value == "(" && j-1 == i+3 || ts[j-1].Value == ",") &&
				ts[j].Kind == "Ident" && j+2 < len(ts) && ts[j+1].Value == ":" {
				args = append(args, CallArg{Name: ts[j].Value, Span: Span{ts[j+2].Offset, ts[j+2].End()}})
				arg = len(args) - 1
				j++ // skip the ':'
				continue
			}
			if ts[j].Kind != "Punct" {
				continue
			}
			switch ts[j].Value {
			case "(", "[", "{":
				depth++
			case ")", "]", "}":
				depth--
				if depth == 0 {
					closeArg(j - 1)
					end = ts[j].End()
				}
			case ",":
				if depth == 1 {
					closeArg(j - 1)
				}
			}
		}
		if end < 0 {
			continue
		}
		sp := Span{ts[i].Offset, end}
		out = append(out, CallSite{Receiver: ts[i].Value, Method: ts[i+2].Value, Text: strings.Join(strings.Fields(b.src[sp[0]:sp[1]]), " "), Span: sp, Args: args})
	}
	return out
}

// exits lists the terminal statements a step body can execute — `pause`,
// `fail`, `complete`, `break` — anywhere in it, deduplicated, source order.
func exits(stmts []*ast.Statement) []string {
	out := []string{}
	seen := map[string]bool{}
	walkTerminals(stmts, nil, false, func(kind string, _ *ast.Statement, _ bool) {
		if !seen[kind] {
			seen[kind] = true
			out = append(out, kind)
		}
	})
	return out
}

// walkTerminals visits each `break` statement and bare `pause(...)` /
// `fail(...)` / `complete()` call statement, with its innermost guarding `if`.
func walkTerminals(stmts []*ast.Statement, guard *ast.Statement, inElse bool, visit func(kind string, guard *ast.Statement, inElse bool)) {
	for _, s := range stmts {
		if s == nil {
			continue
		}
		switch {
		case s.Break != nil:
			visit("break", guard, inElse)
		case s.Expr != nil && IsDivertingCall(s.Expr.Expr):
			visit(ast.BarePostfix(s.Expr.Expr).Primary.Ident, guard, inElse)
		case s.If != nil:
			walkTerminals(s.If.Then, s, false, visit)
			walkTerminals(s.If.Else, s, true, visit)
		case s.While != nil:
			walkTerminals(s.While.Body, guard, inElse, visit)
		case s.ForIn != nil:
			walkTerminals(s.ForIn.Body, guard, inElse, visit)
		case s.Try != nil:
			walkTerminals(s.Try.Body, guard, inElse, visit)
			walkTerminals(s.Try.Catch, guard, inElse, visit)
			walkTerminals(s.Try.Finally, guard, inElse, visit)
		}
	}
}

// guardLabel renders the condition of an `if` statement ("else" for its
// else branch), "" when there is no guard.
func (b *builder) guardLabel(guard *ast.Statement, inElse bool) string {
	if guard == nil {
		return ""
	}
	if inElse {
		return "else"
	}
	ts := b.in(Span{guard.Pos.Offset, guard.EndPos.Offset})
	depth := 0
	for i, t := range ts {
		if t.Kind != "Punct" {
			continue
		}
		switch t.Value {
		case "(":
			depth++
		case ")":
			depth--
			if depth == 0 && i > 1 {
				return b.text(Span{ts[1].End(), t.Offset})
			}
		}
	}
	return ""
}

func joinLabel(a, b string) string {
	switch {
	case a == "":
		return b
	case b == "":
		return a
	default:
		return a + " → " + b
	}
}

func (b *builder) edges(p *ast.Pipeline, top []*Node) []Edge {
	out := []Edge{}
	if len(top) == 0 {
		return append(out, Edge{From: StartID, To: EndID, Kind: "next"})
	}
	first := top[0]
	for _, n := range top {
		if n.Entry {
			first = n
			break
		}
	}
	out = append(out, Edge{From: StartID, To: first.ID, Kind: "next"})

	routes := Routes(p)
	steps := map[string]*ast.Step{}
	for _, m := range p.Body {
		if m.Step != nil {
			steps[m.Step.Name] = m.Step
		}
	}
	for i, n := range top {
		if st := steps[n.ID]; st != nil && n.Kind == "step" {
			WalkGotos(st.Body, routes, func(g GotoRef) {
				e := Edge{From: n.ID, To: g.Target, Label: b.guardLabel(g.Guard, g.Else)}
				if g.Target == "" {
					e.To = FailID
				}
				switch {
				case g.Route != nil:
					e.Kind, e.Via = "route", g.Route.Name
					arm := b.arm(g.Arm)
					e.Label = joinLabel(e.Label, arm.Pattern)
					e.Span = arm.TargetSpan
				case g.Arm != nil:
					e.Kind = "match"
					arm := b.arm(g.Arm)
					e.Label = joinLabel(e.Label, arm.Pattern)
					e.Span = arm.TargetSpan
				default:
					e.Kind = "goto"
					e.Span = b.gotoTarget(g.Stmt)
				}
				out = append(out, e)
			})
			walkTerminals(st.Body, nil, false, func(kind string, guard *ast.Statement, inElse bool) {
				switch kind {
				case "break", "complete":
					out = append(out, Edge{From: n.ID, To: EndID, Kind: "end", Label: joinLabel(b.guardLabel(guard, inElse), kind)})
				case "fail":
					out = append(out, Edge{From: n.ID, To: FailID, Kind: "fail", Label: b.guardLabel(guard, inElse)})
				}
			})
		}
		if n.FallsThrough {
			to := EndID
			if i+1 < len(top) {
				to = top[i+1].ID
			}
			out = append(out, Edge{From: n.ID, To: to, Kind: "next"})
		}
	}
	return out
}

// gotoTarget is the span of the step name in a plain `goto X` /
// `goto nameof(X)` / `goto Route(...)` statement.
func (b *builder) gotoTarget(s *ast.Statement) *Span {
	ts := b.in(Span{s.Pos.Offset, s.EndPos.Offset})
	for i, t := range ts {
		if t.Kind != "Ident" || t.Value != "goto" || i+1 >= len(ts) {
			continue
		}
		j := i + 1
		if ts[j].Value == "nameof" && j+2 < len(ts) {
			j += 2
		}
		sp := tokSpan(ts[j])
		return &sp
	}
	return nil
}

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

// agentShape is an agent's literal `command` (empty when computed) and where
// it puts a call's `schema:` (see Declaration.Schema).
func agentShape(a *ast.Agent) (command, schema string) {
	var args, stdin *ast.Expr
	for _, p := range a.Props {
		switch p.Name {
		case "command":
			command, _ = ast.StringValue(p.Value)
		case "args":
			args = p.Value
		case "stdin":
			stdin = p.Value
		}
	}
	const placeholder = "${schema}"
	known := true
	if stdin != nil {
		s, ok := ast.StringValue(stdin)
		if ok && strings.Contains(s, placeholder) {
			return command, "stdin"
		}
		known = known && ok
	}
	if args != nil {
		arr := ast.BareArray(args)
		if arr == nil {
			return command, ""
		}
		for _, item := range arr.Items {
			if s, ok := ast.StringValue(item); ok && s == placeholder {
				return command, "args"
			}
		}
	}
	if !known {
		return command, ""
	}
	return command, "none"
}
