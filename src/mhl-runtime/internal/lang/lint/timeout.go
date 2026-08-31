package lint

import (
	"fmt"

	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
)

// checkPipelineStepTimeout validates the optional `timeout <dur>` header
// clause on a step or a `parallel` group: the value must be a positive
// duration. runtime.PipelineFromAST drops a non-positive one silently
// (best-effort, like every other reader there), so without this check a
// `timeout 0s` would just mean "no cap" with no feedback. A malformed unit
// never reaches here — the lexer only tokenises `[0-9]+(ms|s|m|h|d)` as a
// Duration, so anything else is already a parse error.
func checkPipelineStepTimeout(file string, prog *ast.Program) []Finding {
	var findings []Finding
	report := func(kind, name, raw string, line, col int) {
		if raw == "" {
			return
		}
		if d, ok := ast.ParseDuration(raw); !ok || d <= 0 {
			findings = append(findings, Finding{
				File: file, Line: line, Column: col,
				Message: fmt.Sprintf("%s %q: timeout %q is not a positive duration (e.g. 30s, 5m, 2h)", kind, name, raw),
			})
		}
	}
	for _, decl := range prog.Decls {
		if decl.Pipeline == nil {
			continue
		}
		for _, m := range decl.Pipeline.Body {
			if m.Parallel != nil {
				report("parallel group", m.Parallel.Name, m.Parallel.Timeout, m.Parallel.Pos.Line, m.Parallel.Pos.Column)
			}
			for _, s := range pipelineMemberSteps(m) {
				report("step", s.Name, s.Timeout, stepLine(s), 1)
			}
		}
	}
	return findings
}
