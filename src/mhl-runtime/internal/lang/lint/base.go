package lint

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/alecthomas/participle/v2/lexer"
	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
	"github.com/mh-language/mhl-core-runtime/internal/lang/parser"
	"github.com/mh-language/mhl-core-runtime/internal/lang/types"
)

// checkBaseRefs checks `base.name` in a partial workflow's fragments, given
// all of them (frags, in merge order). The base is the fragment holding the
// `entry step` / `entry route`; `base.name` (interpreter.isBaseRef) reads or
// writes a pipeline input/var/mem/const exactly like `self.name`, and says
// where it is declared:
//
//   - `base.x` where the base declares no x is an error;
//   - in any other fragment, a name the base declares used bare (or as
//     `self.x`) is a warning suggesting `base.x` — the reader of that file
//     otherwise can't tell where it comes from.
//
// A name the fragment declares itself, or a step declares locally, is its
// own and never flagged. Skipped without exactly one entry (another check
// reports that) — e.g. a lone stage fragment linted on its own.
func checkBaseRefs(file string, prog *ast.Program, frags []*ast.Pipeline) []Finding {
	base := baseFragment(frags)
	if base == nil {
		return nil
	}
	declared := fragmentNames(base)
	var findings []Finding
	for _, f := range frags {
		own := fragmentNames(f)
		isBase := f == base
		for _, m := range f.Body {
			groupLocals := map[string]bool{}
			if m.Parallel != nil {
				for _, v := range m.Parallel.Vars {
					groupLocals[v.Decl.Name] = true
				}
			}
			for _, st := range pipelineMemberSteps(m) {
				locals := stepLocals(prog, st)
				for n := range groupLocals {
					locals[n] = true
				}
				w := baseWalker{file: file, declared: declared, own: own, locals: locals, isBase: isBase}
				w.walk(reflect.ValueOf(st.Body), st.Pos)
				findings = append(findings, w.findings...)
			}
		}
	}
	return findings
}

// checkBaseOutsidePartial reports `base.x` in a workflow that is not
// partial: it has no base fragment to name.
func checkBaseOutsidePartial(file string, prog *ast.Program) []Finding {
	var findings []Finding
	for _, d := range prog.Decls {
		p := d.Pipeline
		if p == nil || p.Partial {
			continue
		}
		for _, m := range p.Body {
			for _, st := range pipelineMemberSteps(m) {
				locals := stepLocals(prog, st)
				w := baseWalker{file: file, locals: locals, noBase: p}
				w.walk(reflect.ValueOf(st.Body), st.Pos)
				findings = append(findings, w.findings...)
			}
		}
	}
	return findings
}

// baseFragment is the fragment declaring the entry step or route, nil
// unless exactly one does.
func baseFragment(frags []*ast.Pipeline) *ast.Pipeline {
	var base *ast.Pipeline
	for _, f := range frags {
		for _, m := range f.Body {
			if (m.Step != nil && m.Step.Entry) || (m.Route != nil && m.Route.Entry) {
				if base != nil && base != f {
					return nil
				}
				base = f
			}
		}
	}
	return base
}

// fragmentNames are the pipeline-level names one fragment declares, each
// with the position of its declaration.
func fragmentNames(p *ast.Pipeline) map[string]lexer.Position {
	out := map[string]lexer.Position{}
	if p.Param != nil {
		out[p.Param.Name] = p.Pos
	}
	for _, m := range p.Body {
		switch {
		case m.Input != nil:
			out[m.Input.Name] = m.Input.Pos
		case m.Var != nil:
			out[m.Var.Name] = m.Pos
		case m.Mem != nil:
			out[m.Mem.Name] = m.Pos
		case m.Const != nil:
			out[m.Const.Name] = m.Pos
		}
	}
	return out
}

// stepLocals are the names a step binds itself — vars, consts, loop and
// spawn variables (collectVarNames), lambda parameters and catch bindings —
// flat over the whole body, as the rest of lint scopes them.
func stepLocals(prog *ast.Program, st *ast.Step) map[string]bool {
	out := map[string]bool{}
	for n := range collectVarNames(prog, st.Body, map[string]types.Type{}, nil) {
		out[n] = true
	}
	var walk func(v reflect.Value)
	walk = func(v reflect.Value) {
		switch v.Kind() {
		case reflect.Ptr, reflect.Interface:
			if !v.IsNil() {
				walk(v.Elem())
			}
		case reflect.Slice:
			for i := 0; i < v.Len(); i++ {
				walk(v.Index(i))
			}
		case reflect.Struct:
			switch x := v.Addr().Interface().(type) {
			case *ast.Lambda:
				for _, p := range x.Params {
					out[p.Name] = true
				}
			case *ast.TryStmt:
				if x.ErrName != "" {
					out[x.ErrName] = true
				}
			case *ast.ForInStmt:
				out[x.VarName] = true
			}
			for i := 0; i < v.NumField(); i++ {
				if v.Type().Field(i).IsExported() {
					walk(v.Field(i))
				}
			}
		}
	}
	walk(reflect.ValueOf(st.Body))
	return out
}

type baseWalker struct {
	file     string
	declared map[string]lexer.Position // the base fragment's names
	own      map[string]lexer.Position // this fragment's names
	locals   map[string]bool
	isBase   bool
	noBase   *ast.Pipeline // set when checking a non-partial workflow
	findings []Finding
	seen     map[string]bool
	// frozen keeps at fixed inside a `${...}` span: the span's expression
	// was parsed on its own, so its positions are relative to it.
	frozen bool
}

func (w *baseWalker) report(pos lexer.Position, sev Severity, format string, args ...any) {
	key := fmt.Sprintf("%d:%d:%s", pos.Line, pos.Column, fmt.Sprintf(format, args...))
	if w.seen == nil {
		w.seen = map[string]bool{}
	}
	if w.seen[key] {
		return
	}
	w.seen[key] = true
	w.findings = append(w.findings, Finding{File: fileAt(w.file, pos), Line: pos.Line, Column: pos.Column, Severity: sev, Message: fmt.Sprintf(format, args...)})
}

// walk visits every Postfix under v; at is the nearest enclosing position.
func (w *baseWalker) walk(v reflect.Value, at lexer.Position) {
	switch v.Kind() {
	case reflect.Ptr, reflect.Interface:
		if !v.IsNil() {
			w.walk(v.Elem(), at)
		}
	case reflect.Slice:
		for i := 0; i < v.Len(); i++ {
			w.walk(v.Index(i), at)
		}
	case reflect.Struct:
		if pos := v.FieldByName("Pos"); pos.IsValid() && !w.frozen {
			if p, ok := pos.Interface().(lexer.Position); ok && p.Line > 0 {
				at = p
			}
		}
		switch x := v.Addr().Interface().(type) {
		case *ast.Postfix:
			w.postfix(x, at)
		case *ast.Primary:
			if x.Str != nil {
				w.interpolation(*x.Str, at)
			} else if x.MultiStr != nil {
				w.interpolation(*x.MultiStr, at)
			}
		}
		for i := 0; i < v.NumField(); i++ {
			if v.Type().Field(i).IsExported() {
				w.walk(v.Field(i), at)
			}
		}
	}
}

func (w *baseWalker) postfix(p *ast.Postfix, at lexer.Position) {
	if p.Primary == nil || p.Primary.Ident == "" {
		return
	}
	name := p.Primary.Ident
	member := ""
	if len(p.Ops) > 0 && p.Ops[0].Member != "" && !p.Ops[0].Optional && !(len(p.Ops) > 1 && p.Ops[1].Call != nil) {
		member = p.Ops[0].Member
	}
	switch {
	case name == "base" && member != "" && !w.locals["base"]:
		if w.noBase != nil {
			w.report(at, SeverityError, "base.%s: `base.` names the base fragment of a partial workflow — %s %q is not partial; write %s", member, w.noBase.Kind, w.noBase.Name, member)
			return
		}
		if _, ok := w.declared[member]; !ok {
			w.report(at, SeverityError, "base.%s: the base fragment (the one with the entry step) declares no input, var, mem or const %q", member, member)
		}
	// No suggestion where `base.` can't be written: in a non-partial
	// workflow, in the base itself, or in a step with a variable named base.
	case w.noBase != nil || w.isBase || w.locals["base"]:
	case name == "self" && member != "":
		if w.fromBase(member) {
			w.report(at, SeverityWarning, "self.%s is declared in the base fragment (%s): write base.%s so its origin shows", member, w.where(member), member)
		}
	case !w.locals[name] && w.fromBase(name):
		w.report(at, SeverityWarning, "%s is declared in the base fragment (%s): write base.%s so its origin shows", name, w.where(name), name)
	}
}

// fromBase reports whether name is the base fragment's and not this one's.
func (w *baseWalker) fromBase(name string) bool {
	_, inBase := w.declared[name]
	_, mine := w.own[name]
	return inBase && !mine
}

func (w *baseWalker) where(name string) string {
	pos := w.declared[name]
	file := fileAt(w.file, pos)
	if i := strings.LastIndexAny(file, `/\`); i >= 0 {
		file = file[i+1:]
	}
	return fmt.Sprintf("%s:%d", file, pos.Line)
}

// interpolation checks the expressions of a string's `${...}` spans.
func (w *baseWalker) interpolation(s string, at lexer.Position) {
	for i := 0; i < len(s); {
		start := strings.Index(s[i:], "${")
		if start < 0 {
			return
		}
		start += i
		end, ok := parser.MatchInterpolationSpan(s, start)
		if !ok {
			return
		}
		if e, err := parser.ParseExpr(s[start+2 : end]); err == nil {
			outer := w.frozen
			w.frozen = true
			w.walk(reflect.ValueOf(e), at)
			w.frozen = outer
		}
		i = end + 1
	}
}
