package cli_test

import (
	"strings"
	"testing"
)

const entryRouteSrc = `
enum Model { Claude, Codex, All }

workflow W {
    input model: Model = Model.Claude

    entry route Choose(model: Model) {
        Model.Claude -> ViaClaude
        Model.Codex -> ViaCodex
        _ -> fail("no branch for ${model}")
    }

    step First {
        log("First ran")
    }

    step ViaClaude {
        log("claude")
        complete()
    }

    step ViaCodex {
        log("codex")
        goto Choose(Model.Claude)
    }
}
`

// An entry route runs before any step, dispatching on the input named like
// its parameter; the first declared step only runs if something reaches it.
func TestEntryRouteStartsTheRun(t *testing.T) {
	cases := []struct {
		model, want string
		steps       []string
	}{
		{"Claude", "claude", []string{"Choose", "ViaClaude"}},
		// The entry route stays a reusable route: ViaCodex re-dispatches through it.
		{"Codex", "codex", []string{"Choose", "ViaCodex", "ViaClaude"}},
	}
	for _, c := range cases {
		t.Run(c.model, func(t *testing.T) {
			out, err := runParallel(t, entryRouteSrc, "--input", "model="+c.model)
			if err != nil {
				t.Fatalf("run: %v\n%s", err, out)
			}
			if strings.Contains(out, "First ran") {
				t.Fatalf("the first declared step ran, the entry route should have skipped it:\n%s", out)
			}
			if !strings.Contains(out, c.want) {
				t.Fatalf("want %q in:\n%s", c.want, out)
			}
			var got []string
			for _, line := range strings.Split(out, "\n") {
				if name, ok := strings.CutPrefix(line, "step: "); ok {
					got = append(got, name)
				}
			}
			if strings.Join(got, ",") != strings.Join(c.steps, ",") {
				t.Fatalf("steps = %v, want %v\n%s", got, c.steps, out)
			}
		})
	}
}

// The wildcard's fail() fails the run at the route's own stage.
func TestEntryRouteFailArm(t *testing.T) {
	out, err := runParallel(t, entryRouteSrc, "--input", "model=All")
	if err == nil {
		t.Fatalf("want a failure, got:\n%s", out)
	}
	if !strings.Contains(err.Error(), `step "Choose" failed: no branch for All`) {
		t.Fatalf("unexpected error: %v", err)
	}
}
