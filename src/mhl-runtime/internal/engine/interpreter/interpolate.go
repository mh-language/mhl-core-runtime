package interpreter

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/yanjustino/mhl-runtime/internal/lang/parser"
)

// interpolate scans s for "${...}" spans, evaluates each span's content as
// a full MHL expression against ctx (so it can reference variables, and —
// through the same evaluator — memory/agent calls), and splices the
// formatted result back into the string. A string with no "${" is returned
// unchanged, so every existing plain string literal behaves exactly as
// before; this is what lets `prompt: "Corrija: ${last_error}"` and
// `log("attempt=${attempt}")` work.
func interpolate(ctx *evalCtx, s string) (string, error) {
	if !strings.Contains(s, "${") {
		return s, nil
	}
	var b strings.Builder
	i := 0
	for i < len(s) {
		start := strings.Index(s[i:], "${")
		if start == -1 {
			b.WriteString(s[i:])
			break
		}
		start += i
		b.WriteString(s[i:start])
		end, ok := matchInterpolationSpan(s, start)
		if !ok {
			return "", fmt.Errorf("unterminated \"${\" in string")
		}
		inner := s[start+2 : end]
		expr, err := parser.ParseExpr(inner)
		if err != nil {
			return "", fmt.Errorf("invalid expression in \"${%s}\": %w", inner, err)
		}
		v, err := evalExpr(ctx, expr)
		if err != nil {
			return "", fmt.Errorf("evaluating \"${%s}\": %w", inner, err)
		}
		b.WriteString(formatValue(v))
		i = end + 1
	}
	return b.String(), nil
}

// matchInterpolationSpan finds the "}" that closes the "${" starting at
// start, counting brace depth so a nested object literal like
// "${ {a: 1} }" closes at the right place instead of its first "}".
func matchInterpolationSpan(s string, start int) (end int, ok bool) {
	depth := 1
	for i := start + 2; i < len(s); i++ {
		switch s[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i, true
			}
		}
	}
	return 0, false
}

// formatValue renders an evaluated value for log(...) output and string
// interpolation: strings pass through raw, numbers drop trailing zeros,
// and structured values (array/object) render as compact JSON rather than
// Go's map[...] syntax.
func formatValue(v any) string {
	switch t := v.(type) {
	case nil:
		return "null"
	case string:
		return t
	case *Closure:
		// A closure holds an AST + captured Env, not JSON-shaped data —
		// json.Marshal would either choke on it or produce noise, so it
		// gets a fixed, deliberately uninformative representation instead.
		return "<function>"
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case bool:
		if t {
			return "true"
		}
		return "false"
	default:
		raw, err := json.Marshal(t)
		if err != nil {
			return fmt.Sprintf("%v", t)
		}
		return string(raw)
	}
}
