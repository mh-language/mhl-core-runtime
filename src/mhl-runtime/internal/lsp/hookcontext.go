package lsp

import (
	"sort"

	"github.com/mh-language/mhl-core-runtime/internal/lang/types"
)

// hookParamTypes maps each lifecycle-hook lambda's own blockKind (see
// blockcontext.go) to the builtin global type its single parameter is bound
// to — SessionContext/StepContext/FailureContext, the same names
// ast.PipelineBodyProperty.ParamType records and any `: Type` annotation
// elsewhere in the language would resolve through types.Parse. Kept here,
// next to the completion logic that's this table's only reader, rather than
// derived from ast.PipelineBodyProperties at init time — blockKind values
// are this package's own vocabulary, unrelated to the property name string
// PipelineBodyProperty keys off.
var hookParamTypes = map[blockKind]string{
	blockSessionStartHook: "SessionContext",
	blockSessionEndHook:   "SessionContext",
	blockStepStartHook:    "StepContext",
	blockStepEndHook:      "StepContext",
	blockStopFailureHook:  "FailureContext",
}

// hookParamCompletionAt returns field-completion items for target when it is
// exactly the literal parameter name the user wrote for the lifecycle hook
// lambda directly enclosing pos — e.g. typing `s.` inside `session_start: (s)
// -> { s. }` — or nil, false when the cursor isn't inside one of the five
// hooks' lambda bodies, or target names something else (a sibling variable,
// a declared symbol). Mirrors selfCompletionAt's shape (a hardcoded
// special-case checked before the generic memberAccessRe symbol lookup in
// completionAt), but resolves its field list from a real types.Type instead
// of a second hand-written table — see hookLambdaType and
// PipelineBodyProperty.ParamType's doc comment for why the shape itself
// isn't duplicated here.
func hookParamCompletionAt(text string, pos position, target string) ([]completionItem, bool) {
	stack := blockStack(textUpToPosition(text, pos))
	if len(stack) == 0 {
		return nil, false
	}
	top := stack[len(stack)-1]
	typeName, known := hookParamTypes[top.Kind]
	if !known || top.Name != target {
		return nil, false
	}
	return objectTypeFieldItems(typeName), true
}

// objectTypeFieldItems resolves typeName through types.Parse — the same
// bare-name resolution any `: Type` annotation in the language goes through
// — and lists its declared fields as completion items, each field's own
// Type.String() rendering as Detail. nil for a name that isn't a declared
// object shape (an unknown name, or a non-object builtin like "string").
func objectTypeFieldItems(typeName string) []completionItem {
	t, ok := types.Parse(typeName)
	if !ok || t.Kind != types.ObjectKind || t.Fields == nil {
		return nil
	}
	names := make([]string, 0, len(t.Fields))
	for name := range t.Fields {
		names = append(names, name)
	}
	sort.Strings(names)
	items := make([]completionItem, 0, len(names))
	for _, name := range names {
		items = append(items, completionItem{Label: name, Kind: kindField, Detail: t.Fields[name].String()})
	}
	return items
}
