package runtime_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/engine/runtime"
	"github.com/mh-language/mhl-core-runtime/internal/lang/parser"
)

func digestOf(t *testing.T, src string) string {
	t.Helper()
	return digestOfPipeline(t, src, "Release")
}

func digestOfPipeline(t *testing.T, src, pipeline string) string {
	t.Helper()
	prog, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return runtime.DefinitionDigest(prog, pipeline)
}

const digestBaseSrc = `
pipeline Release {
    input env: string
    var url = ""
    step Build { url = "https://" + env }
    step Ship  { log("shipping " + url) }
}
`

func TestDefinitionDigestIsStableAndPositionInsensitive(t *testing.T) {
	a := digestOf(t, digestBaseSrc)
	if a != digestOf(t, digestBaseSrc) {
		t.Fatal("digest is not deterministic for the same source")
	}

	// Comments and blank lines are not part of the AST — digest must not move.
	withComments := `
// a leading comment
pipeline Release {
    input env: string

    var url = ""   // trailing comment

    step Build { url = "https://" + env }


    step Ship  { log("shipping " + url) }
}
`
	if got := digestOf(t, withComments); got != a {
		t.Fatalf("comment/whitespace-only edit changed the digest:\n base=%s\n edit=%s", a, got)
	}
}

func TestDefinitionDigestTracksLogicChanges(t *testing.T) {
	base := digestOf(t, digestBaseSrc)

	changedBody := digestOf(t, `
pipeline Release {
    input env: string
    var url = ""
    step Build { url = "http://" + env }
    step Ship  { log("shipping " + url) }
}
`)
	if changedBody == base {
		t.Fatal("a changed statement body did not change the digest")
	}

	extraStep := digestOf(t, `
pipeline Release {
    input env: string
    var url = ""
    step Build  { url = "https://" + env }
    step Verify { log("verify") }
    step Ship   { log("shipping " + url) }
}
`)
	if extraStep == base {
		t.Fatal("an added step did not change the digest")
	}
}

// Editing a *sibling* pipeline, or a test block, in the same file must not
// change a pipeline's digest — only its own body and the shared declarations
// it can reference count.
func TestDefinitionDigestIgnoresSiblingPipelinesAndTests(t *testing.T) {
	base := `
pipeline Release {
    var url = ""
    step Build { url = "x" }
}
pipeline Other {
    step A { log("v1") }
}
test t { describe d { are_equal(1, 1) } }
`
	edited := `
pipeline Release {
    var url = ""
    step Build { url = "x" }
}
pipeline Other {
    step A { log("v2 CHANGED") }
    step B { log("added") }
}
test t { describe d { are_equal(2, 2) } }
`
	if digestOfPipeline(t, base, "Release") != digestOfPipeline(t, edited, "Release") {
		t.Fatal("editing a sibling pipeline / test changed Release's digest")
	}
	// ...but the sibling's own digest does move.
	if digestOfPipeline(t, base, "Other") == digestOfPipeline(t, edited, "Other") {
		t.Fatal("Other's own digest should reflect its changed body")
	}
}

// A shared declaration the pipeline can reference (an agent) is still in the
// digest.
func TestDefinitionDigestIncludesSharedDeclarations(t *testing.T) {
	base := `
agent Claude { command: "claude" args: ["-p"] }
pipeline Release {
    step Build { var r = Claude.run(prompt: "hi") }
}
`
	edited := `
agent Claude { command: "claude" args: ["-p", "--json"] }
pipeline Release {
    step Build { var r = Claude.run(prompt: "hi") }
}
`
	if digestOfPipeline(t, base, "Release") == digestOfPipeline(t, edited, "Release") {
		t.Fatal("changing a referenced agent did not change the pipeline digest")
	}
}

// A resume must be refused when the pipeline definition has changed shape
// since the checkpoint was written.
func TestResumeRefusesDefinitionDigestMismatch(t *testing.T) {
	root := t.TempDir()
	p := parsePipeline(t)

	runner := runtime.NewRunner(root).WithDefinition("digest-of-the-original")
	_, err := runner.Run(context.Background(), p, nil, func(_ context.Context, step string, ctx *runtime.RunContext) error {
		if step == "RefinementLoop" {
			return errSimulatedCrash
		}
		return nil
	}, false)
	if err == nil {
		t.Fatal("expected the simulated crash on the first run")
	}

	// Resume with a different definition digest → refused.
	changed := runtime.NewRunner(root).WithDefinition("digest-after-an-edit")
	_, err = changed.Run(context.Background(), p, nil, func(context.Context, string, *runtime.RunContext) error {
		return nil
	}, true)
	if !errors.Is(err, runtime.ErrCheckpointDefinitionMismatch) {
		t.Fatalf("want ErrCheckpointDefinitionMismatch, got %v", err)
	}

	// Resume with the matching digest → proceeds.
	same := runtime.NewRunner(root).WithDefinition("digest-of-the-original")
	res, err := same.Run(context.Background(), p, nil, func(context.Context, string, *runtime.RunContext) error {
		return nil
	}, true)
	if err != nil {
		t.Fatalf("matching-digest resume failed: %v", err)
	}
	if !res.Resumed {
		t.Fatal("expected Resumed=true for the matching-digest resume")
	}
}

// WithForceResume downgrades a definition-digest mismatch to a warning and
// lets the resume proceed.
func TestForceResumeOverridesDigestMismatch(t *testing.T) {
	root := t.TempDir()
	p := parsePipeline(t)

	runner := runtime.NewRunner(root).WithDefinition("original")
	if _, err := runner.Run(context.Background(), p, nil, func(_ context.Context, step string, ctx *runtime.RunContext) error {
		if step == "RefinementLoop" {
			return errSimulatedCrash
		}
		return nil
	}, false); err == nil {
		t.Fatal("expected the simulated crash on the first run")
	}

	var log strings.Builder
	forced := runtime.NewRunner(root).WithDefinition("edited").WithForceResume(true)
	forced.Out = &log
	res, err := forced.Run(context.Background(), p, nil, func(context.Context, string, *runtime.RunContext) error {
		return nil
	}, true)
	if err != nil {
		t.Fatalf("forced resume should proceed, got: %v", err)
	}
	if !res.Resumed {
		t.Fatal("expected Resumed=true under --force")
	}
	if !strings.Contains(log.String(), "changed pipeline definition") {
		t.Fatalf("expected a force warning on the output, got: %q", log.String())
	}
}

// The loop checkpoint carries the definition digest too: a --resume of an
// edited loop is refused at the LoopCheckpoint.Load, before any iteration —
// and --force downgrades it.
func TestLoopResumeChecksDefinitionDigest(t *testing.T) {
	root := t.TempDir()
	loopP := runtime.Pipeline{Name: "Cycle", Steps: []string{"Only"}, Loop: true}

	newLoop := func(digest string) *runtime.LoopRunner {
		lr := runtime.NewLoopRunner(root)
		lr.Runner.WithDefinition(digest)
		return lr
	}

	// Leg 1: pause on the 2nd iteration, written with digest "orig".
	calls := 0
	_, err := newLoop("orig").Run(context.Background(), loopP, nil,
		func(context.Context, string, *runtime.RunContext) error {
			calls++
			if calls == 2 {
				return &runtime.PauseSignal{Reason: "hold"}
			}
			return nil
		},
		func(string) (bool, error) { return false, nil }, false)
	if err != nil {
		t.Fatalf("leg1: %v", err)
	}

	// Resume with a different digest → refused.
	_, err = newLoop("edited").Run(context.Background(), loopP, nil,
		func(context.Context, string, *runtime.RunContext) error { return nil },
		func(string) (bool, error) { return true, nil }, true)
	if !errors.Is(err, runtime.ErrCheckpointDefinitionMismatch) {
		t.Fatalf("want ErrCheckpointDefinitionMismatch on loop resume, got %v", err)
	}

	// Resume with --force → warns and proceeds.
	var log strings.Builder
	lr := newLoop("edited")
	lr.Runner.WithForceResume(true)
	lr.Runner.Out = &log
	res, err := lr.Run(context.Background(), loopP, nil,
		func(context.Context, string, *runtime.RunContext) error { return nil },
		func(string) (bool, error) { return true, nil }, true)
	if err != nil {
		t.Fatalf("forced loop resume should proceed: %v", err)
	}
	if !res.Resumed || !strings.Contains(log.String(), "changed loop definition") {
		t.Fatalf("expected a forced resume with a warning; resumed=%v log=%q", res.Resumed, log.String())
	}
}

// A checkpoint written before the digest field existed (empty DefinitionDigest)
// still resumes — backward compatibility.
func TestResumeAllowsLegacyCheckpointWithoutDigest(t *testing.T) {
	root := t.TempDir()
	p := parsePipeline(t)

	if err := runtime.NewStore(root).Save(&runtime.Checkpoint{
		Pipeline:       p.Name,
		LastStep:       "AuditWithSkill",
		NextStep:       "RefinementLoop",
		CompletedSteps: []string{"AuditWithSkill"},
		Variables:      map[string]any{"x": "1"},
	}); err != nil {
		t.Fatal(err)
	}

	runner := runtime.NewRunner(root).WithDefinition("some-current-digest")
	res, err := runner.Run(context.Background(), p, nil, func(context.Context, string, *runtime.RunContext) error {
		return nil
	}, true)
	if err != nil {
		t.Fatalf("legacy checkpoint resume failed: %v", err)
	}
	if !res.Resumed {
		t.Fatal("expected a legacy checkpoint to resume")
	}
}
