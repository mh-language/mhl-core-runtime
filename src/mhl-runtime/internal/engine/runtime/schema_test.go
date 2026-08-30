package runtime_test

import (
	"reflect"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/engine/runtime"
	"github.com/mh-language/mhl-core-runtime/internal/lang/types"
)

func TestPipelineInputSchema(t *testing.T) {
	p := runtime.Pipeline{
		Name: "Review",
		Inputs: []runtime.PipelineInputSpec{
			{Name: "diff", Type: types.String},
			{Name: "budget", Type: types.Number},
		},
	}

	got := p.InputSchema()
	if got["type"] != "object" || got["additionalProperties"] != false {
		t.Fatalf("base schema wrong: %#v", got)
	}
	if req, _ := got["required"].([]string); !reflect.DeepEqual(req, []string{"budget", "diff"}) {
		t.Errorf("required = %#v, want [budget diff] (sorted, all declared inputs)", got["required"])
	}
	props, _ := got["properties"].(map[string]any)
	if !reflect.DeepEqual(props["diff"], map[string]any{"type": "string"}) ||
		!reflect.DeepEqual(props["budget"], map[string]any{"type": "number"}) {
		t.Errorf("properties wrong: %#v", props)
	}
}

func TestPipelineInputSchemaNoInputs(t *testing.T) {
	got := runtime.Pipeline{Name: "P"}.InputSchema()
	want := map[string]any{
		"type":                 "object",
		"properties":           map[string]any{},
		"additionalProperties": false,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("no-input schema = %#v, want %#v (no `required` key)", got, want)
	}
}
