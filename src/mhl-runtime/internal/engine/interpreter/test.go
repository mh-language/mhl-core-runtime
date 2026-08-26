package interpreter

import (
	"fmt"
	"io"
	"reflect"
	"strings"

	"github.com/mh-language/mhl-core-runtime/internal/features/memory"
	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
)

// assertionNames is the set of builtin assertion functions a describe
// block's body recognizes as an assertion (see assertionCall and
// execExprStatement, exec.go) rather than an ordinary call, which would
// otherwise fail as "undefined variable" since none of these are a
// declared tool, agent, or var.
var assertionNames = map[string]bool{
	"are_equal":             true,
	"are_not_equal":         true,
	"not_equal":             true,
	"is_true":               true,
	"is_false":              true,
	"is_null":               true,
	"not_null":              true,
	"greater_than":          true,
	"less_than":             true,
	"greater_than_or_equal": true,
	"less_than_or_equal":    true,
	"includes":              true,
	"incomplete":            true,
}

// assertionCall recognizes expr as a bare call to a builtin assertion
// function — `name(args...)` with no operators, member access, or index
// trailers applied — returning its name and Call node. Anything else
// (log(...), a tool/agent call, a plain boolean expression, ...) reports
// ok=false and is left for execExprStatement to evaluate normally.
func assertionCall(expr *ast.Expr) (name string, call *ast.Call, ok bool) {
	pf := ast.BarePostfix(expr)
	if pf == nil || pf.Primary == nil || pf.Primary.Ident == "" || len(pf.Ops) != 1 {
		return "", nil, false
	}
	if pf.Ops[0].Call == nil || !assertionNames[pf.Primary.Ident] {
		return "", nil, false
	}
	return pf.Primary.Ident, pf.Ops[0].Call, true
}

// AssertionResult is the outcome of one assertion call encountered while
// running a describe block's body: exactly one of Passed or Skipped is
// true (an `incomplete(...)` assertion is never evaluated for pass/fail,
// only recorded as pending).
type AssertionResult struct {
	Call    string // e.g. `are_equal(1, 2)`, rendered from name and the evaluated args
	Passed  bool
	Skipped bool
	Detail  string // failure reason, or the incomplete() message
}

// DescribeResult is one `describe name { ... }` block's assertion results,
// in the order they actually executed — which, since a describe body can
// contain if/while/for-in, may skip some statically-present assertions
// (an untaken if branch) or run one more than once (a while/for-in loop).
type DescribeResult struct {
	Name       string
	Assertions []AssertionResult
}

// TestResult is one `test Name { ... }` block's full run: every describe
// block it declares, in source order.
type TestResult struct {
	Name      string
	Describes []DescribeResult
}

// Counts tallies passed/failed/skipped assertions across every describe
// block in the result.
func (r *TestResult) Counts() (passed, failed, skipped int) {
	for _, d := range r.Describes {
		for _, a := range d.Assertions {
			switch {
			case a.Skipped:
				skipped++
			case a.Passed:
				passed++
			default:
				failed++
			}
		}
	}
	return passed, failed, skipped
}

// Failed reports whether any assertion in the result failed (incomplete
// assertions do not count as failures).
func (r *TestResult) Failed() bool {
	_, failed, _ := r.Counts()
	return failed > 0
}

// RunTests executes every `test { ... }` block declared at the top level of
// prog, in source order, running each describe block's body against a
// fresh, describe-scoped environment — nothing declared in one describe
// block is visible to another, matching how RunStep scopes a pipeline
// step's Env. It stops and returns an error only on a genuine runtime
// error (e.g. an undefined variable, or a condition that isn't a bool),
// not on an assertion that merely fails — a failed assertion is recorded
// in the result, exactly like a passed one.
func RunTests(prog *ast.Program, file string, out io.Writer, store *memory.KVStore, jsonStore *memory.JSONStore) ([]*TestResult, error) {
	var results []*TestResult
	for _, decl := range prog.Decls {
		if decl.Test == nil {
			continue
		}
		result, err := runTest(prog, decl.Test, file, out, store, jsonStore)
		if err != nil {
			return results, err
		}
		results = append(results, result)
	}
	return results, nil
}

func runTest(prog *ast.Program, test *ast.Test, file string, out io.Writer, store *memory.KVStore, jsonStore *memory.JSONStore) (*TestResult, error) {
	result := &TestResult{Name: test.Name}
	for _, describe := range test.Describes {
		var assertions []AssertionResult
		ctx := &evalCtx{prog: prog, store: store, jsonStore: jsonStore, out: out, env: Env{}, file: file, assertions: &assertions}
		if err := execBlock(ctx, describe.Body); err != nil {
			return result, err
		}
		result.Describes = append(result.Describes, DescribeResult{Name: describe.Name, Assertions: assertions})
	}
	return result, nil
}

// evalAssertionCall evaluates a recognized assertion call's arguments and
// dispatches to runAssertion (or records incomplete()'s message). Called
// from execExprStatement (exec.go) once assertionCall has identified expr
// as one of these builtins.
func evalAssertionCall(ctx *evalCtx, name string, call *ast.Call) (AssertionResult, error) {
	args := make([]any, len(call.Args))
	for i, arg := range call.Args {
		v, err := evalExpr(ctx, arg.Value)
		if err != nil {
			return AssertionResult{}, err
		}
		args[i] = v
	}
	display := fmt.Sprintf("%s(%s)", name, formatAssertionArgs(args))

	if name == "incomplete" {
		msg := ""
		if len(args) > 0 {
			msg = formatValue(args[0])
		}
		return AssertionResult{Call: display, Skipped: true, Detail: msg}, nil
	}

	passed, detail, err := runAssertion(name, args)
	if err != nil {
		return AssertionResult{}, err
	}
	return AssertionResult{Call: display, Passed: passed, Detail: detail}, nil
}

// runAssertion evaluates one builtin assertion function by name against its
// already-evaluated args, returning whether it passed and, on failure, a
// human-readable reason. An unrecognized name or wrong argument
// count/type is a genuine error (like calling an undefined function
// elsewhere in the language), not a failed assertion.
func runAssertion(name string, args []any) (passed bool, detail string, err error) {
	switch name {
	case "are_equal":
		if err := requireArgs(name, args, 2); err != nil {
			return false, "", err
		}
		eq := reflect.DeepEqual(args[0], args[1])
		return eq, failIf(!eq, "expected %s to equal %s", formatValue(args[0]), formatValue(args[1])), nil

	case "are_not_equal", "not_equal":
		if err := requireArgs(name, args, 2); err != nil {
			return false, "", err
		}
		eq := reflect.DeepEqual(args[0], args[1])
		return !eq, failIf(eq, "expected %s to not equal %s", formatValue(args[0]), formatValue(args[1])), nil

	case "is_true":
		if err := requireArgs(name, args, 1); err != nil {
			return false, "", err
		}
		b, ok := args[0].(bool)
		if !ok {
			return false, "", fmt.Errorf("is_true() requires a bool argument, got %s", typeName(args[0]))
		}
		return b, failIf(!b, "expected true, got false"), nil

	case "is_false":
		if err := requireArgs(name, args, 1); err != nil {
			return false, "", err
		}
		b, ok := args[0].(bool)
		if !ok {
			return false, "", fmt.Errorf("is_false() requires a bool argument, got %s", typeName(args[0]))
		}
		return !b, failIf(b, "expected false, got true"), nil

	case "is_null":
		if err := requireArgs(name, args, 1); err != nil {
			return false, "", err
		}
		isNull := args[0] == nil
		return isNull, failIf(!isNull, "expected null, got %s", formatValue(args[0])), nil

	case "not_null":
		if err := requireArgs(name, args, 1); err != nil {
			return false, "", err
		}
		isNull := args[0] == nil
		return !isNull, failIf(isNull, "expected a non-null value"), nil

	case "greater_than", "less_than", "greater_than_or_equal", "less_than_or_equal":
		if err := requireArgs(name, args, 2); err != nil {
			return false, "", err
		}
		lf, ok1 := args[0].(float64)
		rf, ok2 := args[1].(float64)
		if !ok1 || !ok2 {
			return false, "", fmt.Errorf("%s() requires number arguments, got %s and %s", name, typeName(args[0]), typeName(args[1]))
		}
		var b bool
		var op string
		switch name {
		case "greater_than":
			b, op = lf > rf, "greater than"
		case "less_than":
			b, op = lf < rf, "less than"
		case "greater_than_or_equal":
			b, op = lf >= rf, "greater than or equal to"
		case "less_than_or_equal":
			b, op = lf <= rf, "less than or equal to"
		}
		return b, failIf(!b, "expected %s to be %s %s", formatValue(args[0]), op, formatValue(args[1])), nil

	case "includes":
		if err := requireArgs(name, args, 2); err != nil {
			return false, "", err
		}
		arr, ok := args[0].([]any)
		if !ok {
			return false, "", fmt.Errorf("includes() requires an array as its first argument, got %s", typeName(args[0]))
		}
		found := false
		for _, item := range arr {
			if reflect.DeepEqual(item, args[1]) {
				found = true
				break
			}
		}
		return found, failIf(!found, "expected %s to include %s", formatValue(args[0]), formatValue(args[1])), nil

	default:
		return false, "", fmt.Errorf("unknown assertion %q", name)
	}
}

// requireArgs returns a descriptive error when args doesn't have exactly n
// elements, the same "wrong argument count" failure mode a misused builtin
// elsewhere in the language reports.
func requireArgs(name string, args []any, n int) error {
	if len(args) != n {
		return fmt.Errorf("%s() requires exactly %d argument(s), got %d", name, n, len(args))
	}
	return nil
}

// failIf returns the formatted reason when cond is true, or "" when the
// assertion passed — AssertionResult.Detail is only meant to explain a
// failure.
func failIf(cond bool, format string, args ...any) string {
	if !cond {
		return ""
	}
	return fmt.Sprintf(format, args...)
}

// formatAssertionArgs renders assertion call arguments for display,
// quoting strings (unlike formatValue, which is also used for raw
// log(...)/interpolation output where a bare string is what's wanted).
func formatAssertionArgs(args []any) string {
	parts := make([]string, len(args))
	for i, a := range args {
		if s, ok := a.(string); ok {
			parts[i] = fmt.Sprintf("%q", s)
			continue
		}
		parts[i] = formatValue(a)
	}
	return strings.Join(parts, ", ")
}
