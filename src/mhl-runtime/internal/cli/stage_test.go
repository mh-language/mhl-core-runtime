package cli_test

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
