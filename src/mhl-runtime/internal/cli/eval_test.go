package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/cli"
)

// run is a small helper shared by this file's tests: write src as main.mh
// in a fresh temp dir and run it, returning stdout and the run error.
func run(t *testing.T, src string) (string, error) {
	t.Helper()
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	if err := os.WriteFile(main, []byte(src), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	var buf bytes.Buffer
	err := cli.Run([]string{"run", main}, &buf)
	return buf.String(), err
}

func wrapStep(body string) string {
	return "pipeline P {\n    step S {\n" + body + "\n    }\n}\n"
}

// --- operators ---------------------------------------------------------

func TestEvalArithmeticOperators(t *testing.T) {
	out, err := run(t, wrapStep(`
        log(1 + 2)
        log(5 - 3)
        log(4 * 2)
        log(9 / 2)
        log("a" + "b")
    `))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, want := range []string{"3\n", "2\n", "8\n", "4.5\n", "ab\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q, got: %s", want, out)
		}
	}
}

func TestEvalLogLevels(t *testing.T) {
	out, err := run(t, wrapStep(`
        log("no level")
        log.info("info msg")
        log.warn("warn msg")
        log.error("error msg")
    `))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, want := range []string{"no level\n", "[INFO] info msg\n", "[WARN] warn msg\n", "[ERROR] error msg\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q, got: %s", want, out)
		}
	}
}

func TestEvalFailStopsRunWithError(t *testing.T) {
	_, err := run(t, wrapStep(`fail("something went fatally wrong")`))
	if err == nil || !strings.Contains(err.Error(), "something went fatally wrong") {
		t.Fatalf("expected an error containing the fail message, got: %v", err)
	}
}

func TestEvalFailStopsBeforeLaterStatements(t *testing.T) {
	out, err := run(t, wrapStep(`
        fail("stop here")
        log("should never run")
    `))
	if err == nil {
		t.Fatal("expected fail to stop the step with an error")
	}
	if strings.Contains(out, "should never run") {
		t.Errorf("statement after fail(...) must not execute: %s", out)
	}
}

func TestEvalFailIsCatchableByTry(t *testing.T) {
	out, err := run(t, wrapStep(`
        try {
            fail("boom")
        } catch (e) {
            log("caught: ${e}")
        }
    `))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "caught: boom") {
		t.Errorf("expected fail(...) to be catchable, got: %s", out)
	}
}

func TestEvalDivisionByZero(t *testing.T) {
	_, err := run(t, wrapStep(`log(1 / 0)`))
	if err == nil || !strings.Contains(err.Error(), "division by zero") {
		t.Fatalf("expected division by zero error, got: %v", err)
	}
}

func TestEvalComparisonAndLogicalOperators(t *testing.T) {
	out, err := run(t, wrapStep(`
        log(1 < 2)
        log(2 <= 2)
        log(3 > 2)
        log(1 == 1)
        log(1 != 2)
        log(true && false)
        log(true || false)
        log(!false)
    `))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, want := range []string{"true\n", "false\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q, got: %s", want, out)
		}
	}
}

func TestEvalTypeMismatchErrors(t *testing.T) {
	cases := []struct {
		name string
		expr string
		want string
	}{
		{"add-string-number", `log("a" + 1)`, "requires both operands to be strings"},
		{"cmp-strings", `log("a" < "b")`, `requires number operands`},
		{"and-non-bool", `log(1 && true)`, `requires bool operands`},
		{"unary-not-non-bool", `log(!1)`, `requires a bool operand`},
		{"unary-minus-non-number", `log(-"a")`, `requires a number operand`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := run(t, wrapStep(tc.expr))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected error containing %q, got: %v", tc.want, err)
			}
		})
	}
}

// --- var / assign --------------------------------------------------------

func TestEvalVarThenLog(t *testing.T) {
	out, err := run(t, wrapStep(`
        var x = 41
        log(x)
    `))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "41\n") {
		t.Errorf("unexpected output: %s", out)
	}
}

func TestEvalAssignReassignsDeclaredVar(t *testing.T) {
	out, err := run(t, wrapStep(`
        var attempt = 0
        attempt = attempt + 1
        attempt = attempt + 1
        log(attempt)
    `))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "2\n") {
		t.Errorf("unexpected output: %s", out)
	}
}

func TestEvalAssignToUndeclaredVarErrors(t *testing.T) {
	_, err := run(t, wrapStep(`y = 1`))
	if err == nil || !strings.Contains(err.Error(), `undefined variable "y"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEvalCompoundAddAssign(t *testing.T) {
	out, err := run(t, wrapStep(`
        var errors = []
        errors += ["first"]
        errors += ["second"]
        var more = ["third", "fourth"]
        errors += more
        log(errors.join(" | "))

        var count = 0
        count += 1
        count += 4
        log(count)

        var msg = "a"
        msg += "b"
        log(msg)

        var grid = [[1], [2]]
        grid[0] += [9]
        log(grid[0].join(","))
    `))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, want := range []string{"first | second | third | fourth\n", "5\n", "ab\n", "1,9\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in output: %s", want, out)
		}
	}
}

func TestEvalCompoundAddAssignToUndeclaredVarErrors(t *testing.T) {
	_, err := run(t, wrapStep(`y += 1`))
	if err == nil || !strings.Contains(err.Error(), `undefined variable "y"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEvalCompoundAddAssignTypeMismatchErrors(t *testing.T) {
	_, err := run(t, wrapStep(`
        var msg = "x"
        msg += 3
    `))
	if err == nil || !strings.Contains(err.Error(), "requires both operands to be strings") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEvalAssignToMemberTargetErrors(t *testing.T) {
	_, err := run(t, wrapStep(`
        var obj = {a: 1}
        obj.a = 2
    `))
	if err == nil || !strings.Contains(err.Error(), "assignment target must be a plain variable") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEvalUndefinedVariableErrors(t *testing.T) {
	_, err := run(t, wrapStep(`log(ghost)`))
	if err == nil || !strings.Contains(err.Error(), `undefined variable "ghost"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEvalVariableScopeDoesNotSurviveAcrossSteps(t *testing.T) {
	_, err := run(t, `
pipeline P {
    step S1 {
        var shared = 1
    }
    step S2 {
        log(shared)
    }
}
`)
	if err == nil || !strings.Contains(err.Error(), `undefined variable "shared"`) {
		t.Fatalf("expected step S2 to not see S1's variable, got: %v", err)
	}
}

// --- if/else --------------------------------------------------------------

func TestEvalIfTrueBranch(t *testing.T) {
	out, err := run(t, wrapStep(`
        if (1 < 2) {
            log("then")
        } else {
            log("else")
        }
    `))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "then\n") || strings.Contains(out, "else\n") {
		t.Errorf("unexpected output: %s", out)
	}
}

func TestEvalIfFalseBranch(t *testing.T) {
	out, err := run(t, wrapStep(`
        if (1 > 2) {
            log("then")
        } else {
            log("else")
        }
    `))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "else\n") || strings.Contains(out, "then\n") {
		t.Errorf("unexpected output: %s", out)
	}
}

func TestEvalIfConditionMustBeBool(t *testing.T) {
	_, err := run(t, wrapStep(`if (1) { log("x") }`))
	if err == nil || !strings.Contains(err.Error(), "if condition must be a bool") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// --- if-expression (ternary form) --------------------------------------

func TestEvalIfExprTrueBranch(t *testing.T) {
	out, err := run(t, wrapStep(`
        var result = if (true) "yes" else "no"
        log(result)
    `))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "yes\n") {
		t.Errorf("unexpected output: %s", out)
	}
}

func TestEvalIfExprFalseBranch(t *testing.T) {
	out, err := run(t, wrapStep(`
        var result = if (false) "yes" else "no"
        log(result)
    `))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "no\n") {
		t.Errorf("unexpected output: %s", out)
	}
}

// TestEvalIfExprAsCallArgument confirms it can appear anywhere an
// expression can, not just as a var's initializer.
func TestEvalIfExprAsCallArgument(t *testing.T) {
	out, err := run(t, wrapStep(`log(if (2 > 1) "big" else "small")`))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "big\n") {
		t.Errorf("unexpected output: %s", out)
	}
}

// TestEvalIfExprElseIfChains confirms `else if` works with no dedicated
// grammar: Else just holds another bare if-expression.
func TestEvalIfExprElseIfChains(t *testing.T) {
	out, err := run(t, wrapStep(`
        var n = 0
        var sign = if (n < 0) "negative" else if (n == 0) "zero" else "positive"
        log(sign)
    `))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "zero\n") {
		t.Errorf("unexpected output: %s", out)
	}
}

func TestEvalIfExprConditionMustBeBool(t *testing.T) {
	_, err := run(t, wrapStep(`var x = if (1) "yes" else "no"`))
	if err == nil || !strings.Contains(err.Error(), "if expression condition must be a bool") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// --- while ------------------------------------------------------------

func TestEvalWhileLoop(t *testing.T) {
	out, err := run(t, wrapStep(`
        var i = 0
        while (i < 3) {
            log(i)
            i = i + 1
        }
    `))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, want := range []string{"0\n", "1\n", "2\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q, got: %s", want, out)
		}
	}
	// Match the loop's own output sequence, not a bare "3\n" — the latter
	// also occurs in the `session: <hex>` banner whenever the random id ends
	// in 3, which made this assertion flaky.
	if strings.Contains(out, "2\n3\n") {
		t.Errorf("loop ran one iteration too many: %s", out)
	}
}

func TestEvalWhileExceedsMaxIterations(t *testing.T) {
	_, err := run(t, wrapStep(`
        var i = 0
        while (i < 1000000) {
            i = i + 1
        }
    `))
	if err == nil || !strings.Contains(err.Error(), "exceeded the maximum of 10000 iterations") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// --- try/catch/finally ------------------------------------------------

func TestEvalTryCatchCapturesErrorMessage(t *testing.T) {
	out, err := run(t, wrapStep(`
        try {
            log(1 / 0)
        } catch (e) {
            log("caught: ${e}")
        }
    `))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "caught: division by zero") {
		t.Errorf("unexpected output: %s", out)
	}
}

func TestEvalTryNoErrorSkipsCatch(t *testing.T) {
	out, err := run(t, wrapStep(`
        try {
            log("ok")
        } catch (e) {
            log("caught: ${e}")
        }
    `))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if strings.Contains(out, "caught:") {
		t.Errorf("catch block ran despite no error: %s", out)
	}
}

func TestEvalTryFinallyAlwaysRuns(t *testing.T) {
	out, err := run(t, wrapStep(`
        try {
            log("body")
        } catch (e) {
            log("caught: ${e}")
        } finally {
            log("finally")
        }
    `))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "finally\n") {
		t.Errorf("finally did not run: %s", out)
	}
}

// TestEvalTryErrorInsideCatchPropagates: catch is mandatory in the grammar
// (only its "(errName)" is optional — see ast.TryStmt), so a try can't skip
// it. An error raised inside catch itself is what propagates out of the
// whole try/catch/finally, after finally still runs.
func TestEvalTryErrorInsideCatchPropagates(t *testing.T) {
	out, err := run(t, wrapStep(`
        try {
            log(1 / 0)
        } catch (e) {
            log(1 / 0)
        } finally {
            log("finally")
        }
    `))
	if err == nil || !strings.Contains(err.Error(), "division by zero") {
		t.Fatalf("expected catch's own error to propagate, got: %v", err)
	}
	if !strings.Contains(out, "finally\n") {
		t.Errorf("finally must still run even though catch errored: %s", out)
	}
}

// TestEvalTryFinallyErrorOverridesCatch confirms a Finally error is what
// ultimately propagates, even when Body/Catch already handled their own
// error successfully.
func TestEvalTryFinallyErrorOverridesCatch(t *testing.T) {
	_, err := run(t, wrapStep(`
        try {
            log(1 / 0)
        } catch (e) {
            log("caught")
        } finally {
            log(1 / 0)
        }
    `))
	if err == nil || !strings.Contains(err.Error(), "division by zero") {
		t.Fatalf("expected finally's error to override the already-handled catch, got: %v", err)
	}
}

// --- log(...) -----------------------------------------------------------

func TestEvalLogFormatsStructuredValues(t *testing.T) {
	out, err := run(t, wrapStep(`
        log(["a", "b"])
        log({retries: 3})
        log(true, "and", 1)
    `))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, want := range []string{`["a","b"]`, `{"retries":3}`, "true and 1\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q, got: %s", want, out)
		}
	}
}

func TestEvalEnvCallReadsSetVariable(t *testing.T) {
	t.Setenv("MHL_TEST_ENV_VAR", "1")
	out, err := run(t, wrapStep(`
        log(env("MHL_TEST_ENV_VAR"))
    `))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "1\n") {
		t.Errorf("unexpected output: %s", out)
	}
}

func TestEvalEnvCallReturnsEmptyStringWhenUnset(t *testing.T) {
	out, err := run(t, wrapStep(`
        var flag = env("MHL_TEST_ENV_VAR_UNSET")
        log("[" + flag + "]")
    `))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "[]\n") {
		t.Errorf("unexpected output: %s", out)
	}
}

func TestEvalEnvCallGatesIfBranch(t *testing.T) {
	t.Setenv("MHL_TEST_ENV_VAR", "1")
	out, err := run(t, wrapStep(`
        if (env("MHL_TEST_ENV_VAR") == "1") {
            log("enabled")
        } else {
            log("disabled")
        }
    `))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "enabled\n") {
		t.Errorf("unexpected output: %s", out)
	}
}

func TestEvalEnvCallRequiresStringArgument(t *testing.T) {
	_, err := run(t, wrapStep(`
        var v = env(1)
    `))
	if err == nil {
		t.Fatal("expected an error for a non-string env() argument")
	}
	if !strings.Contains(err.Error(), "env() requires a string argument") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestEvalJSONParseObjectFieldAccess(t *testing.T) {
	out, err := run(t, wrapStep(`
        var parsed = json.parse("{\"a\":{\"b\":1}}")
        log(parsed.a.b)
    `))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "1\n") {
		t.Errorf("unexpected output: %s", out)
	}
}

func TestEvalJSONParseArrayIndexAccess(t *testing.T) {
	out, err := run(t, wrapStep(`
        var parsed = json.parse("[10,20,30]")
        log(parsed[1])
    `))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "20\n") {
		t.Errorf("unexpected output: %s", out)
	}
}

func TestEvalJSONParseStructuredOutputShape(t *testing.T) {
	out, err := run(t, wrapStep(`
        var raw = "{\"type\":\"result\",\"structured_output\":{\"message\":\"Pong! What can I help you with?\"}}"
        var parsed = json.parse(raw)
        log(parsed.structured_output.message)
    `))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "Pong! What can I help you with?\n") {
		t.Errorf("unexpected output: %s", out)
	}
}

func TestEvalJSONParseInvalidJSONReturnsError(t *testing.T) {
	_, err := run(t, wrapStep(`
        var parsed = json.parse("not json")
    `))
	if err == nil {
		t.Fatal("expected an error for invalid JSON")
	}
	if !strings.Contains(err.Error(), "json.parse") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestEvalJSONParseNonStringArgumentErrors(t *testing.T) {
	_, err := run(t, wrapStep(`
        var parsed = json.parse(1)
    `))
	if err == nil {
		t.Fatal("expected an error for a non-string argument")
	}
	if !strings.Contains(err.Error(), "json.parse requires a string") {
		t.Errorf("unexpected error: %v", err)
	}
}

// --- interpolation ------------------------------------------------------

func TestEvalInterpolationInLog(t *testing.T) {
	out, err := run(t, wrapStep(`
        var attempt = 2
        log("attempt=${attempt}")
    `))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "attempt=2\n") {
		t.Errorf("unexpected output: %s", out)
	}
}

func TestEvalInterpolationOfMemoryCall(t *testing.T) {
	out, err := run(t, `
memory session_mem {
    type: "kv"
    store: "memory"
}

`+wrapStep(`
        session_mem.set("cfg", {retries: 3})
        log("retries=${session_mem.get(\"cfg::retries\")}")
    `))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "retries=3\n") {
		t.Errorf("unexpected output: %s", out)
	}
}

func TestEvalInterpolationPlainStringUnaffected(t *testing.T) {
	out, err := run(t, wrapStep(`log("no interpolation here")`))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "no interpolation here\n") {
		t.Errorf("unexpected output: %s", out)
	}
}

func TestEvalInterpolationInvalidExpressionErrors(t *testing.T) {
	_, err := run(t, wrapStep(`log("bad=${1 +}")`))
	if err == nil {
		t.Fatal("expected an error for an invalid expression inside \"${...}\"")
	}
}

func TestEvalInterpolationUnterminatedErrors(t *testing.T) {
	_, err := run(t, wrapStep(`log("bad=${x")`))
	if err == nil || !strings.Contains(err.Error(), `unterminated`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEvalInterpolationInPromptArgument(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	src := `
agent LocalEcho {
    command: "echo"
    args: []
    trace: true
}

pipeline P {
    step S {
        var name = "World"
        var response = LocalEcho.run(prompt: "Hello, ${name}!")
    }
}
`
	if err := os.WriteFile(main, []byte(src), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	var buf bytes.Buffer
	if err := cli.Run([]string{"run", main}, &buf); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(buf.String(), "Hello, World!") {
		t.Errorf("unexpected output: %s", buf.String())
	}
}

// --- memory/agent calls used as expression values ------------------------

func TestEvalMemoryGetAsExpressionValue(t *testing.T) {
	out, err := run(t, `
memory session_mem {
    type: "kv"
    store: "memory"
}

`+wrapStep(`
        session_mem.set("cfg", {retries: 3, enabled: true})
        var r = session_mem.get("cfg::retries")
        log(r)
    `))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "3\n") {
		t.Errorf("unexpected output: %s", out)
	}
}

func TestEvalMemorySetReturnsWrittenValue(t *testing.T) {
	out, err := run(t, `
memory session_mem {
    type: "kv"
    store: "memory"
}

`+wrapStep(`
        var v = session_mem.set("attempt", 1)
        log(v)
    `))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "1\n") {
		t.Errorf("unexpected output: %s", out)
	}
}

func TestEvalAgentRunAsExpressionValue(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	src := `
agent LocalEcho {
    command: "echo"
    args: []
    trace: true
}

pipeline P {
    step S {
        var response = LocalEcho.run(prompt: "hi there")
        log("got: ${response}")
    }
}
`
	if err := os.WriteFile(main, []byte(src), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	var buf bytes.Buffer
	if err := cli.Run([]string{"run", main}, &buf); err != nil {
		t.Fatalf("run: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "agent LocalEcho response:") {
		t.Errorf("agent response tracing missing: %s", out)
	}
	if !strings.Contains(out, "got: hi there") {
		t.Errorf("expected the captured response usable afterward: %s", out)
	}
}

func TestEvalFieldAccessOnMemoryObjectValue(t *testing.T) {
	out, err := run(t, `
memory session_mem {
    type: "kv"
    store: "memory"
}

`+wrapStep(`
        session_mem.set("cfg", {retries: 3})
        var cfg = session_mem.get("cfg")
        log(cfg.retries)
    `))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "3\n") {
		t.Errorf("unexpected output: %s", out)
	}
}

func TestEvalFieldAccessOnNonObjectErrors(t *testing.T) {
	_, err := run(t, wrapStep(`
        var x = 1
        log(x.field)
    `))
	if err == nil || !strings.Contains(err.Error(), `cannot access field "field"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

// --- built-in value methods: size() / is_empty() -------------------------

func TestEvalSizeChainedOnMemoryGet(t *testing.T) {
	out, err := run(t, `
memory session_mem {
    type: "kv"
    store: "memory"
}

`+wrapStep(`
        session_mem.set("objs", [1, 2, 3])
        log(session_mem.get("objs").size())
    `))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "3\n") {
		t.Errorf("unexpected output: %s", out)
	}
}

func TestEvalSizeOnVariable(t *testing.T) {
	out, err := run(t, `
memory session_mem {
    type: "kv"
    store: "memory"
}

`+wrapStep(`
        session_mem.set("objs", [1, 2, 3])
        var objs = session_mem.get("objs")
        log(objs.size())
    `))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "3\n") {
		t.Errorf("unexpected output: %s", out)
	}
}

func TestEvalSizeOnObjectCountsEntries(t *testing.T) {
	out, err := run(t, wrapStep(`
        var cfg = {a: 1, b: 2}
        log(cfg.size())
    `))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "2\n") {
		t.Errorf("unexpected output: %s", out)
	}
}

func TestEvalSizeOnStringCountsBytes(t *testing.T) {
	out, err := run(t, wrapStep(`
        var s = "hello"
        log(s.size())
    `))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "5\n") {
		t.Errorf("unexpected output: %s", out)
	}
}

func TestEvalIsEmpty(t *testing.T) {
	out, err := run(t, wrapStep(`
        var empty = []
        var full = [1]
        log(empty.is_empty())
        log(full.is_empty())
    `))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "true\n") || !strings.Contains(out, "false\n") {
		t.Errorf("unexpected output: %s", out)
	}
}

func TestEvalSizeOnScalarErrors(t *testing.T) {
	_, err := run(t, wrapStep(`
        var x = 1
        log(x.size())
    `))
	if err == nil || !strings.Contains(err.Error(), "size() is not defined for a number value") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEvalUnknownValueMethodErrors(t *testing.T) {
	_, err := run(t, wrapStep(`
        var arr = [1]
        log(arr.pop())
    `))
	if err == nil || !strings.Contains(err.Error(), `array value has no method "pop"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEvalSizeWithArgumentsErrors(t *testing.T) {
	_, err := run(t, wrapStep(`
        var arr = [1]
        log(arr.size(1))
    `))
	if err == nil || !strings.Contains(err.Error(), "size() takes no arguments") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEvalGetIndexChainedOnMemoryGet(t *testing.T) {
	out, err := run(t, `
memory session_mem {
    type: "kv"
    store: "memory"
}

`+wrapStep(`
        session_mem.set("objs", [{id: 1}, {id: 2}, {id: 3}])
        log(session_mem.get("objs").get_index(1))
    `))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, `{"id":2}`) {
		t.Errorf("unexpected output: %s", out)
	}
}

func TestEvalGetIndexOnVariableThenFieldAccess(t *testing.T) {
	out, err := run(t, `
memory session_mem {
    type: "kv"
    store: "memory"
}

`+wrapStep(`
        session_mem.set("objs", [{id: 1}, {id: 2}])
        var objs = session_mem.get("objs")
        log(objs.get_index(0).id)
    `))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "1\n") {
		t.Errorf("unexpected output: %s", out)
	}
}

func TestEvalGetIndexOutOfRangeErrors(t *testing.T) {
	_, err := run(t, wrapStep(`
        var arr = [1, 2]
        log(arr.get_index(5))
    `))
	if err == nil || !strings.Contains(err.Error(), "get_index(5) out of range (size 2)") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEvalGetIndexNegativeErrors(t *testing.T) {
	_, err := run(t, wrapStep(`
        var arr = [1, 2]
        log(arr.get_index(-1))
    `))
	if err == nil || !strings.Contains(err.Error(), "out of range") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEvalGetIndexNonIntegerErrors(t *testing.T) {
	_, err := run(t, wrapStep(`
        var arr = [1, 2]
        log(arr.get_index(1.5))
    `))
	if err == nil || !strings.Contains(err.Error(), "must be an integer") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEvalGetIndexOnNonArrayErrors(t *testing.T) {
	_, err := run(t, wrapStep(`
        var obj = {a: 1}
        log(obj.get_index(0))
    `))
	if err == nil || !strings.Contains(err.Error(), "get_index() is not defined for a object value") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEvalGetIndexWrongArgCountErrors(t *testing.T) {
	_, err := run(t, wrapStep(`
        var arr = [1, 2]
        log(arr.get_index())
    `))
	if err == nil || !strings.Contains(err.Error(), "get_index() requires exactly one argument") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEvalIndexOfFindsElement(t *testing.T) {
	out, err := run(t, wrapStep(`
        var arr = [1, 2, 3]
        log(arr.index_of(3))
    `))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "2\n") {
		t.Errorf("unexpected output: %s", out)
	}
}

func TestEvalIndexOfMissingElementReturnsMinusOne(t *testing.T) {
	out, err := run(t, wrapStep(`
        var arr = [1, 2, 3]
        log(arr.index_of(99))
    `))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "-1\n") {
		t.Errorf("unexpected output: %s", out)
	}
}

func TestEvalIndexOfComparesByValueNotJustNumbers(t *testing.T) {
	out, err := run(t, wrapStep(`
        var arr = ["a", "b", "c"]
        log(arr.index_of("c"))
    `))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "2\n") {
		t.Errorf("unexpected output: %s", out)
	}
}

func TestEvalIndexOfOnNonArrayErrors(t *testing.T) {
	_, err := run(t, wrapStep(`
        var obj = {a: 1}
        log(obj.index_of(1))
    `))
	if err == nil || !strings.Contains(err.Error(), "index_of() is not defined for a object value") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEvalIndexOfWrongArgCountErrors(t *testing.T) {
	_, err := run(t, wrapStep(`
        var arr = [1, 2]
        log(arr.index_of())
    `))
	if err == nil || !strings.Contains(err.Error(), "index_of() requires exactly one argument") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEvalBracketIndexReadsElement(t *testing.T) {
	out, err := run(t, wrapStep(`
        var arr = [10, 20, 30]
        log(arr[0])
        log(arr[2])
    `))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, want := range []string{"10\n", "30\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q, got: %s", want, out)
		}
	}
}

// TestEvalOptionalMemberOnNonObject proves `x?.name` yields null — not a
// type error — when x is a non-null scalar, matching its behavior on a null
// receiver or a missing field. Plain `x.name` still raises.
func TestEvalOptionalMemberOnNonObject(t *testing.T) {
	out, err := run(t, wrapStep(`
        var s = "hello"
        log(s?.name ?? "none")
        log((42)?.field ?? "none")
    `))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if strings.Count(out, "none\n") != 2 {
		t.Errorf("expected two null short-circuits, got: %s", out)
	}

	_, err = run(t, wrapStep(`
        var s = "hello"
        log(s.name)
    `))
	if err == nil || !strings.Contains(err.Error(), `cannot access field "name" on a string value`) {
		t.Fatalf("plain access should still raise, got: %v", err)
	}
}

// TestEvalOptionalDynamicIndex covers the `x?.[key]` trailer: a present key,
// a missing object key, an out-of-range array index, a non-indexable
// receiver, and a null hop that short-circuits the rest of the chain.
func TestEvalOptionalDynamicIndex(t *testing.T) {
	out, err := run(t, wrapStep(`
        var obj = {a: 1, nested: {x: 10}}
        var arr = [10, 20, 30]
        var k = "a"
        log(obj?.[k])
        log(obj?.["missing"] ?? "def")
        log(obj?.["nested"]?.["x"])
        log(obj?.["nope"]?.["x"] ?? "sc")
        log(arr?.[1])
        log(arr?.[99] ?? "oob")
        log("hi"?.[k] ?? "scalar")
        log(null?.[k] ?? "nil")
    `))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, want := range []string{"1\n", "def\n", "10\n", "sc\n", "20\n", "oob\n", "scalar\n", "nil\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

// TestEvalOptionalDynamicIndexKeyTypeStillRaises proves `?.[key]` only
// swallows *absence* — a key whose type is wrong for the receiver is a real
// program bug and still errors.
func TestEvalOptionalDynamicIndexKeyTypeStillRaises(t *testing.T) {
	_, err := run(t, wrapStep(`
        var arr = [1, 2]
        log(arr?.["oops"])
    `))
	if err == nil || !strings.Contains(err.Error(), "array index must be a number") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEvalBracketIndexChained(t *testing.T) {
	out, err := run(t, wrapStep(`
        var matrix = [[1, 2], [3, 4]]
        log(matrix[1][0])
    `))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "3\n") {
		t.Errorf("unexpected output: %s", out)
	}
}

// TestEvalBracketIndexReadsObjectByDynamicKey proves `obj[key]` reads a
// field by a runtime-computed string key — unlike the static `.field`
// trailer, key here is a variable, not a literal identifier fixed at parse
// time. This is what makes dynamic field access on config-shaped objects
// possible at all.
func TestEvalBracketIndexReadsObjectByDynamicKey(t *testing.T) {
	out, err := run(t, wrapStep(`
        var obj = {a: 1, b: 2}
        var key = "b"
        log(obj[key])
    `))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "2\n") {
		t.Errorf("unexpected output: %s", out)
	}
}

// TestEvalBracketIndexChainedNestedObjects proves dynamic-key indexing
// composes across nesting levels — `config[section][field]`, both keys
// runtime variables — the same chaining TestEvalBracketIndexChained already
// covers for arrays, now for a nested object.
func TestEvalBracketIndexChainedNestedObjects(t *testing.T) {
	out, err := run(t, wrapStep(`
        var config = {agent: {model: "gpt-5", retries: 3}}
        var section = "agent"
        var field = "retries"
        log(config[section][field])
    `))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "3\n") {
		t.Errorf("unexpected output: %s", out)
	}
}

// TestEvalBracketIndexMissingFieldErrors proves a missing key surfaces the
// same "field not found" error the static `.field` trailer already gives
// for a missing member (eval.go's Member case), rather than silently
// returning null.
func TestEvalBracketIndexMissingFieldErrors(t *testing.T) {
	_, err := run(t, wrapStep(`
        var obj = {a: 1}
        var key = "missing"
        log(obj[key])
    `))
	if err == nil || !strings.Contains(err.Error(), `field "missing" not found`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEvalBracketIndexOutOfRangeErrors(t *testing.T) {
	_, err := run(t, wrapStep(`
        var arr = [1, 2]
        log(arr[5])
    `))
	if err == nil || !strings.Contains(err.Error(), "index 5 out of range (size 2)") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEvalBracketIndexOnNonArrayErrors(t *testing.T) {
	_, err := run(t, wrapStep(`
        var notacontainer = "hello"
        log(notacontainer[0])
    `))
	if err == nil || !strings.Contains(err.Error(), "cannot index a string value") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestEvalBracketIndexOnObjectWithNonStringKeyErrors proves an object is
// indexable (unlike a plain scalar), but only by a string key — a numeric
// key gets its own clear error rather than the generic "cannot index"
// one, since `obj[0]` is a type mismatch on the key, not an attempt to
// index something unindexable.
func TestEvalBracketIndexOnObjectWithNonStringKeyErrors(t *testing.T) {
	_, err := run(t, wrapStep(`
        var obj = {a: 1}
        log(obj[0])
    `))
	if err == nil || !strings.Contains(err.Error(), "object key must be a string, got number") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEvalBracketIndexAssignWritesElement(t *testing.T) {
	out, err := run(t, wrapStep(`
        var arr = [1, 2, 3]
        arr[1] = 99
        log(arr)
    `))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "[1,99,3]") {
		t.Errorf("unexpected output: %s", out)
	}
}

func TestEvalBracketIndexAssignChained(t *testing.T) {
	out, err := run(t, wrapStep(`
        var matrix = [[1, 2], [3, 4]]
        matrix[1][0] = 100
        log(matrix)
    `))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "[[1,2],[100,4]]") {
		t.Errorf("unexpected output: %s", out)
	}
}

// TestEvalBracketIndexAssignChainedNestedObjects is
// TestEvalBracketIndexAssignChained's object counterpart: writing through a
// chain of dynamic keys (`config[section][field] = value`) mutates the
// inner object in place, visible through the outer variable with no
// reassignment step at either level — the same reference-sharing behavior
// arrays already had, now for nested objects.
func TestEvalBracketIndexAssignChainedNestedObjects(t *testing.T) {
	out, err := run(t, wrapStep(`
        var config = {agent: {model: "gpt-5", retries: 3}}
        var section = "agent"
        var field = "retries"
        config[section][field] = 5
        log(config)
    `))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, `{"agent":{"model":"gpt-5","retries":5}}`) {
		t.Errorf("unexpected output: %s", out)
	}
}

func TestEvalBracketIndexAssignAliasesSameBackingArray(t *testing.T) {
	// A slice assigned to another variable shares its backing array — the
	// same reference semantics `var b = a; b[...] via get_index already has
	// — so a bracket-index write through one name is visible through the
	// other, with no reassignment step needed.
	out, err := run(t, wrapStep(`
        var a = [1, 2, 3]
        var b = a
        b[0] = 42
        log(a)
    `))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "[42,2,3]") {
		t.Errorf("unexpected output: %s", out)
	}
}

func TestEvalBracketIndexAssignOutOfRangeErrors(t *testing.T) {
	_, err := run(t, wrapStep(`
        var arr = [1, 2, 3]
        arr[5] = 1
    `))
	if err == nil || !strings.Contains(err.Error(), "index 5 out of range (size 3)") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEvalBracketIndexAssignNonIntegerErrors(t *testing.T) {
	_, err := run(t, wrapStep(`
        var arr = [1, 2, 3]
        arr[1.5] = 1
    `))
	if err == nil || !strings.Contains(err.Error(), "array index must be an integer") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEvalBracketIndexAssignOnNonArrayErrors(t *testing.T) {
	_, err := run(t, wrapStep(`
        var notacontainer = "hello"
        notacontainer[0] = 1
    `))
	if err == nil || !strings.Contains(err.Error(), "cannot index a string value") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestEvalBracketIndexAssignOnObjectWritesDynamicField proves `obj[key] =
// value` (key evaluated at runtime, unlike the static `obj.field = value`
// which was never supported at all) sets/overwrites a field on an object,
// the write-side counterpart of TestEvalBracketIndexReadsObjectByDynamicKey.
func TestEvalBracketIndexAssignOnObjectWritesDynamicField(t *testing.T) {
	out, err := run(t, wrapStep(`
        var obj = {a: 1}
        var key = "b"
        obj[key] = 2
        obj["a"] = 99
        log(obj)
    `))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, `{"a":99,"b":2}`) {
		t.Errorf("unexpected output: %s", out)
	}
}

func TestEvalMemberThenIndexAssignErrors(t *testing.T) {
	// obj.field = expr was never supported, and mixing a Member trailer into
	// an index-assignment chain isn't either — only a pure identifier[i]...
	// chain is an assignable target.
	_, err := run(t, wrapStep(`
        var obj = {list: [1, 2, 3]}
        obj.list[0] = 42
    `))
	if err == nil || !strings.Contains(err.Error(), "assignment target must be a plain variable or an array index") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEvalIndexAssignToUndeclaredVariableErrors(t *testing.T) {
	_, err := run(t, wrapStep(`
        arr[0] = 1
    `))
	if err == nil || !strings.Contains(err.Error(), `undefined variable "arr"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEvalKeysReturnsSortedKeys(t *testing.T) {
	out, err := run(t, wrapStep(`
        var obj = {b: 2, a: 1, c: 3}
        log(obj.keys())
    `))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, `["a","b","c"]`) {
		t.Errorf("unexpected output: %s", out)
	}
}

func TestEvalValuesMatchesKeysOrder(t *testing.T) {
	out, err := run(t, wrapStep(`
        var obj = {b: 2, a: 1, c: 3}
        log(obj.values())
    `))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, `[1,2,3]`) {
		t.Errorf("unexpected output: %s", out)
	}
}

func TestEvalKeysOnNonObjectErrors(t *testing.T) {
	_, err := run(t, wrapStep(`
        var arr = [1, 2]
        log(arr.keys())
    `))
	if err == nil || !strings.Contains(err.Error(), "keys() is not defined for a array value") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEvalValuesOnNonObjectErrors(t *testing.T) {
	_, err := run(t, wrapStep(`
        var arr = [1, 2]
        log(arr.values())
    `))
	if err == nil || !strings.Contains(err.Error(), "values() is not defined for a array value") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEvalKeysWrongArgCountErrors(t *testing.T) {
	_, err := run(t, wrapStep(`
        var obj = {a: 1}
        log(obj.keys(1))
    `))
	if err == nil || !strings.Contains(err.Error(), "keys() takes no arguments") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEvalValueNestingStillCapped(t *testing.T) {
	nested := strings.Repeat("[", 11) + "1" + strings.Repeat("]", 11)
	_, err := run(t, wrapStep(`var deep = `+nested))
	if err == nil || !strings.Contains(err.Error(), "value nesting exceeds the maximum depth of 10") {
		t.Fatalf("unexpected error: %v", err)
	}
}
