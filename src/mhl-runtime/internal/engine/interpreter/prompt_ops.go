package interpreter

import (
	"fmt"

	"github.com/yanjustino/mhl-runtime/internal/features/prompt"
	"github.com/yanjustino/mhl-runtime/internal/lang/ast"
)

// resolvePromptArgument resolves call's `prompt:` argument. It accepts a
// plain string literal — interpolated for "${...}" spans against ctx's
// variable environment (e.g. `prompt: "Corrija: ${last_error}"`, matching
// language-design.md §8's RefinementLoop example) — or a reference to a
// declared `prompt Name(...) { "..." }` template (§2 "Prompts Dinâmicos"),
// e.g. `prompt: SecurityAuditPrompt(file_path: "x")`, which is rendered by
// substituting its ${param} placeholders with the call's named arguments
// (that template mechanism is intentionally left as its own path — not
// unified with the general interpolator). A template argument may itself be
// a reference to another declared prompt (e.g.
// `SystemPrompt(nested: SecurityAuditPrompt(file_path: "x"))`), which is
// rendered first and substituted in as a plain string — see
// resolvePromptCallArgs. Since a nested reference is always a literal call
// expression written in source (a tree, not a graph with back-edges), this
// recursion is bounded by the source file's own nesting depth and cannot
// cycle — no cycle guard is needed.
// ok is false (with a nil error) when the prompt argument is absent or
// neither shape matches, so callers keep producing the existing "requires a
// non-empty prompt" error; a non-nil error means the prompt reference
// itself is invalid (unknown prompt, bad arguments, missing/undeclared
// parameters, or a bad "${...}" expression).
func resolvePromptArgument(ctx *evalCtx, call *ast.Call) (string, bool, error) {
	for _, arg := range call.Args {
		if arg.Name != "prompt" {
			continue
		}
		if s, ok := ast.StringValue(arg.Value); ok {
			rendered, err := interpolate(ctx, s)
			if err != nil {
				return "", false, err
			}
			return rendered, true, nil
		}
		promptCall, name, ok := promptRefCall(arg.Value)
		if !ok {
			return "", false, nil
		}
		rendered, err := renderPromptCall(ctx.prog, name, promptCall)
		if err != nil {
			return "", false, err
		}
		return rendered, true, nil
	}
	return "", false, nil
}

// renderPromptCall resolves and renders the `name(...)` prompt reference in
// call, recursively rendering any nested prompt-reference argument first.
func renderPromptCall(prog *ast.Program, name string, call *ast.Call) (string, error) {
	decl, ok := findPrompt(prog, name)
	if !ok {
		return "", fmt.Errorf("prompt %q not found", name)
	}
	callArgs, err := resolvePromptCallArgs(prog, call)
	if err != nil {
		return "", fmt.Errorf("prompt %q: %w", name, err)
	}
	return prompt.Render(decl, callArgs)
}

// resolvePromptCallArgs collects call's arguments into a name->value map.
// Every argument must be named; its value must be either a string literal
// or a reference to another declared `prompt Name(...) { "..." }` template,
// which is rendered (via renderPromptCall) and substituted in as a plain
// string.
func resolvePromptCallArgs(prog *ast.Program, call *ast.Call) (map[string]string, error) {
	args := make(map[string]string, len(call.Args))
	for _, arg := range call.Args {
		if arg.Name == "" {
			return nil, fmt.Errorf("arguments must be named")
		}
		if s, ok := ast.StringValue(arg.Value); ok {
			args[arg.Name] = s
			continue
		}
		nestedCall, nestedName, ok := promptRefCall(arg.Value)
		if !ok {
			return nil, fmt.Errorf("argument %q must be a string literal or a prompt reference", arg.Name)
		}
		rendered, err := renderPromptCall(prog, nestedName, nestedCall)
		if err != nil {
			return nil, err
		}
		args[arg.Name] = rendered
	}
	return args, nil
}

// promptRefCall recognizes a bare `Name(...)` call expression, e.g. the
// `SecurityAuditPrompt(file_path: "x")` in `prompt: SecurityAuditPrompt(...)`.
func promptRefCall(expr *ast.Expr) (*ast.Call, string, bool) {
	postfix := ast.BarePostfix(expr)
	if postfix == nil || postfix.Primary == nil || postfix.Primary.Ident == "" || len(postfix.Ops) != 1 {
		return nil, "", false
	}
	if postfix.Ops[0].Call != nil {
		return postfix.Ops[0].Call, postfix.Primary.Ident, true
	}
	return nil, "", false
}

func findPrompt(prog *ast.Program, name string) (*ast.Prompt, bool) {
	for _, decl := range prog.Decls {
		if decl.Prompt != nil && decl.Prompt.Name == name {
			return decl.Prompt, true
		}
	}
	return nil, false
}
