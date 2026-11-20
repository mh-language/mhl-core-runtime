package cli_test

import (
	"encoding/json"
	"strings"
	"testing"
)

const gatePipeline = `
enum Action { Approve, Revise, Reject }
type Review = { action: Action, feedback?: string }

workflow Gated {
    input legacy: bool = false
    var rounds = 0
    var outcome = ""

    step Work { self.rounds = rounds + 1 }
    step Gate {
        var d = pause("round ${rounds}", decision: Review)
        goto match d.action {
            Action.Approve -> Done
            Action.Revise -> Work
            Action.Reject -> Rejected
        }
    }
    step Rejected {
        self.outcome = "rejected"
        complete()
    }
    step Done { self.outcome = "approved after ${rounds}" }
}
`

func gateRun(t *testing.T, run func(...string) (string, error), args ...string) map[string]any {
	t.Helper()
	out, _ := run(append(args, "--format", "json")...)
	var obj map[string]any
	if err := json.Unmarshal([]byte(out), &obj); err != nil {
		t.Fatalf("not one JSON object: %v\n%s", err, out)
	}
	return obj
}

func TestGateAnswersOnlyThePauseItWasGivenFor(t *testing.T) {
	run := runInDir(t, t.TempDir(), gatePipeline)

	first := gateRun(t, run, "--session", "s")
	awaiting, _ := first["awaiting"].(map[string]any)
	if first["paused"] != true || awaiting["step"] != "Gate" || awaiting["type"] != "Review" {
		t.Fatalf("first leg = %v", first)
	}
	schema, _ := awaiting["schema"].(map[string]any)
	action, _ := schema["properties"].(map[string]any)["action"].(map[string]any)
	if got, _ := json.Marshal(action["enum"]); string(got) != `["Approve","Revise","Reject"]` {
		t.Fatalf("decision schema = %v", schema)
	}

	// Resuming without a decision pauses at the same gate again.
	again := gateRun(t, run, "--session", "s", "--resume")
	if again["paused"] != true || again["vars"].(map[string]any)["rounds"] != 1.0 {
		t.Fatalf("resume without a decision = %v", again)
	}

	// A decision of the wrong shape is refused before anything runs.
	bad := gateRun(t, run, "--session", "s", "--resume", "--decision", `{"action":"Maybe"}`)
	if bad["ok"] != false || bad["kind"] != "invalid_decision" || !strings.Contains(bad["error"].(string), "not a variant") {
		t.Fatalf("bad decision = %v", bad)
	}

	// Revise is consumed by this gate: the next round pauses again.
	revised := gateRun(t, run, "--session", "s", "--resume", "--decision", `{"action":"Revise","feedback":"tighten"}`)
	if revised["paused"] != true || revised["vars"].(map[string]any)["rounds"] != 2.0 {
		t.Fatalf("after Revise = %v", revised)
	}

	done := gateRun(t, run, "--session", "s", "--resume", "--decision", `{"action":"Approve"}`)
	if done["ok"] != true || done["paused"] == true || done["vars"].(map[string]any)["outcome"] != "approved after 2" {
		t.Fatalf("after Approve = %v", done)
	}

	// Nothing is waiting any more.
	late := gateRun(t, run, "--session", "s", "--resume", "--decision", `{"action":"Approve"}`)
	if late["kind"] != "invalid_decision" {
		t.Fatalf("decision after the run ended = %v", late)
	}
}

func TestDecisionRefusedWhereNoGateWaits(t *testing.T) {
	run := runInDir(t, t.TempDir(), `
workflow Plain {
    input approved: bool = false
    step Ask { if (!approved) { pause("approve?") } }
    step Done { log("done") }
}
`)
	if _, err := run("--decision", "true"); err == nil || !strings.Contains(err.Error(), "only applies with --resume") {
		t.Fatalf("decision without --resume: %v", err)
	}
	first := gateRun(t, run, "--session", "p")
	if first["paused"] != true || first["awaiting"] != nil {
		t.Fatalf("a plain pause awaits no decision: %v", first)
	}
	bad := gateRun(t, run, "--session", "p", "--resume", "--decision", "true")
	if bad["kind"] != "invalid_decision" || !strings.Contains(bad["error"].(string), "not waiting for a decision") {
		t.Fatalf("decision for a plain pause = %v", bad)
	}
	// The plain pause still resumes the way it always did.
	ok := gateRun(t, run, "--session", "p", "--resume", "--input", "approved=true")
	if ok["ok"] != true || ok["paused"] == true {
		t.Fatalf("plain resume = %v", ok)
	}
}

func TestGateWithAScalarDecision(t *testing.T) {
	run := runInDir(t, t.TempDir(), `
workflow Confirm {
    var answer = ""
    step Ask {
        var yes = pause("ship it?", decision: bool)
        self.answer = if (yes) "shipped" else "held"
    }
}
`)
	gateRun(t, run, "--session", "c")
	done := gateRun(t, run, "--session", "c", "--resume", "--decision", "false")
	if done["vars"].(map[string]any)["answer"] != "held" {
		t.Fatalf("scalar decision = %v", done)
	}
}
