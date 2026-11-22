package cli_test

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A workflow whose stages declare their own inputs: `customer` is the
// workflow's, `plan` is asked for on entering Plan, `card` on entering
// Billing (`coupon` has a default, so it never is).
var stageInputFragments = map[string]string{
	"pipeline.mh": `
import "plan.mh"
import "billing.mh"

partial workflow Onboarding {
    input customer: string
    var trail = []

    entry step Welcome {
        trail.append("welcome ${customer}")
        goto Plan
    }
}
`,
	"plan.mh": `
partial workflow Onboarding stage Plan {
    /// plan: the plan the customer picks.
    input plan: string

    step Choose {
        trail.append("plan ${plan}")
        goto Billing
    }
}
`,
	"billing.mh": `
partial workflow Onboarding stage Billing {
    input card: string
    input coupon: string = "none"

    step Charge { trail.append("charge ${card}") }
    step Receipt {
        trail.append("receipt ${coupon}")
        complete()
    }
}
`,
}

func stageInputRun(t *testing.T, fragments map[string]string) func(extra ...string) (string, error) {
	t.Helper()
	dir := t.TempDir()
	for name, src := range fragments {
		if name == "pipeline.mh" {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return runInDir(t, dir, fragments["pipeline.mh"])
}

func awaitingInputs(t *testing.T, obj map[string]any) (stage, step string, inputs []any) {
	t.Helper()
	a, ok := obj["awaiting_inputs"].(map[string]any)
	if !ok {
		t.Fatalf("no awaiting_inputs in %v", obj)
	}
	stage, _ = a["stage"].(string)
	step, _ = a["step"].(string)
	inputs, _ = a["inputs"].([]any)
	return stage, step, inputs
}

func TestStageInputsPauseOnEntryAndResumeWithOnlyThem(t *testing.T) {
	run := stageInputRun(t, stageInputFragments)

	first := gateRun(t, run, "--session", "s", "--input", "customer=ana")
	if first["ok"] != true || first["paused"] != true {
		t.Fatalf("first leg = %v", first)
	}
	if stage, step, inputs := awaitingInputs(t, first); stage != "Plan" || step != "Choose" || len(inputs) != 1 || inputs[0] != "plan" {
		t.Fatalf("awaiting_inputs = %v", first["awaiting_inputs"])
	}
	if got, _ := json.Marshal(first["executed"]); string(got) != `["Welcome"]` {
		t.Fatalf("executed = %s, want only Welcome (the stage must not start)", got)
	}

	// Still missing: refused, the run stays paused at the same place.
	missing := gateRun(t, run, "--session", "s", "--resume")
	if missing["ok"] != false || missing["kind"] != "invalid_inputs" || missing["stage"] != "Plan" || !strings.Contains(missing["error"].(string), `stage "Plan"`) {
		t.Fatalf("resume without the input = %v", missing)
	}
	if got, _ := json.Marshal(missing["missing"]); string(got) != `["plan"]` {
		t.Fatalf("missing = %s", got)
	}

	// A decision does not answer an input gate.
	decided := gateRun(t, run, "--session", "s", "--resume", "--decision", `"pro"`)
	if decided["ok"] != false || decided["kind"] != "invalid_decision" {
		t.Fatalf("resume with a decision = %v", decided)
	}

	second := gateRun(t, run, "--session", "s", "--resume", "--input", "plan=pro")
	if second["paused"] != true {
		t.Fatalf("second leg = %v", second)
	}
	if stage, step, inputs := awaitingInputs(t, second); stage != "Billing" || step != "Charge" || len(inputs) != 1 || inputs[0] != "card" {
		t.Fatalf("awaiting_inputs = %v", second["awaiting_inputs"])
	}
	schema := second["awaiting_inputs"].(map[string]any)["schema"].(map[string]any)
	if got, _ := json.Marshal(schema["required"]); string(got) != `["card"]` {
		t.Fatalf("schema = %v", schema)
	}

	done := gateRun(t, run, "--session", "s", "--resume", "--input", "card=4242")
	if done["ok"] != true || done["paused"] == true {
		t.Fatalf("last leg = %v", done)
	}
	trail, _ := json.Marshal(done["vars"].(map[string]any)["trail"])
	if string(trail) != `["welcome ana","plan pro","charge 4242","receipt none"]` {
		t.Fatalf("trail = %s", trail)
	}
}

func TestStageInputsSuppliedUpFrontNeverPause(t *testing.T) {
	run := stageInputRun(t, stageInputFragments)
	res := gateRun(t, run, "--input", "customer=ana", "--input", "plan=pro", "--input", "card=1", "--input", "coupon=x")
	if res["ok"] != true || res["paused"] == true || res["awaiting_inputs"] != nil {
		t.Fatalf("run = %v", res)
	}
	trail, _ := json.Marshal(res["vars"].(map[string]any)["trail"])
	if string(trail) != `["welcome ana","plan pro","charge 1","receipt x"]` {
		t.Fatalf("trail = %s", trail)
	}
}

func TestStageInputsWorkflowInputStillRequiredAtStart(t *testing.T) {
	run := stageInputRun(t, stageInputFragments)
	res := gateRun(t, run, "--input", "plan=pro")
	if res["kind"] != "invalid_inputs" {
		t.Fatalf("run = %v", res)
	}
	if got, _ := json.Marshal(res["missing"]); string(got) != `["customer"]` {
		t.Fatalf("missing = %s, want only the workflow-level input", got)
	}
}

// A goto that lands past a stage's first member still enters the stage:
// it pauses there for the stage's inputs.
func TestStageInputsGotoIntoTheMiddleOfAStagePauses(t *testing.T) {
	fragments := map[string]string{}
	for k, v := range stageInputFragments {
		fragments[k] = v
	}
	fragments["plan.mh"] = strings.Replace(fragments["plan.mh"], "goto Billing", "goto Receipt", 1)
	run := stageInputRun(t, fragments)
	res := gateRun(t, run, "--input", "customer=ana", "--input", "plan=pro")
	if stage, step, _ := awaitingInputs(t, res); stage != "Billing" || step != "Receipt" {
		t.Fatalf("awaiting_inputs = %v", res["awaiting_inputs"])
	}
}

func TestStageInputsTextJSONLAndDryRun(t *testing.T) {
	run := stageInputRun(t, stageInputFragments)

	text, err := run("--input", "customer=ana", "--session", "t")
	if err != nil || !strings.Contains(text, `awaiting inputs for stage "Plan": plan — resume with --resume --input plan=<value>`) {
		t.Fatalf("text = %v\n%s", err, text)
	}

	out, err := run("--input", "customer=ana", "--format", "jsonl")
	if err != nil {
		t.Fatalf("jsonl: %v\n%s", err, out)
	}
	var result map[string]any
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		var ev map[string]any
		if json.Unmarshal(sc.Bytes(), &ev) == nil && ev["type"] == "result" {
			result = ev
		}
	}
	if stage, _, _ := awaitingInputs(t, result); stage != "Plan" {
		t.Fatalf("result event = %v", result)
	}

	dry := gateRun(t, run, "--dry-run", "--input", "customer=ana")
	if dry["ok"] != true || dry["missing_inputs"] != nil {
		t.Fatalf("dry-run = %v", dry)
	}
	stages := map[string]any{}
	for _, in := range dry["inputs"].([]any) {
		m := in.(map[string]any)
		stages[m["name"].(string)] = m["stage"]
	}
	if stages["customer"] != nil || stages["plan"] != "Plan" || stages["card"] != "Billing" {
		t.Fatalf("dry-run inputs = %v", dry["inputs"])
	}
	// An input's `///` doc rides along, its repeated "plan:" dropped.
	for _, in := range dry["inputs"].([]any) {
		if m := in.(map[string]any); m["name"] == "plan" && m["doc"] != "the plan the customer picks." {
			t.Fatalf("plan doc = %v", m["doc"])
		}
	}
}
