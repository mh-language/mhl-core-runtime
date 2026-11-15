package lint

import (
	"fmt"

	"github.com/alecthomas/participle/v2/lexer"

	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
	"github.com/mh-language/mhl-core-runtime/internal/lang/types"
)

// checkEntryRoutes validates `entry route` (ast.WorkflowRoute.Entry), the
// route a workflow's run starts by dispatching on:
//
//   - a workflow carries at most one (a partial's fragments are counted
//     together with `entry step`s by mergePartials);
//   - its name is not also a step's or parallel group's: it runs as a stage
//     under that name (runtime.PipelineFromAST), so they would collide in
//     step events, checkpoints and goto;
//   - its parameter is bound to the workflow variable of the same name, so
//     that must be an `input` (or the typed signature's param) of the same
//     type.
//
// The rules shared with every route (one typed parameter, `workflow` only,
// declared arm targets) stay in checkPipelineGoto.
func checkEntryRoutes(file string, prog *ast.Program, aliases map[string]types.Type) []Finding {
	var findings []Finding
	at := func(pos lexer.Position, msg string) {
		findings = append(findings, Finding{File: fileAt(file, pos), Line: pos.Line, Column: pos.Column, Message: msg})
	}
	for _, decl := range prog.Decls {
		p := decl.Pipeline
		if p == nil {
			continue
		}
		var entries []*ast.WorkflowRoute
		names := map[string]bool{}
		for _, m := range p.Body {
			if m.Route != nil && m.Route.Entry {
				entries = append(entries, m.Route)
			}
			for _, s := range pipelineMemberSteps(m) {
				names[s.Name] = true
			}
			if m.Parallel != nil {
				names[m.Parallel.Name] = true
			}
		}
		if len(entries) == 0 {
			continue
		}
		if len(entries) > 1 && !p.Partial {
			for _, r := range entries[1:] {
				at(r.Pos, fmt.Sprintf("workflow %q has more than one `entry route` (%q is already its entry)", p.Name, entries[0].Name))
			}
		}
		// A partial short of its single entry may be missing the fragment
		// that declares the input; checkPipelineGoto skips likewise.
		incomplete := p.Partial && p.EntryStepCount() != 1
		inputs := pipelineInputTypes(p, aliases)
		for _, r := range entries {
			if names[r.Name] {
				at(r.Pos, fmt.Sprintf("entry route %q has the name of a step of workflow %q: it runs as a step of its own — rename one", r.Name, p.Name))
			}
			if len(r.Params) != 1 || r.Params[0].Type == nil || incomplete {
				continue // checkPipelineGoto reports a malformed parameter
			}
			param := r.Params[0]
			in, ok := inputs[param.Name]
			if !ok {
				at(param.Pos, fmt.Sprintf("entry route %q dispatches on input %q, which workflow %q doesn't declare — add `input %s: %s`, or rename the parameter after an input", r.Name, param.Name, p.Name, param.Name, param.Type.String()))
				continue
			}
			pt, ok := types.FromExprAlias(param.Type, aliases)
			if ok && in.Kind != types.AnyKind && !pt.Equal(in) {
				at(param.Pos, fmt.Sprintf("entry route %q parameter %q is %s, but input %q is %s", r.Name, param.Name, pt.String(), param.Name, in.String()))
			}
		}
	}
	return findings
}
