package lint

import (
	"fmt"
	"strings"

	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
)

// hookParamTypes maps each lifecycle hook (ast.PipelineBodyProperties with a
// ParamType) to the context type its parameter is bound to.
var hookParamTypes = func() map[string]string {
	m := map[string]string{}
	for _, p := range ast.PipelineBodyProperties {
		if p.ParamType != "" {
			m[p.Name] = p.ParamType
		}
	}
	return m
}()

// hookEventList is the lifecycle events, in declaration order, for a message.
var hookEventList = func() string {
	var names []string
	for _, p := range ast.PipelineBodyProperties {
		if p.ParamType != "" {
			names = append(names, p.Name)
		}
	}
	return strings.Join(names, ", ")
}()

// hookParamHint is the conventional parameter name a hook's message shows.
func hookParamHint(event string) string {
	switch hookParamTypes[event] {
	case "SessionContext":
		return "session"
	case "FailureContext":
		return "failure"
	default:
		return "step"
	}
}

// checkHookSpelling checks one body property against the two spellings of a
// lifecycle hook. An `on <event> { ... }` hook (Property.On) must name a
// lifecycle event and take at most one parameter. A hook written as a
// property — `step_end: (step) -> { ... }` — still works, with a warning to
// write it as an event handler instead.
func checkHookSpelling(file string, p *ast.Pipeline, prop *ast.Property) []Finding {
	at := func(msg string, sev Severity) []Finding {
		return []Finding{{File: fileAt(file, prop.Pos), Line: prop.Pos.Line, Column: prop.Pos.Column, Severity: sev,
			Message: fmt.Sprintf("%s %q: %s", pipelineKind(p), p.Name, msg)}}
	}
	_, isHook := hookParamTypes[prop.Name]
	switch {
	case prop.On && !isHook:
		return at(fmt.Sprintf("`on %s`: not a lifecycle event — the events are %s", prop.Name, hookEventList), SeverityError)
	case prop.On && len(prop.OnParams) > 1:
		return at(fmt.Sprintf("`on %s` takes at most one parameter (the %s), got %d", prop.Name, hookParamTypes[prop.Name], len(prop.OnParams)), SeverityError)
	case !prop.On && isHook:
		return at(fmt.Sprintf("`%s: (...) -> { ... }` is deprecated — write the hook as an event handler: `on %s(%s) { ... }` (the parameter is optional)",
			prop.Name, prop.Name, hookParamHint(prop.Name)), SeverityWarning)
	}
	return nil
}

// checkDuplicateHooks flags a lifecycle event handled twice in one body:
// only the last handler would run, which an `on step_end { ... }` written
// twice does not suggest.
func checkDuplicateHooks(file string, prog *ast.Program) []Finding {
	var findings []Finding
	for _, decl := range prog.Decls {
		p := decl.Pipeline
		if p == nil {
			continue
		}
		seen := map[string]bool{}
		for _, m := range p.Body {
			if m.Prop == nil {
				continue
			}
			if _, isHook := hookParamTypes[m.Prop.Name]; !isHook {
				continue
			}
			if seen[m.Prop.Name] {
				findings = append(findings, Finding{File: fileAt(file, m.Prop.Pos), Line: m.Prop.Pos.Line, Column: m.Prop.Pos.Column,
					Message: fmt.Sprintf("%s %q: %s is handled twice — only one handler per event runs; merge them into one `on %s { ... }`", pipelineKind(p), p.Name, m.Prop.Name, m.Prop.Name)})
			}
			seen[m.Prop.Name] = true
		}
	}
	return findings
}
