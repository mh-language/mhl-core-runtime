package runtime_test

import (
	"reflect"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/engine/runtime"
	"github.com/mh-language/mhl-core-runtime/internal/lang/parser"
	"github.com/mh-language/mhl-core-runtime/internal/lang/types"
)

func typedPipeline(t *testing.T) runtime.Pipeline {
	t.Helper()
	prog, err := parser.Parse(`
enum Level { Low, High }
type ReviewInput = { diff: string, base?: string, level: Level }
type ReviewOutput = { approved: bool, summary: string }

workflow Review(req: ReviewInput): ReviewOutput {
    step S { log(req.diff) }
    output: { approved: true, summary: "ok" }
}
`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	p, err := runtime.FindPipeline(prog, "Review")
	if err != nil {
		t.Fatalf("FindPipeline: %v", err)
	}
	return p
}

func TestPipelineFromASTTypedSignature(t *testing.T) {
	p := typedPipeline(t)
	if p.InputParam != "req" || p.InputTypeName != "ReviewInput" || p.OutputTypeName != "ReviewOutput" {
		t.Fatalf("signature not projected: param=%q in=%q out=%q", p.InputParam, p.InputTypeName, p.OutputTypeName)
	}
	var names []string
	for _, in := range p.Inputs {
		names = append(names, in.Name)
	}
	if !reflect.DeepEqual(names, []string{"base", "diff", "level"}) {
		t.Fatalf("inputs = %v, want the param type's fields sorted", names)
	}
	if p.Inputs[0].Required() || !p.Inputs[1].Required() {
		t.Fatalf("base must be optional and diff required: %+v", p.Inputs)
	}
	if !reflect.DeepEqual(p.Inputs[2].EnumVariants, []string{"Low", "High"}) {
		t.Fatalf("enum field variants = %v", p.Inputs[2].EnumVariants)
	}

	schema := p.InputSchema()
	if req, _ := schema["required"].([]string); !reflect.DeepEqual(req, []string{"diff", "level"}) {
		t.Errorf("inputSchema required = %v, want [diff level]", schema["required"])
	}
	if err := p.ValidateInputs(map[string]any{"diff": "x", "level": "Low"}); err != nil {
		t.Errorf("an omitted optional field must be admitted: %v", err)
	}

	out := p.OutputSchema()
	if out == nil || out["title"] != "ReviewOutput" || out["type"] != "object" {
		t.Fatalf("outputSchema = %#v", out)
	}
	if req, _ := out["required"].([]string); !reflect.DeepEqual(req, []string{"approved", "summary"}) {
		t.Errorf("outputSchema required = %v", out["required"])
	}
}

func TestOutputSchemaNilWhenUntyped(t *testing.T) {
	if s := (runtime.Pipeline{Name: "P"}).OutputSchema(); s != nil {
		t.Fatalf("untyped pipeline must advertise no outputSchema, got %#v", s)
	}
}

func TestBindInputs(t *testing.T) {
	p := typedPipeline(t)

	vars := map[string]any{}
	p.BindInputs(vars, map[string]any{"diff": "x", "decision": "yes"})
	want := map[string]any{"diff": "x", "base": nil, "level": nil}
	if !reflect.DeepEqual(vars["req"], want) {
		t.Fatalf("req = %#v, want %#v (every declared field present, omitted ones null)", vars["req"], want)
	}
	if vars["decision"] != "yes" || vars["diff"] != nil {
		t.Fatalf("an undeclared key binds top-level, a declared one only on req: %#v", vars)
	}

	// A resume that supplies only some fields keeps the checkpointed rest.
	p.BindInputs(vars, map[string]any{"base": "dev"})
	want = map[string]any{"diff": "x", "base": "dev", "level": nil}
	if !reflect.DeepEqual(vars["req"], want) {
		t.Fatalf("after partial rebind req = %#v, want %#v", vars["req"], want)
	}

	untyped := runtime.Pipeline{Name: "P", Inputs: []runtime.PipelineInputSpec{{Name: "diff", Type: types.String}}}
	vars = map[string]any{}
	untyped.BindInputs(vars, map[string]any{"diff": "x"})
	if !reflect.DeepEqual(vars, map[string]any{"diff": "x"}) {
		t.Fatalf("per-line form binds each input as its own var, got %#v", vars)
	}
}

func TestSchemasResolveNestedEnumsAndOptionals(t *testing.T) {
	prog, err := parser.Parse(`
enum Severity { Low, High }
type Finding = { file: string, severity: Severity, line?: number }
type In = { meta: { severity: Severity, note?: string } }
type Out = { findings: Finding[], worst?: Severity }

workflow W(req: In): Out {
    step S { log("x") }
    output: { findings: [] }
}
`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	p, err := runtime.FindPipeline(prog, "W")
	if err != nil {
		t.Fatal(err)
	}
	variants := []any{"Low", "High"}

	out := p.OutputSchema()
	props := out["properties"].(map[string]any)
	if !reflect.DeepEqual(props["worst"].(map[string]any)["enum"], variants) {
		t.Errorf("top-level enum field: %v", props["worst"])
	}
	item := props["findings"].(map[string]any)["items"].(map[string]any)
	if !reflect.DeepEqual(item["properties"].(map[string]any)["severity"].(map[string]any)["enum"], variants) {
		t.Errorf("enum inside array items: %v", item)
	}
	if !reflect.DeepEqual(item["required"], []string{"file", "severity"}) {
		t.Errorf("nested optional must not be required: %v", item["required"])
	}
	if !reflect.DeepEqual(out["required"], []string{"findings"}) {
		t.Errorf("top-level optional must not be required: %v", out["required"])
	}

	meta := p.InputSchema()["properties"].(map[string]any)["meta"].(map[string]any)
	if !reflect.DeepEqual(meta["properties"].(map[string]any)["severity"].(map[string]any)["enum"], variants) {
		t.Errorf("enum nested in an input field: %v", meta)
	}
	if !reflect.DeepEqual(meta["required"], []string{"severity"}) {
		t.Errorf("nested optional input field must not be required: %v", meta["required"])
	}
}
