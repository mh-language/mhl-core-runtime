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
	"fmt"
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
	// Severity is "warning" for a lint warning, absent for an error.
	Severity string `json:"severity,omitempty"`
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
	Schema   string   `json:"schema,omitempty"`
	Methods  []Method `json:"methods,omitempty"`  // tool methods, `internal` ones included
	Variants []string `json:"variants,omitempty"` // an enum's variants, in order
	Targets  []string `json:"targets,omitempty"`  // a prompt's `for` variant patterns, in order (its default is Detail/the body)
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
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	Loop    bool   `json:"loop,omitempty"`
	Partial bool   `json:"partial,omitempty"`
	// Merged marks a `partial` workflow projected from every fragment
	// (`mhl graph --merged`, BuildMerged) rather than from this file's
	// fragment alone. Its members then carry File when they come from
	// another fragment; the workflow's own spans stay this file's.
	Merged      bool    `json:"merged,omitempty"`
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
	// Stages are the workflow's stages, in order — each the header stage of
	// one of its fragments (all of them in a Merged workflow, else this
	// file's): the named phases its steps and parallel groups belong to (each
	// member node names its stage in Node.Stage). Absent without any.
	Stages []StageGroup `json:"stages,omitempty"`
	Nodes  []Node       `json:"nodes"`
	Edges  []Edge       `json:"edges"`
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
	// File is the absolute path of the fragment this member was declared
	// in, set only in a Merged workflow and only when that is another file
	// than the one graphed — Span then indexes that file, not this one.
	File string `json:"file,omitempty"`
	// Stage is the named stage the input belongs to (declared in a
	// `partial ... stage S` fragment): the run asks for it on entering S.
	// Absent for a workflow-level input.
	Stage string `json:"stage,omitempty"`
	// Doc is the input's `///` documentation, "" when it has none.
	Doc string `json:"doc,omitempty"`
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
	File string `json:"file,omitempty"` // as Input.File
}

// Prop is a body property (`description:`, `output:`, a hook, ...).
type Prop struct {
	Name string `json:"name"`
	Span Span   `json:"span"`
	File string `json:"file,omitempty"` // as Input.File
}

// StageGroup is a `partial` fragment's header stage (`partial workflow X
// stage Name { ... }`, ast.Stage): a named phase made of the fragment's steps
// and parallel groups. `goto Name` enters it at its first member, so an edge
// may target a stage's name. Span and the body braces are the fragment's,
// HeaderSpan its header, NameSpan the name after `stage`.
type StageGroup struct {
	Name string `json:"name"`
	// Members are the ids of its steps and parallel groups, in order.
	Members    []string `json:"members"`
	Span       Span     `json:"span"`
	HeaderSpan Span     `json:"headerSpan"`
	NameSpan   Span     `json:"nameSpan"`
	BodyOpen   int      `json:"bodyOpen"`
	BodyClose  int      `json:"bodyClose"`
	File       string   `json:"file,omitempty"` // as Input.File
}

// Route is a `route Name(params) { arms }` dispatch table. Entry marks an
// `entry route`: the run starts by dispatching on it, so the start edges
// are its arms (kind "route", from StartID).
type Route struct {
	Name   string   `json:"name"`
	Entry  bool     `json:"entry,omitempty"`
	Params []string `json:"params"`
	Span   Span     `json:"span"`
	Arms   []Arm    `json:"arms"`
	File   string   `json:"file,omitempty"` // as Input.File
}

// Arm is one `pattern -> Target` / `pattern -> fail(...)` arm.
type Arm struct {
	Pattern  string `json:"pattern"`
	Target   string `json:"target,omitempty"`
	Fail     bool   `json:"fail,omitempty"`
	Wildcard bool   `json:"wildcard,omitempty"`
	Span     Span   `json:"span"`
	// PatternSpan covers the pattern (`_` for the wildcard), ThenSpan what
	// follows `->` (the target name or the whole `fail(...)`), so an editor
	// can rewrite either side alone.
	PatternSpan Span  `json:"patternSpan"`
	ThenSpan    Span  `json:"thenSpan"`
	TargetSpan  *Span `json:"targetSpan,omitempty"`
}

// Merge is a parallel group's `merge (Param) { ... }` join (ast.ParallelMerge):
// it runs once after every branch, as the step "<group>.merge" (StepName).
type Merge struct {
	StepName string `json:"stepName"`
	// Param is "" for `merge { ... }`, and ParamSpan then zero.
	Param     string `json:"param"`
	ParamSpan Span   `json:"paramSpan"`
	// Span runs from `merge` through the body's closing brace.
	Span      Span       `json:"span"`
	BodyOpen  int        `json:"bodyOpen"`
	BodyClose int        `json:"bodyClose"`
	Calls     []string   `json:"calls"`
	CallSites []CallSite `json:"callSites"`
}

// Choice is a step's top-level `goto match Subject { arms }` statement —
// what an editor shows as a Step Functions-style Choice state.
type Choice struct {
	Subject     string `json:"subject"`
	SubjectSpan Span   `json:"subjectSpan"`
	// Span runs from `goto` through the closing `}`; Close is that `}`.
	Span  Span  `json:"span"`
	Close int   `json:"close"`
	Arms  []Arm `json:"arms"`
}

// Node is a step or a parallel group. Steps inside a group are nodes too,
// with Parent naming the group.
type Node struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"` // "step" | "parallel"
	Name   string `json:"name"`
	Parent string `json:"parent,omitempty"`
	// Stage names the stage of the fragment a top-level step or parallel
	// group was declared in ("" for none; a group's branches carry their
	// group's Parent instead).
	Stage string `json:"stage,omitempty"`
	// File is as Input.File: every span of this node (and of its outgoing
	// edges) indexes that fragment's source.
	File        string   `json:"file,omitempty"`
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
	CallSites []CallSite `json:"callSites"`
	// Choice is the body's first top-level `goto match`, if any.
	Choice *Choice `json:"choice,omitempty"`
	// Vars are a parallel group's own `var`s, declared before its branches.
	Vars []Var `json:"vars,omitempty"`
	// Merge is a parallel group's `merge (param) { ... }` block, if any.
	Merge        *Merge   `json:"merge,omitempty"`
	Exits        []string `json:"exits"`
	FallsThrough bool     `json:"fallsThrough"`
	// Decisions are the decision types of the step's gates — `pause(reason,
	// decision: T)` — in source order; absent when it has none. A gate is a
	// human-in-the-loop hand-off like a bare pause, but returns the answer
	// instead of being an exit.
	Decisions []string `json:"decisions,omitempty"`
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
	b, g, err := newBuilder(prog, src)
	if err != nil {
		return nil, err
	}
	for _, d := range prog.Decls {
		if d.Pipeline != nil {
			g.Workflows = append(g.Workflows, b.workflow(d))
		}
	}
	return g, nil
}

// Fragment is one file's `partial` declaration of a workflow, for
// BuildMerged.
type Fragment struct {
	// File is the fragment's absolute path, or "" for the file being
	// graphed (whose spans the caller already indexes).
	File string
	Src  string
	Prog *ast.Program // the fragment file's whole program (its imports name callables)
	Decl *ast.Declaration
}

// BuildMerged projects every fragment of one `partial` workflow onto a
// single Workflow, in the order a run concatenates their bodies (the entry
// file's fragment first, then each import's, depth-first): the start edge
// goes to the one `entry` step, and a fragment's last step falls through to
// the next fragment's first, exactly as at run time. Members from a fragment
// with a File carry it, their spans indexing that file. The workflow's own
// header spans are those of the fragment with no File, else the first.
func BuildMerged(frags []Fragment) (Workflow, error) {
	if len(frags) == 0 {
		return Workflow{}, fmt.Errorf("flow: no fragments to merge")
	}
	home := frags[0]
	for _, f := range frags {
		if f.File == "" {
			home = f
			break
		}
	}
	hb, _, err := newBuilder(home.Prog, home.Src)
	if err != nil {
		return Workflow{}, err
	}
	w := hb.header(home.Decl)
	w.Merged = true
	merged := &ast.Pipeline{Pos: home.Decl.Pipeline.Pos, Partial: true, Kind: home.Decl.Pipeline.Kind, Name: home.Decl.Pipeline.Name}
	owner := map[string]*builder{}
	var top []string
	for _, f := range frags {
		b := hb
		if f.File != home.File || f.Src != home.Src {
			if b, _, err = newBuilder(f.Prog, f.Src); err != nil {
				return Workflow{}, fmt.Errorf("%s: %w", f.File, err)
			}
		}
		p := f.Decl.Pipeline
		b.stages(&w, p.Stages, f.File)
		w.Loop = w.Loop || p.Loop
		w.Internal = w.Internal || p.Internal
		if w.Max == "" {
			w.Max = p.Max
		}
		merged.Body = append(merged.Body, p.Body...)
		nodes := len(w.Nodes)
		routes := len(w.Routes)
		top = append(top, b.members(&w, p.Body, f.File)...)
		for _, n := range w.Nodes[nodes:] {
			owner[n.ID] = b
		}
		for _, r := range w.Routes[routes:] {
			owner[r.Name] = b
		}
	}
	finish(&w, merged, top, func(id string) *builder {
		if b := owner[id]; b != nil {
			return b
		}
		return hb
	})
	return w, nil
}

// newBuilder tokenizes src and collects prog's callable names; the Graph
// it returns holds prog's imports and declarations, no workflows yet.
func newBuilder(prog *ast.Program, src string) (*builder, *Graph, error) {
	toks, err := parser.Tokens(src)
	if err != nil {
		return nil, nil, err
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
	return b, g, nil
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
		decl.Targets = d.Prompt.Targets()
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
		decl.Kind, decl.Name, decl.Variants = "enum", d.Enum.Name, d.Enum.Variants
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
	w := b.header(d)
	b.stages(&w, d.Pipeline.Stages, "")
	top := b.members(&w, d.Pipeline.Body, "")
	finish(&w, d.Pipeline, top, func(string) *builder { return b })
	return w
}

// header projects a workflow declaration's own spans, signature and name —
// everything but its members.
func (b *builder) header(d *ast.Declaration) Workflow {
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
	return w
}

// stages appends a fragment's header stage to w, stamped with file.
func (b *builder) stages(w *Workflow, stages []*ast.Stage, file string) {
	for _, st := range stages {
		s := Span{st.Pos.Offset, st.EndPos.Offset}
		g := StageGroup{Name: st.Name, Members: append([]string{}, st.Members...), Span: s, File: file, BodyClose: b.closeOf(s)}
		ts := b.in(s)
		for i, t := range ts {
			// The name is the identifier right after the `stage` keyword.
			if t.Kind == "Ident" && t.Value == "stage" && i+1 < len(ts) && ts[i+1].Value == st.Name && g.NameSpan == (Span{}) {
				g.NameSpan = tokSpan(ts[i+1])
			}
			if t.Kind == "Punct" && t.Value == "{" {
				g.BodyOpen = t.Offset
				if i > 0 {
					g.HeaderSpan = Span{s[0], ts[i-1].End()}
				}
				break
			}
		}
		w.Stages = append(w.Stages, g)
	}
}

// members appends body's inputs, vars, props, routes and nodes to w, each
// stamped with file (see Input.File), and returns the ids of its top-level
// nodes — steps and parallel groups, in order — for finish.
func (b *builder) members(w *Workflow, body []*ast.PipelineMember, file string) []string {
	var top []string
	for _, m := range body {
		ms := Span{m.Pos.Offset, m.EndPos.Offset}
		switch {
		case m.Input != nil:
			in := Input{Name: m.Input.Name, Type: m.Input.Type.String(), Span: ms, File: file, Stage: m.Input.Stage, Doc: m.Input.Doc}
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
			w.Vars = append(w.Vars, Var{Name: m.Const.Name, Kind: "const", Span: ms, File: file})
		case m.Var != nil:
			w.Vars = append(w.Vars, Var{Name: m.Var.Name, Kind: "var", Span: ms, File: file})
		case m.Mem != nil:
			w.Vars = append(w.Vars, Var{Name: m.Mem.Name, Kind: "mem", Span: ms, File: file})
		case m.Prop != nil:
			w.Props = append(w.Props, Prop{Name: m.Prop.Name, Span: ms, File: file})
			if m.Prop.Name == "description" && w.Description == "" {
				w.Description, _ = ast.StringValue(m.Prop.Value)
			}
		case m.Route != nil:
			r := b.route(m.Route, ms)
			r.File = file
			w.Routes = append(w.Routes, r)
		case m.Step != nil:
			n := b.step(m.Step, ms, "")
			n.File, n.Stage = file, m.Step.Stage
			w.Nodes = append(w.Nodes, n)
			top = append(top, n.ID)
		case m.Parallel != nil:
			group := b.parallel(m.Parallel, ms)
			group.File, group.Stage = file, m.Parallel.Stage
			w.Nodes = append(w.Nodes, group)
			for _, st := range m.Parallel.Steps {
				n := b.step(st, Span{st.Pos.Offset, st.EndPos.Offset}, m.Parallel.Name)
				n.File = file
				w.Nodes = append(w.Nodes, n)
			}
			top = append(top, group.ID)
		}
	}
	return top
}

// finish computes w's edges over p — the whole (possibly merged) workflow —
// and its externals. builderOf names the builder whose source holds a
// node's (or route's) spans: one per file.
func finish(w *Workflow, p *ast.Pipeline, topIDs []string, builderOf func(id string) *builder) {
	byID := map[string]*Node{}
	for i := range w.Nodes {
		byID[w.Nodes[i].ID] = &w.Nodes[i]
	}
	top := make([]*Node, 0, len(topIDs))
	for _, id := range topIDs {
		top = append(top, byID[id])
	}
	w.Edges = edges(p, top, builderOf)
	w.Externals = []string{}
	stages := map[string]bool{}
	for _, s := range w.Stages {
		stages[s.Name] = true
	}
	for _, e := range w.Edges {
		if byID[e.To] == nil && !stages[e.To] && e.To != EndID && e.To != FailID && !contains(w.Externals, e.To) {
			w.Externals = append(w.Externals, e.To)
		}
	}
}

func (b *builder) route(r *ast.WorkflowRoute, s Span) Route {
	out := Route{Name: r.Name, Entry: r.Entry, Span: s, Params: []string{}, Arms: []Arm{}}
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
	out := Arm{Span: s, Target: a.Target, Fail: a.Fail != nil, Wildcard: a.Wildcard}
	ts := b.in(s)
	for i, t := range ts {
		if t.Kind == "Punct" && t.Value == "->" {
			out.Pattern = b.text(Span{s[0], t.Offset})
			if i > 0 {
				out.PatternSpan = Span{ts[0].Offset, ts[i-1].End()}
			}
			if i+1 < len(ts) {
				out.ThenSpan = Span{ts[i+1].Offset, ts[len(ts)-1].End()}
			}
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
	n.Choice = b.choice(st.Body)
	n.Exits = exits(st.Body)
	n.Decisions = decisions(st.Body)
	return n
}

// choice projects the first top-level `goto match` of a step body.
func (b *builder) choice(body []*ast.Statement) *Choice {
	for _, st := range body {
		if st.GotoMatch == nil {
			continue
		}
		s := Span{st.Pos.Offset, st.EndPos.Offset}
		ts := b.in(s)
		if len(ts) < 4 {
			return nil
		}
		i := bodyBrace(ts[2:])
		if i < 1 {
			return nil
		}
		c := &Choice{Span: s, Close: b.closeOf(s), Arms: []Arm{}}
		c.SubjectSpan = Span{ts[2].Offset, ts[2+i-1].End()}
		c.Subject = b.text(c.SubjectSpan)
		for _, a := range st.GotoMatch.Arms {
			c.Arms = append(c.Arms, b.arm(a))
		}
		return c
	}
	return nil
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
	for _, v := range pg.Vars {
		n.Vars = append(n.Vars, Var{Name: v.Decl.Name, Kind: "var", Span: Span{v.Pos.Offset, v.EndPos.Offset}})
	}
	if mg := pg.Merge; mg != nil {
		ms := Span{mg.Pos.Offset, mg.EndPos.Offset}
		m := &Merge{StepName: ast.MergeStepName(pg.Name), Param: mg.Param, Span: ms, BodyClose: b.closeOf(ms), Calls: b.calls(ms), CallSites: b.callSites(ms)}
		for _, t := range b.in(ms) {
			if mg.Param != "" && t.Kind == "Ident" && t.Value == mg.Param && m.ParamSpan == (Span{}) {
				m.ParamSpan = tokSpan(t)
			}
			if t.Kind == "Punct" && t.Value == "{" {
				m.BodyOpen = t.Offset
				break
			}
		}
		n.Merge = m
		n.FallsThrough = !AlwaysDiverts(mg.Body)
		n.Exits = exits(mg.Body)
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

func edges(p *ast.Pipeline, top []*Node, builderOf func(id string) *builder) []Edge {
	out := []Edge{}
	if len(top) == 0 {
		return append(out, Edge{From: StartID, To: EndID, Kind: "next"})
	}
	first := top[0]
	hasEntry := false
	for _, n := range top {
		if n.Entry {
			first, hasEntry = n, true
			break
		}
	}
	if r := p.EntryRoute(); r != nil {
		b := builderOf(r.Name)
		// The run starts by dispatching on the entry route: one start edge
		// per arm, flattened like a step's `goto Route(x)`.
		for _, a := range r.Arms {
			arm := b.arm(a)
			e := Edge{From: StartID, To: a.Target, Kind: "route", Via: r.Name, Label: arm.Pattern, Span: arm.TargetSpan}
			if a.Fail != nil {
				e.To = FailID
			}
			out = append(out, e)
		}
	} else if hasEntry || !p.Partial {
		// A partial fragment without the `entry` step is not where a run
		// starts: some other fragment's step reaches it.
		out = append(out, Edge{From: StartID, To: first.ID, Kind: "next"})
	}

	routes := Routes(p)
	// A step's transitions come from its body; a parallel group's, from its
	// merge (the only part of a group that may goto, after the barrier).
	steps := map[string]*ast.Step{}
	for _, m := range p.Body {
		if m.Step != nil {
			steps[m.Step.Name] = m.Step
		}
		if ms := m.Parallel.MergeStep(); ms != nil {
			steps[m.Parallel.Name] = ms
		}
	}
	for i, n := range top {
		b := builderOf(n.ID)
		if st := steps[n.ID]; st != nil {
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
