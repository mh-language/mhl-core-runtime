package cli_test

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/cli"
)

// A workflow split by phase: the entry fragment, then one fragment per stage,
// each declaring its stage on its header.
var stagedFragments = map[string]string{
	"pipeline.mh": `
import "build.mh"
import "ship.mh"

partial workflow Staged {
    var n = 0

    step_start: (step) -> {
        log("hook ${step.step}@${step.stage}")
    }

    entry step Intake {
        log("intake")
        goto Build
    }
}
`,
	"build.mh": `
partial workflow Staged stage Build {
    step Compile { self.n = n + 1 }
    parallel Checks {
        step Lint { log("lint") }
        step Unit { log("unit") }
        merge { goto Ship }
    }
}
`,
	"ship.mh": `
partial workflow Staged stage Ship {
    step Gate { if (n < 2) { goto Build } }
    step Publish {
        log("published after ${n}")
        complete()
    }
}
`,
}

func stagedRun(t *testing.T) func(extra ...string) (string, error) {
	t.Helper()
	dir := t.TempDir()
	for name, src := range stagedFragments {
		if name == "pipeline.mh" {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return runInDir(t, dir, stagedFragments["pipeline.mh"])
}

func TestStageGotoEntersItsFirstMember(t *testing.T) {
	out, err := stagedRun(t)("--format", "jsonl")
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	var started []string
	var stages []any
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		var ev map[string]any
		if json.Unmarshal(sc.Bytes(), &ev) != nil {
			continue
		}
		switch ev["type"] {
		case "run_start":
			stages, _ = ev["stages"].([]any)
		case "step_start":
			s := ev["step"].(string)
			if strings.HasSuffix(s, ".merge") || s == "Lint" || s == "Unit" {
				continue // branch order varies; the group is checked through Compile
			}
			if st, ok := ev["stage"].(string); ok {
				s += "@" + st
			}
			started = append(started, s)
		}
	}
	want := "Intake Compile@Build Gate@Ship Compile@Build Gate@Ship Publish@Ship"
	if got := strings.Join(started, " "); got != want {
		t.Fatalf("steps started:\n got %s\nwant %s", got, want)
	}
	if b, _ := json.Marshal(stages); string(b) != `[{"name":"Build","steps":["Compile","Lint","Unit","Checks.merge"]},{"name":"Ship","steps":["Gate","Publish"]}]` {
		t.Fatalf("run_start stages = %s", b)
	}
}

func TestStageReachesStepHooks(t *testing.T) {
	out, err := stagedRun(t)()
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	for _, want := range []string{"hook Intake@null", "hook Compile@Build", "hook Lint@Build", "hook Checks.merge@Build", "hook Publish@Ship"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

// A goto into the middle of another stage is a lint warning: listed by
// `mhl lint` and `--dry-run`, never a failure.
func TestStageEntryWarningDoesNotFail(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "spec.mh"), []byte(`
partial workflow W stage Spec {
    step Draft { log("draft") }
    step Approve { complete() }
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	run := runInDir(t, dir, `
import "spec.mh"
partial workflow W {
    entry step Start { goto Approve }
}
`)
	var buf strings.Builder
	if err := cli.Run([]string{"lint", "."}, &buf); err != nil {
		t.Fatalf("mhl lint failed on a warning: %v\n%s", err, buf.String())
	}
	if !strings.Contains(buf.String(), "warning: `goto Approve` from outside any stage enters stage \"Spec\"") || !strings.Contains(buf.String(), "No problems found; 1 warning(s).") {
		t.Fatalf("mhl lint output:\n%s", buf.String())
	}
	out, err := run("--dry-run")
	if err != nil || !strings.Contains(out, "warning: `goto Approve`") {
		t.Fatalf("dry-run: %v\n%s", err, out)
	}
}

// `--resume --from <stage>` redoes a phase of a stopped run: it re-enters the
// stage at its first step, with the variables as of the stop.
func TestResumeFromAStage(t *testing.T) {
	dir := t.TempDir()
	write := func(name, src string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("build.mh", `
partial workflow Release stage Build {
    step Compile { self.builds = builds + 1 }
    step Package {
        log("package ${builds}")
        goto Review
    }
}
`)
	write("review.mh", `
partial workflow Release stage Review {
    step Gate { var ok = pause("ship build ${builds}?", decision: bool) }
    step Ship { complete() }
}
`)
	run := runInDir(t, dir, `
import "build.mh"
import "review.mh"
partial workflow Release {
    var builds = 0
    entry step Start { goto Build }
}
`)
	first := gateRun(t, run, "--session", "r")
	if first["paused"] != true || first["vars"].(map[string]any)["builds"] != 1.0 {
		t.Fatalf("first leg = %v", first)
	}

	redo := gateRun(t, run, "--session", "r", "--resume", "--from", "Build")
	executed := func(r map[string]any) string {
		var s []string
		for _, x := range r["executed"].([]any) {
			s = append(s, x.(string))
		}
		return strings.Join(s, ",")
	}
	// The Build stage ran again on the stopped run's variables, then paused at the gate anew.
	if redo["paused"] != true || redo["vars"].(map[string]any)["builds"] != 2.0 || executed(redo) != "Compile,Package,Gate" {
		t.Fatalf("redo = %v", redo)
	}
	if skipped, _ := json.Marshal(redo["skipped"]); string(skipped) != `["Start"]` {
		t.Fatalf("skipped = %s, want only the steps outside the redone stage", skipped)
	}

	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"--session", "r", "--resume", "--from", "Nope"}, `has no stage "Nope" (its stages: Build, Review)`},
		{[]string{"--session", "r", "--resume", "--from", "Build", "--decision", "true"}, "pass one or the other"},
		{[]string{"--session", "fresh", "--resume", "--from", "Build"}, "no stopped run"},
	} {
		bad := gateRun(t, run, tc.args...)
		if bad["kind"] != "invalid_from" || !strings.Contains(bad["error"].(string), tc.want) {
			t.Fatalf("%v: %v", tc.args, bad)
		}
	}
	if _, err := run("--from", "Build"); err == nil || !strings.Contains(err.Error(), "only applies with --resume") {
		t.Fatalf("--from without --resume: %v", err)
	}

	// The gate still answers normally afterwards.
	done := gateRun(t, run, "--session", "r", "--resume", "--decision", "true")
	if done["ok"] != true || done["paused"] == true {
		t.Fatalf("after the redo, the gate = %v", done)
	}
}
