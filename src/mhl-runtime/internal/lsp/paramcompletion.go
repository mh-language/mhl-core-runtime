package lsp

import (
	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
	"github.com/mh-language/mhl-core-runtime/internal/lang/parser"
	"github.com/mh-language/mhl-core-runtime/internal/lang/types"
)

// paramTypeCompletionAt is hookParamCompletionAt's general-purpose sibling:
// it returns field-completion items for target when it resolves to an
// *ordinary* typed tool-method parameter — `read(session: SessionContext):
// ... -> { session.§ }` — not just the one lambda parameter a
// session_start/session_end/step_start/step_end/stop_failure hook binds.
// SessionContext et al. are real global types (types.Parse resolves them
// from anywhere a `: Type` annotation appears — a tool method param/return,
// a pipeline `input`), so restricting field completion to hook bodies was
// always an LSP-only gap, not a language one: `mhl lint`/`mhl run` already
// accept and type-check `session.vars` inside a tool method exactly like
// they do inside a hook lambda.
//
// Reuses selfCompletionAt's established approach (repairForParse +
// blockStack to find the enclosing declaration by name, then a real
// parser.Parse to read its actual fields) instead of a second hand-rolled
// parameter-list scanner — and, unlike objectTypeFieldItems'
// (types.Parse-only) resolution, also resolves a param typed with the
// current file's own `type X = ...` alias via types.Aliases, since a tool
// method parameter can be annotated with either kind of name.
func paramTypeCompletionAt(text string, pos position, target string) ([]completionItem, bool) {
	repaired := repairForParse(text, pos)
	stack := blockStack(textUpToPosition(repaired, pos))
	toolName, found := "", false
	for i := len(stack) - 1; i >= 0; i-- {
		if stack[i].Kind == blockTool {
			toolName, found = stack[i].Name, true
			break
		}
	}
	if !found {
		return nil, false
	}
	prog, err := parser.Parse(repaired)
	if err != nil {
		return nil, false
	}
	var tool *ast.Tool
	for _, decl := range prog.Decls {
		if decl.Tool != nil && decl.Tool.Name == toolName {
			tool = decl.Tool
			break
		}
	}
	if tool == nil {
		return nil, false
	}
	method := toolMethodAtLine(tool, pos.Line)
	if method == nil {
		return nil, false
	}
	var param *ast.Param
	for _, p := range method.Params {
		if p.Name == target {
			param = p
			break
		}
	}
	if param == nil || param.Type == nil {
		return nil, false
	}
	aliasTypes, _ := types.Aliases(prog)
	t, ok := types.FromExprAlias(param.Type, aliasTypes)
	if !ok {
		return nil, false
	}
	return fieldCompletionItems(t), true
}

// toolMethodAtLine returns tool's method most likely to contain the
// (0-indexed) LSP line line: the last method, in source order, whose own
// declaration starts at or before it. Methods never overlap and ast.Tool's
// Method list is already in source order (parser.Parse's merge step, see
// package parser's doc comment), so this exactly identifies the enclosing
// method with no need for an end position ast.ToolMethod doesn't carry —
// callers already know line falls inside *some* method's body (blockTool
// was found enclosing it), just not which one.
func toolMethodAtLine(tool *ast.Tool, line int) *ast.ToolMethod {
	var best *ast.ToolMethod
	for _, m := range tool.Methods {
		if m.Pos.Line-1 > line {
			continue
		}
		if best == nil || m.Pos.Line > best.Pos.Line {
			best = m
		}
	}
	return best
}

// pipelineParamCompletionAt is paramTypeCompletionAt for a pipeline/workflow's
// typed signature param: `workflow W(req: ReviewInput) { step S { req.§ } }`
// lists ReviewInput's fields. Same approach — blockStack for the enclosing
// declaration's name, then a real parse (alias-aware) for its param type.
// Only the declaration's own header is consulted; a `partial` fragment
// whose signature lives in a sibling file gets no field items.
func pipelineParamCompletionAt(text string, pos position, target string) ([]completionItem, bool) {
	repaired := repairForParse(text, pos)
	stack := blockStack(textUpToPosition(repaired, pos))
	name, found := "", false
	for i := len(stack) - 1; i >= 0; i-- {
		if k := stack[i].Kind; k == blockPipeline || k == blockLoopPipeline {
			name, found = stack[i].Name, true
			break
		}
	}
	if !found {
		return nil, false
	}
	prog, err := parser.Parse(repaired)
	if err != nil {
		return nil, false
	}
	for _, decl := range prog.Decls {
		p := decl.Pipeline
		if p == nil || p.Name != name || p.Param == nil || p.Param.Name != target {
			continue
		}
		aliasTypes, _ := types.Aliases(prog)
		t, ok := types.FromExprAlias(p.Param.Type, aliasTypes)
		if !ok {
			return nil, false
		}
		return fieldCompletionItems(t), true
	}
	return nil, false
}
