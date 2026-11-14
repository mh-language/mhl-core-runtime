package lint

import (
	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
	"github.com/mh-language/mhl-core-runtime/internal/lang/flow"
)

// alwaysDiverts is flow.AlwaysDiverts — the fallthrough rule lives there so
// the IDE graph (mhl graph) and this check can never disagree.
func alwaysDiverts(stmts []*ast.Statement) bool { return flow.AlwaysDiverts(stmts) }

// lastStepLikeMember returns the last Step or Parallel-group member of
// body, skipping any trailing non-stage member (a `var`/`input`/`route`/
// property declared, unusually, after the last step) — nil if body
// declares no step/parallel member at all.
func lastStepLikeMember(body []*ast.PipelineMember) *ast.PipelineMember {
	for i := len(body) - 1; i >= 0; i-- {
		if body[i].Step != nil || body[i].Parallel != nil {
			return body[i]
		}
	}
	return nil
}

// firstStepLikeName returns the name of the first Step or Parallel-group
// member of body, skipping any leading non-stage member — "" if body
// declares no step/parallel member at all.
func firstStepLikeName(body []*ast.PipelineMember) string {
	for _, m := range body {
		switch {
		case m.Step != nil:
			return m.Step.Name
		case m.Parallel != nil:
			return m.Parallel.Name
		}
	}
	return ""
}
