package skills

import "github.com/yanjustino/mhl-runtime/internal/lang/ast"

// refListValue flattens an array-literal of tool/mcp_server references (e.g.
// `[execution.read_file, execution.git_diff]` or `[PostgresDB]`) into their
// dotted-name string form. Entries that are not simple references are skipped.
func refListValue(e *ast.Expr) []string {
	postfix := ast.BarePostfix(e)
	if postfix == nil || len(postfix.Ops) != 0 || postfix.Primary == nil || postfix.Primary.Array == nil {
		return nil
	}
	var out []string
	for _, item := range postfix.Primary.Array.Items {
		if name, ok := refName(item); ok {
			out = append(out, name)
		}
	}
	return out
}

// refName renders a simple dotted reference expression (an identifier followed
// only by `.member` accesses, e.g. `execution.read_file`) into its string
// form. It returns false for anything more complex.
func refName(e *ast.Expr) (string, bool) {
	pf := ast.BarePostfix(e)
	if pf == nil || pf.Primary == nil {
		return "", false
	}
	if pf.Primary.Ident == "" {
		return "", false
	}
	name := pf.Primary.Ident
	for _, t := range pf.Ops {
		if t.Member == "" {
			// A call or other trailer means this is not a plain reference.
			return "", false
		}
		name += "." + t.Member
	}
	return name, true
}
