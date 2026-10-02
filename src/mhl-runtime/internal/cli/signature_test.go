package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/cli"
)

const typedReviewSrc = `
type Finding = { file: string, message: string }
type ReviewInput = { diff: string, base?: string }
type ReviewOutput = { approved: bool, findings: Finding[], summary: string }

workflow Review(req: ReviewInput): ReviewOutput {
    var findings = []
    var summary = ""

    step Inspect {
        var base = req.base ?? "main"
        findings = [{ file: "a.go", message: req.diff }]
        summary = "against " + base
    }

    output: {
        approved: findings.size() == 0
        findings: findings
        summary: summary
    }
}
`

func TestRunTypedSignature(t *testing.T) {
	cases := []struct {
		name        string
		args        []string
		wantSummary string
	}{
		{"optional field omitted reads null", []string{"--input", "diff=d1"}, "against main"},
		{"optional field supplied", []string{"--input", "diff=d1", "--input", "base=dev"}, "against dev"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			obj, err := runJSONOut(t, typedReviewSrc, tc.args...)
			if err != nil {
				t.Fatalf("run: %v (%#v)", err, obj)
			}
			want := map[string]any{
				"approved": false,
				"findings": []any{map[string]any{"file": "a.go", "message": "d1"}},
				"summary":  tc.wantSummary,
			}
			if !reflect.DeepEqual(obj["vars"], want) {
				t.Fatalf("vars = %#v, want exactly the output projection %#v", obj["vars"], want)
			}
		})
	}
}

func TestRunTypedSignatureMissingRequiredField(t *testing.T) {
	obj, err := runJSONOut(t, typedReviewSrc)
	if err == nil {
		t.Fatal("expected a missing required field to fail admission")
	}
	if obj["kind"] != "invalid_inputs" {
		t.Fatalf("kind = %v, want invalid_inputs: %#v", obj["kind"], obj)
	}
	if missing, _ := obj["missing"].([]any); len(missing) != 1 || missing[0] != "diff" {
		t.Fatalf("missing = %v, want [diff]", obj["missing"])
	}
}

func TestRunTypedSignatureOutputContract(t *testing.T) {
	obj, err := runJSONOut(t, `
type Out = { ok: bool, n: number }

workflow Broken: Out {
    var result = { ok: "yes", n: 1 }
    step S { log("s") }
    output: result
}
`)
	if err == nil {
		t.Fatal("expected a projection that breaks the result type to fail the run")
	}
	if obj["kind"] != "output_contract" {
		t.Fatalf("kind = %v, want output_contract: %#v", obj["kind"], obj)
	}
}

func TestRunTypedSignatureBreakIsChecked(t *testing.T) {
	obj, err := runJSONOut(t, `
type Out = { done: bool }

workflow Early: Out {
    var done = null
    step S { break "early" }
    output: { done: done ?? "no" }
}
`)
	if err == nil || obj["kind"] != "output_contract" {
		t.Fatalf("a `break` result is checked against the result type too; got err=%v obj=%#v", err, obj)
	}
}

// runJSONFiles writes files into a fresh temp cwd once, then returns a
// runner for `mhl run <args> --format json` there — so a test can run, then
// --resume, against the same state directory.
func runJSONFiles(t *testing.T, files map[string]string) func(args ...string) (map[string]any, error) {
	t.Helper()
	chdirTemp(t)
	for name, body := range files {
		if err := os.WriteFile(name, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return func(args ...string) (map[string]any, error) {
		t.Helper()
		var buf bytes.Buffer
		runErr := cli.Run(append(append([]string{"run"}, args...), "--format", "json"), &buf)
		var obj map[string]any
		if err := json.Unmarshal(buf.Bytes(), &obj); err != nil {
			t.Fatalf("stdout is not a single JSON object: %v\n%s", err, buf.String())
		}
		return obj, runErr
	}
}

func TestRunTypedSignatureResumeMergesParam(t *testing.T) {
	run := runJSONFiles(t, map[string]string{"main.mh": `
type In = { diff: string, base?: string, approved?: string }
type Out = { summary: string }

workflow Review(req: In): Out {
    var summary = ""
    step Prepare { summary = req.diff }
    step Gate { if (req.approved != "yes") { pause("awaiting approval") } }
    step Finish { summary = summary + "@" + (req.base ?? "main") }
    output: { summary: summary }
}
`})
	first, err := run("main.mh", "--session", "typed-resume", "--input", "diff=d1")
	if err != nil || first["pause_reason"] != "awaiting approval" {
		t.Fatalf("want a pause, got err=%v obj=%#v", err, first)
	}
	// Only the new fields: diff comes back from the checkpointed req.
	final, err := run("main.mh", "--session", "typed-resume", "--resume", "--input", "approved=yes", "--input", "base=dev")
	if err != nil {
		t.Fatalf("resume: %v (%#v)", err, final)
	}
	if want := map[string]any{"summary": "d1@dev"}; !reflect.DeepEqual(final["vars"], want) {
		t.Fatalf("vars = %#v, want %#v", final["vars"], want)
	}
}

func TestRunTypedLoopWorkflowChecksOutputAtTheEnd(t *testing.T) {
	src := `
type In = { step: number }
type Out = { total: number }

loop workflow Count(req: In): Out max 3 {
    mem total = 0                                // vars reset per iteration; mem carries across
    step Add { total = total + req.step }
    output: { total: total }
}
`
	obj, err := runJSONOut(t, src, "--input", "step=2")
	if err != nil {
		t.Fatalf("run: %v (%#v)", err, obj)
	}
	if want := map[string]any{"total": 6.0}; !reflect.DeepEqual(obj["vars"], want) {
		t.Fatalf("vars = %#v, want %#v (three iterations of +2, projected once)", obj["vars"], want)
	}

	broken := strings.Replace(src, "output: { total: total }", `output: { total: total > 0 }`, 1)
	obj, err = runJSONOut(t, broken, "--input", "step=2")
	if err == nil || obj["kind"] != "output_contract" {
		t.Fatalf("a loop's final projection is checked too; got err=%v obj=%#v", err, obj)
	}
}

func TestRunTypedParamVisibleInParallelBranches(t *testing.T) {
	obj, err := runJSONOut(t, `
type In = { a: string, b: string }
type Out = { joined: string }

workflow Fan(req: In): Out {
    var left = ""
    var right = ""
    parallel Both {
        step L { left = req.a }
        step R { right = req.b }
    }
    output: { joined: left + right }
}
`, "--input", "a=x", "--input", "b=y")
	if err != nil {
		t.Fatalf("run: %v (%#v)", err, obj)
	}
	if want := map[string]any{"joined": "xy"}; !reflect.DeepEqual(obj["vars"], want) {
		t.Fatalf("vars = %#v, want %#v", obj["vars"], want)
	}
}

func TestRunTypedSignatureWithImportedTypes(t *testing.T) {
	run := runJSONFiles(t, map[string]string{
		"types.mh": `
export type In = { name: string, greeting?: string }
export type Out = { message: string }
`,
		"main.mh": `
import { In, Out } from "./types.mh"

workflow Greet(req: In): Out {
    var message = ""
    step S { message = (req.greeting ?? "hi") + " " + req.name }
    output: { message: message }
}
`,
	})
	obj, err := run("main.mh", "--input", "name=ana")
	if err != nil {
		t.Fatalf("run: %v (%#v)", err, obj)
	}
	if want := map[string]any{"message": "hi ana"}; !reflect.DeepEqual(obj["vars"], want) {
		t.Fatalf("vars = %#v, want %#v", obj["vars"], want)
	}
}

func TestRunTypedSignatureOnASiblingPartialFragment(t *testing.T) {
	run := runJSONFiles(t, map[string]string{
		"main.mh": `
import "main.finish.mh"

type In = { name: string }
type Out = { message: string }

partial workflow Greet(req: In): Out {
    var message = ""
    entry step Start { goto Finish }
    output: { message: message }
}
`,
		"main.finish.mh": `
partial workflow Greet {
    step Finish { message = "hello " + req.name }
}
`,
	})
	obj, err := run("main.mh", "--input", "name=ana")
	if err != nil {
		t.Fatalf("run: %v (%#v)", err, obj)
	}
	if want := map[string]any{"message": "hello ana"}; !reflect.DeepEqual(obj["vars"], want) {
		t.Fatalf("vars = %#v, want %#v (the sibling fragment reads req)", obj["vars"], want)
	}
}

func TestRunTypedParamIsReadOnly(t *testing.T) {
	cases := map[string]string{
		"plain assignment":     `req = {diff: "y"}`,
		"through self":         `self.req = 1`,
		"index assignment":     `req["diff"] = "y"`,
		"inside a closure":     "var f = () -> { req = 2 }\n        f()",
		"step-local redeclare": `var req = 1`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			obj, err := runJSONOut(t, "type In = { diff: string }\nworkflow W(req: In) {\n    step S {\n        "+body+"\n    }\n}\n", "--input", "diff=x")
			msg, _ := obj["error"].(string)
			if err == nil || !(strings.Contains(msg, "typed parameter of W is read-only") || strings.Contains(msg, "typed parameter of W and can't be redeclared")) {
				t.Fatalf("want a read-only refusal, got err=%v error=%q", err, msg)
			}
		})
	}

	obj, err := runJSONOut(t, `
type In = { diff: string }
workflow W(req: In) {
    var copy = ""
    step S {
        var local = req.diff
        local = local + "!"
        copy = local
    }
}
`, "--input", "diff=x")
	if err != nil || obj["vars"].(map[string]any)["copy"] != "x!" {
		t.Fatalf("reading the param into a var and changing that must work: err=%v obj=%#v", err, obj)
	}
}

// session_start's session.inputs is the arguments exactly as the caller sent
// them — flat, keyed by field — not the bound `req` object.
func TestRunTypedSessionInputsStayFlat(t *testing.T) {
	obj, err := runJSONOut(t, `
type In = { diff: string, base?: string }
workflow W(req: In) {
    session_start: (session) -> { log("INPUTS " + json.stringify(session.inputs)) }
    step S { log("x") }
}
`, "--input", "diff=d1")
	if err != nil {
		t.Fatalf("run: %v (%#v)", err, obj)
	}
	if log, _ := obj["log"].(string); !strings.Contains(log, `INPUTS {"diff":"d1"}`) {
		t.Fatalf("session.inputs must be the flat caller arguments, log:\n%s", log)
	}
}
