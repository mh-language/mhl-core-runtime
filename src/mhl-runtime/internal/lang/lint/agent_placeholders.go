package lint

import (
	"fmt"
	"strings"

	"github.com/alecthomas/participle/v2/lexer"

	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
)

// checkAgentPlaceholders flags a "${...}" span in an agent's literal
// `command`/`args` values that will never be substituted at run time
// (MHL-Melhorias.md #19) — interpreter/agent.go's injectPromptArg/
// injectSchemaArg only ever recognize a whole `args:` element that is
// exactly "${prompt}" or exactly "${schema}"; `command:` is never
// substituted at all, regardless of content. Anything else — a typo'd
// name, "${prompt}" embedded inside a longer string instead of standing
// alone as its own element, or any "${...}" in `command:` — silently
// reaches the real subprocess argv as literal text, with no error from
// `mhl lint` or `mhl run` today; the only way to notice was inspecting the
// real argv the child process received.
//
// A non-literal `command`/`args` value (an expression, a variable, a tool
// call) is left alone — nothing to statically read a "${...}" span out of,
// and command/args accepting an arbitrary expression (MHL-Melhorias.md #1)
// is itself the fix this finding steers a caller toward: resolve the value
// as an expression element instead of a string placeholder.
func checkAgentPlaceholders(file, agentName string, p *ast.Property) []Finding {
	var findings []Finding
	switch p.Name {
	case "command":
		if s, ok := ast.StringValue(p.Value); ok {
			for _, span := range findPlaceholderSpans(s) {
				findings = append(findings, agentPlaceholderFinding(file, agentName, "command", span, p.Pos))
			}
		}
	case "args":
		arr := ast.BareArray(p.Value)
		if arr == nil {
			return nil
		}
		for _, item := range arr.Items {
			s, ok := ast.StringValue(item)
			if !ok || s == "${prompt}" || s == "${schema}" {
				continue
			}
			for _, span := range findPlaceholderSpans(s) {
				findings = append(findings, agentPlaceholderFinding(file, agentName, "args", span, p.Pos))
			}
		}
	}
	return findings
}

func agentPlaceholderFinding(file, agentName, prop, span string, pos lexer.Position) Finding {
	return Finding{
		File: file, Line: pos.Line, Column: pos.Column,
		Message: fmt.Sprintf(
			"agent %q: %s contains %s, which is never substituted — args: only recognizes a whole element exactly equal to \"${prompt}\" or \"${schema}\"; command: recognizes neither. Pass the value as an expression element instead (a variable, env(...), a tool call — command/args accept any expression), not a \"${...}\" placeholder.",
			agentName, prop, span),
	}
}

// findPlaceholderSpans returns each "${...}" span found in s verbatim
// (braces included), brace-depth aware so "${ {a: 1} }" still closes at
// its matching "}" — this package's own copy of the same scan
// interpreter.interpolate (matchInterpolationSpan) performs on a real
// interpolated string; lint cannot import the interpreter package (see
// this package's own architecture note in lint.go), and unlike that
// scan this one never evaluates anything, purely a static string read.
// An unterminated "${" is left alone — that is a parse-time concern for
// wherever this string is actually interpolated, not this check's.
func findPlaceholderSpans(s string) []string {
	var spans []string
	i := 0
	for i < len(s) {
		start := strings.Index(s[i:], "${")
		if start == -1 {
			return spans
		}
		start += i
		depth := 1
		end := -1
		for j := start + 2; j < len(s); j++ {
			switch s[j] {
			case '{':
				depth++
			case '}':
				depth--
				if depth == 0 {
					end = j
				}
			}
			if end != -1 {
				break
			}
		}
		if end == -1 {
			return spans
		}
		spans = append(spans, s[start:end+1])
		i = end + 1
	}
	return spans
}
