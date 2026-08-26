package interpreter

import (
	"fmt"

	"github.com/mh-language/mhl-core-runtime/internal/features/prompt"
	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
)

// resolvePromptArgument resolves call's `prompt:` argument. A bare
// `Name(...)` call shape (promptRefCall) is treated as a reference to a
// declared `prompt Name(...) { "..." }` template (language-design.md §2
// "Prompts Dinâmicos") and rendered via renderPromptCallDynamic — reported
// as "prompt %q not found" rather than falling through to a generic
// "undefined variable" error, since a call-shaped value in this position is
// virtually never meant to invoke a plain closure. Anything else (a string
// literal — interpolated for "${...}" spans exactly like every other string
// literal in the language, e.g. `prompt: "Corrija: ${last_error}"` — a
// variable holding a previously-rendered prompt, string concatenation, ...)
// is evaluated as an ordinary expression and must produce a string.
// ok is false (with a nil error) when the prompt argument is absent, so
// callers keep producing the existing "requires a non-empty prompt" error.
func resolvePromptArgument(ctx *evalCtx, call *ast.Call, depth int) (string, bool, error) {
	for _, arg := range call.Args {
		if arg.Name != "prompt" {
			continue
		}
		if promptCall, name, ok := promptRefCall(arg.Value); ok {
			rendered, err := renderPromptCallDynamic(ctx, name, promptCall, depth)
			if err != nil {
				return "", false, err
			}
			return rendered, true, nil
		}
		v, err := evalExprAt(ctx, arg.Value, depth)
		if err != nil {
			return "", false, err
		}
		s, ok := v.(string)
		if !ok {
			return "", false, fmt.Errorf("prompt must be a string, got %s", typeName(v))
		}
		return s, true, nil
	}
	return "", false, nil
}

// renderPromptCallDynamic renders the declared prompt template named name
// using call's arguments evaluated as ordinary expressions against ctx — a
// variable, string concatenation, another prompt call (which renders
// through this same function via evalPostfix's dispatch below, so
// `Greeting(role: Role(title: "security"), ...)` composes prompts out of
// smaller ones with no special-cased recursion needed here), or a plain
// literal, unlike the old restriction to a literal string token or a bare
// nested prompt reference. Every argument must still be named and every
// evaluated value must still be a string — prompt.Render only ever
// substitutes text, never any other value shape.
func renderPromptCallDynamic(ctx *evalCtx, name string, call *ast.Call, depth int) (string, error) {
	decl, ok := findPrompt(ctx.prog, name)
	if !ok {
		return "", fmt.Errorf("prompt %q not found", name)
	}
	args := make(map[string]string, len(call.Args))
	for _, arg := range call.Args {
		if arg.Name == "" {
			return "", fmt.Errorf("prompt %q: arguments must be named", name)
		}
		v, err := evalExprAt(ctx, arg.Value, depth)
		if err != nil {
			return "", fmt.Errorf("prompt %q: %w", name, err)
		}
		s, ok := v.(string)
		if !ok {
			return "", fmt.Errorf("prompt %q: argument %q must be a string, got %s", name, arg.Name, typeName(v))
		}
		args[arg.Name] = s
	}
	return prompt.Render(decl, args)
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
	name = resolveName(prog, name)
	for _, decl := range prog.Decls {
		if decl.Prompt != nil && decl.Prompt.Name == name {
			return decl.Prompt, true
		}
	}
	return nil, false
}
