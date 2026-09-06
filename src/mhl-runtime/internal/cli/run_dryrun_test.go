package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/cli"
)

func dryRun(t *testing.T, src string, extra ...string) (string, error) {
	t.Helper()
	chdirTemp(t)
	if err := os.WriteFile("p.mh", []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	args := append([]string{"run", "p.mh", "--dry-run"}, extra...)
	err := cli.Run(args, &buf)
	return buf.String(), err
}

func TestDryRunPrintsPlanWithoutExecuting(t *testing.T) {
	out, err := dryRun(t, `
workflow Deploy {
    input env: string
    var url = ""
    output: { url: url }
    step Build { url = "x" }
    parallel Checks {
        step Lint { log("nope — must not run") }
        step Test { log("nope") }
    }
    step Ship { url = url + "!" }
}
`, "--input", "env=staging")
	if err != nil {
		t.Fatalf("dry-run: %v\n%s", err, out)
	}
	if strings.Contains(out, "nope") {
		t.Fatalf("dry-run executed a step (log output leaked):\n%s", out)
	}
	for _, want := range []string{"workflow Deploy", "parallel Checks", "Ship", "explicit projection", "No step ran"} {
		if !strings.Contains(out, want) {
			t.Errorf("plan missing %q:\n%s", want, out)
		}
	}
}

func TestDryRunListsGotoEdgesAndPrettyTTL(t *testing.T) {
	out, err := dryRun(t, `
workflow Review {
    checkpoint: { ttl: 7d }
    var approved = false
    step Do { approved = true }
    step Gate {
        if (!approved) goto Do
    }
}
`)
	if err != nil {
		t.Fatalf("dry-run: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Gate -> Do") {
		t.Errorf("goto edge not listed:\n%s", out)
	}
	if !strings.Contains(out, "ttl 7d") {
		t.Errorf("ttl not pretty-printed (want 7d):\n%s", out)
	}
}

func TestDryRunJSONShape(t *testing.T) {
	out, err := dryRun(t, `
pipeline P {
    input name: string
    step S { var x = name }
}
`, "--format", "json", "--input", "name=ana")
	if err != nil {
		t.Fatalf("dry-run: %v\n%s", err, out)
	}
	var obj map[string]any
	if e := json.Unmarshal([]byte(out), &obj); e != nil {
		t.Fatalf("not one JSON object: %v\n%s", e, out)
	}
	if obj["ok"] != true || obj["dry_run"] != true {
		t.Fatalf("ok/dry_run = %v/%v", obj["ok"], obj["dry_run"])
	}
	if obj["pipeline"] != "P" {
		t.Errorf("pipeline = %v", obj["pipeline"])
	}
	steps, _ := obj["steps"].([]any)
	if len(steps) != 1 || steps[0] != "S" {
		t.Errorf("steps = %v", obj["steps"])
	}
	if _, ok := obj["note"].(string); !ok {
		t.Error("expected a scope `note`")
	}
}

func TestDryRunReportsMissingInputAndExitsNonZero(t *testing.T) {
	out, err := dryRun(t, `
pipeline P {
    input required: string
    step S { var x = required }
}
`)
	if err == nil {
		t.Fatalf("expected non-zero exit for the missing input:\n%s", out)
	}
	if !strings.Contains(out, "MISSING inputs") || !strings.Contains(out, "required") {
		t.Errorf("plan did not flag the missing input:\n%s", out)
	}
}

func TestDryRunSurfacesLintFindings(t *testing.T) {
	out, err := dryRun(t, `
pipeline P {
    checkpont: { enabled: true }
    step S { var x = 1 }
}
`)
	if err == nil {
		t.Fatalf("expected non-zero exit when lint has a finding:\n%s", out)
	}
	if !strings.Contains(out, "checkpont") {
		t.Errorf("lint finding not surfaced:\n%s", out)
	}
}

func TestDryRunRejectsResumeCombo(t *testing.T) {
	_, err := dryRun(t, "pipeline P { step S { var x = 1 } }", "--resume")
	if err == nil || !strings.Contains(err.Error(), "cannot be combined with --resume") {
		t.Fatalf("want a --dry-run/--resume error, got: %v", err)
	}
}
