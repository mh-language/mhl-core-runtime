package runtime_test

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/engine/runtime"
	"github.com/mh-language/mhl-core-runtime/internal/lang/parser"
	"github.com/mh-language/mhl-core-runtime/internal/lang/types"
)

// stageInputPipeline: A (no stage), then stage S of B and C, which asks for
// `plan`; `note` has a default and never pauses.
func stageInputPipeline(t *testing.T) runtime.Pipeline {
	t.Helper()
	return runtime.Pipeline{
		Name:  "W",
		Steps: []string{"A", "B", "C"},
		Inputs: []runtime.PipelineInputSpec{
			{Name: "who", Type: types.String},
			{Name: "plan", Type: types.String, Stage: "S"},
			{Name: "note", Type: types.String, Stage: "S", Default: mustParseExpr(t, `"-"`)},
		},
		StageOf:    map[string]string{"B": "S", "C": "S"},
		StageEntry: map[string]string{"S": "B"},
		Checkpoint: runtime.CheckpointConfig{Enabled: true, Strategy: "per_step"},
	}
}

func TestStageInputsLeftOutOfTheStartContract(t *testing.T) {
	p := stageInputPipeline(t)
	if err := p.ValidateInputs(map[string]any{"who": "a"}); err != nil {
		t.Fatalf("ValidateInputs = %v, want nil (stage inputs are asked for later)", err)
	}
	s := p.InputSchema()
	if req, _ := s["required"].([]string); !reflect.DeepEqual(req, []string{"who"}) {
		t.Errorf("required = %#v, want [who]", s["required"])
	}
	plan := s["properties"].(map[string]any)["plan"].(map[string]any)
	if plan["x-mhl-stage"] != "S" {
		t.Errorf("plan schema = %#v, want x-mhl-stage S", plan)
	}
	if got := p.MissingStageInputs("S", map[string]any{}); !reflect.DeepEqual(got, []string{"plan"}) {
		t.Errorf("MissingStageInputs = %v, want [plan]", got)
	}
	gate := p.StageInputSchema("S", []string{"plan"})
	if req, _ := gate["required"].([]string); !reflect.DeepEqual(req, []string{"plan"}) || len(gate["properties"].(map[string]any)) != 1 {
		t.Errorf("StageInputSchema = %#v", gate)
	}

	err := p.ValidateStageInputs("S", map[string]any{"bogus": 1}, map[string]any{"who": "a"})
	ie, ok := err.(*runtime.InvalidInputsError)
	if !ok || ie.Stage != "S" || !reflect.DeepEqual(ie.Missing, []string{"plan"}) || !reflect.DeepEqual(ie.Unknown, []string{"bogus"}) {
		t.Fatalf("ValidateStageInputs = %v", err)
	}
	if err := p.ValidateStageInputs("S", map[string]any{"plan": "pro"}, nil); err != nil {
		t.Fatalf("ValidateStageInputs with the input = %v", err)
	}
}

func TestRunnerPausesOnEnteringAStageWithUnboundInputs(t *testing.T) {
	p := stageInputPipeline(t)
	r := runtime.NewRunner(t.TempDir()).Session("s")
	var ran []string
	exec := func(_ context.Context, step string, rc *runtime.RunContext) error {
		ran = append(ran, step)
		return nil
	}

	res, err := r.Run(context.Background(), p, nil, exec, false)
	if err != nil {
		t.Fatal(err)
	}
	want := &runtime.InputGate{Step: "B", Stage: "S", Inputs: []string{"plan"}}
	if !res.Paused || !reflect.DeepEqual(res.PauseInputs, want) || !reflect.DeepEqual(ran, []string{"A"}) {
		t.Fatalf("res = %+v (inputs %+v), ran %v", res, res.PauseInputs, ran)
	}
	cp, found, err := r.Checkpoint("W")
	if err != nil || !found || !reflect.DeepEqual(cp.InputGate, want) || cp.NextStep != "B" {
		t.Fatalf("checkpoint = %+v, %v, %v", cp, found, err)
	}

	// Resuming with the input continues into the stage.
	ran = nil
	res, err = r.WithInputs(map[string]any{"plan": "pro"}).Run(context.Background(), p, nil, exec, true)
	if err != nil || res.Paused || !reflect.DeepEqual(ran, []string{"B", "C"}) {
		t.Fatalf("resume = %+v, %v, ran %v", res, err, ran)
	}
}

// A staged pipeline's input schema says in which order a run asks for its
// inputs: declaration order, and each named stage in run order with its own.
func TestInputSchemaCarriesTheJourney(t *testing.T) {
	p := stageInputPipeline(t)
	p.Stages = []runtime.Stage{{Name: "A", Steps: []string{"A"}}, {Name: "B", Steps: []string{"B"}}, {Name: "C", Steps: []string{"C"}}}
	s := p.InputSchema()
	if got := s["x-mhl-order"]; !reflect.DeepEqual(got, []string{"who", "plan", "note"}) {
		t.Errorf("x-mhl-order = %#v", got)
	}
	want := []any{map[string]any{"name": "S", "inputs": []string{"plan", "note"}}}
	if got := s["x-mhl-stages"]; !reflect.DeepEqual(got, want) {
		t.Errorf("x-mhl-stages = %#v, want %#v", got, want)
	}
	// Without named stages, the schema is unchanged.
	if _, ok := (runtime.Pipeline{Name: "P", Inputs: p.Inputs}).InputSchema()["x-mhl-stages"]; ok {
		t.Error("an unstaged pipeline carries no journey")
	}
}

func TestInputSchemaEnumDefault(t *testing.T) {
	p := runtime.Pipeline{
		Name: "P",
		Inputs: []runtime.PipelineInputSpec{
			{Name: "mode", Type: types.Type{Kind: types.EnumKind, Name: "Mode"}, EnumVariants: []string{"Existing", "Draft"}, Default: mustParseExpr(t, "Mode.Draft")},
		},
	}
	prop := p.InputSchema()["properties"].(map[string]any)["mode"].(map[string]any)
	if prop["default"] != "Draft" {
		t.Fatalf("mode schema = %#v, want default Draft", prop)
	}
}

func TestPauseStepsAndCanPause(t *testing.T) {
	src := `workflow W {
    step A { if (true) { pause("check") } }
    parallel P {
        step B { log(1) }
        step C { var d = pause("pick", decision: bool) }
        merge { log(2) }
    }
    step D { log(3) }
}
workflow Plain {
    step A { log(1) }
}`
	prog, err := parser.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	w, err := runtime.FindPipeline(prog, "W")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(w.PauseSteps, []string{"A", "C"}) || !w.CanPause() {
		t.Fatalf("PauseSteps = %v, CanPause = %v", w.PauseSteps, w.CanPause())
	}
	plain, _ := runtime.FindPipeline(prog, "Plain")
	if plain.PauseSteps != nil || plain.CanPause() {
		t.Fatalf("Plain: %v", plain.PauseSteps)
	}
	// A stage input without a default is a pause point too.
	if !stageInputPipeline(t).CanPause() {
		t.Fatal("a required stage input can pause the run")
	}
}

// An input's `///` doc is its "description" in the input schema — over an
// enum's "mhl enum X" marker — and is not part of the definition digest.
func TestInputDocIsTheSchemaDescriptionAndNotInTheDigest(t *testing.T) {
	src := `enum Mode { A, B }
workflow W {
    /// The folder to work in.
    input dir: string
    /// How to run.
    input mode: Mode = Mode.A
    input bare: string
    step S { log(dir) }
}`
	prog, err := parser.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	w, err := runtime.FindPipeline(prog, "W")
	if err != nil {
		t.Fatal(err)
	}
	props := w.InputSchema()["properties"].(map[string]any)
	if d := props["dir"].(map[string]any)["description"]; d != "The folder to work in." {
		t.Errorf("dir description = %v", d)
	}
	mode := props["mode"].(map[string]any)
	if mode["description"] != "How to run." || !reflect.DeepEqual(mode["enum"], []any{"A", "B"}) {
		t.Errorf("mode schema = %#v", mode)
	}
	if _, ok := props["bare"].(map[string]any)["description"]; ok {
		t.Errorf("an undocumented input has no description")
	}
	undocumented := strings.NewReplacer("    /// The folder to work in.\n", "", "    /// How to run.\n", "").Replace(src)
	if digestOfPipeline(t, src, "W") != digestOfPipeline(t, undocumented, "W") {
		t.Error("a /// doc must not change DefinitionDigest")
	}
}
