// Package prompt implements §2 "Prompts Dinâmicos" of language-design.md: a
// `prompt Name(param: type, ...) { "template with ${param}" }` declaration
// is a reusable text template whose ${param} placeholders are substituted
// with caller-supplied values.
package prompt

import (
	"fmt"
	"regexp"

	"github.com/yanjustino/mhl-runtime/internal/lang/ast"
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
func Render(decl *ast.Prompt, args map[string]string) (string, error) {
	body, ok := ast.StringValue(decl.Body)
	if !ok {
		return "", fmt.Errorf("prompt %q has no text body", decl.Name)
	}

	declared := make(map[string]bool, len(decl.Params))
	for _, p := range decl.Params {
		declared[p.Name] = true
		if _, ok := args[p.Name]; !ok {
			return "", fmt.Errorf("prompt %q: missing value for parameter %q", decl.Name, p.Name)
		}
	}
	for name := range args {
		if !declared[name] {
			return "", fmt.Errorf("prompt %q: unexpected argument %q", decl.Name, name)
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
			err = fmt.Errorf("prompt %q: template references undeclared parameter %q", decl.Name, name)
			return match
		}
		return args[name]
	})
	if err != nil {
		return "", err
	}
	return rendered, nil
}
