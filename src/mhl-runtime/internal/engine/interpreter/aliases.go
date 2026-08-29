package interpreter

import (
	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
	"github.com/mh-language/mhl-core-runtime/internal/lang/types"
)

// aliasTypesFor resolves prog's `type X = ...` declarations into the Type
// vocabulary, for evalCtx.aliasTypes. Resolution errors (a cycle, an unknown
// target) are intentionally dropped: the affected alias is left out of the
// map, so a `: X` annotation that uses it falls through to types.Parse and
// fails as "unrecognized type" at the annotation's use site — the same shape
// a plain keyword typo already produces — while `mhl lint` reports the root
// cause against the `type` declaration itself.
func aliasTypesFor(prog *ast.Program) map[string]types.Type {
	m, _ := types.Aliases(prog)
	return m
}
