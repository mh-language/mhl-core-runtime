// Package prompt implements §2 "Prompts Dinâmicos" of language-design.md: a
// `prompt Name(param: type, ...) { "template with ${param}" }` declaration
// is a reusable text template whose ${param} placeholders are substituted
// with caller-supplied values.
package prompt

import (
	"fmt"
	"regexp"

	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
)

// placeholderPattern's optional leading backslash marks an escaped
// placeholder (`\${name}`) — text pulled in from an external file (a
// `prompt ... from "file.md"` body, ast.NewMultilineStringExpr) is far more
// likely than a hand-written template to contain incidental ${...} that
// isn't an mhl parameter at all (shell/env-var examples, JSON, ...), and
// Render's fail-closed validation below would otherwise reject it outright.
// There's no way in this pattern to also get a literal backslash
// immediately before a real substitution (`\\${param}`); not needed yet.
var placeholderPattern = regexp.MustCompile(`(\\)?\$\{([a-zA-Z_][a-zA-Z0-9_]*)\}`)

// Render interpolates decl's template body using args (keyed by parameter
// name) and returns the resulting text. It fails closed: every declared
// parameter must have a value in args, every key in args must be a declared
// parameter, and every unescaped ${...} placeholder found in the body must
// name a declared parameter — any mismatch is reported as an error rather
// than silently producing partial or garbled output. A `\${name}` escape
// bypasses that validation entirely and renders as the literal `${name}`,
// regardless of whether "name" is a declared parameter.
//
// Render always uses the default body; RenderFor picks a `for` variant.
func Render(decl *ast.Prompt, args map[string]string) (string, error) {
	return RenderBody(decl, nil, args)
}

// RenderFor renders the body target selects (ast.Prompt.Variant: exact
// pattern, then longest prefix, then the default).
func RenderFor(decl *ast.Prompt, target string, args map[string]string) (string, error) {
	return RenderBody(decl, decl.Variant(target), args)
}

// RenderBody renders variant's body (the default's when variant is nil) with
// Render's fail-closed rules. Every arm shares decl's parameters; an error
// names the arm it came from.
func RenderBody(decl *ast.Prompt, variant *ast.PromptVariant, args map[string]string) (string, error) {
	label := armLabel(decl, variant)
	bodyExpr := decl.Body
	if variant != nil {
		bodyExpr = variant.Body
	}
	body, ok := ast.StringValue(bodyExpr)
	if !ok {
		return "", fmt.Errorf("%s has no text body", label)
	}

	declared := make(map[string]bool, len(decl.Params))
	for _, p := range decl.Params {
		declared[p.Name] = true
		if _, ok := args[p.Name]; !ok {
			return "", fmt.Errorf("%s: missing value for parameter %q", label, p.Name)
		}
	}
	for name := range args {
		if !declared[name] {
			return "", fmt.Errorf("%s: unexpected argument %q", label, name)
		}
	}

	var err error
	rendered := placeholderPattern.ReplaceAllStringFunc(body, func(match string) string {
		if err != nil {
			return match
		}
		groups := placeholderPattern.FindStringSubmatch(match)
		escaped, name := groups[1] != "", groups[2]
		if escaped {
			return "${" + name + "}"
		}
		if !declared[name] {
			err = fmt.Errorf("%s: template references undeclared parameter %q", label, name)
			return match
		}
		return args[name]
	})
	if err != nil {
		return "", err
	}
	return rendered, nil
}

// CheckBody reports the first placeholder in variant's body (the default's
// when nil) that names no declared parameter, without needing argument
// values — lint's declaration-level check of every arm.
func CheckBody(decl *ast.Prompt, variant *ast.PromptVariant) error {
	bodyExpr := decl.Body
	if variant != nil {
		bodyExpr = variant.Body
	}
	body, ok := ast.StringValue(bodyExpr)
	if !ok {
		return fmt.Errorf("%s body must be a string", armLabel(decl, variant))
	}
	declared := make(map[string]bool, len(decl.Params))
	for _, p := range decl.Params {
		declared[p.Name] = true
	}
	for _, groups := range placeholderPattern.FindAllStringSubmatch(body, -1) {
		if groups[1] == "" && !declared[groups[2]] {
			return fmt.Errorf("%s: template references undeclared parameter %q", armLabel(decl, variant), groups[2])
		}
	}
	return nil
}

func armLabel(decl *ast.Prompt, variant *ast.PromptVariant) string {
	if variant == nil {
		return fmt.Sprintf("prompt %q", decl.Name)
	}
	return fmt.Sprintf("prompt %q (for %q)", decl.Name, variant.Pattern)
}
