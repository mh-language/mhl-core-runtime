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

func TestPipelineValidateInputs(t *testing.T) {
	p := runtime.Pipeline{
		Name: "DocPipeline",
		Inputs: []runtime.PipelineInputSpec{
			{Name: "repo", Type: types.String},
			{Name: "approved", Type: types.String},
		},
	}

	tests := []struct {
		name    string
		args    map[string]any
		ok      bool
		missing []string
		unknown []string
	}{
		{"all present", map[string]any{"repo": "r", "approved": "y"}, true, nil, nil},
		{"missing one", map[string]any{"approved": "y"}, false, []string{"repo"}, nil},
		{"missing all / nil", nil, false, []string{"approved", "repo"}, nil},
		{"undeclared key", map[string]any{"repo": "r", "approved": "y", "campoExtra": 123}, false, nil, []string{"campoExtra"}},
		{"missing and undeclared", map[string]any{"campoExtra": 1}, false, []string{"approved", "repo"}, []string{"campoExtra"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := p.ValidateInputs(tc.args)
			if tc.ok {
				if err != nil {
					t.Fatalf("ValidateInputs = %v, want nil", err)
				}
				return
			}
			ie, ok := err.(*runtime.InvalidInputsError)
			if !ok {
				t.Fatalf("err = %T %v, want *InvalidInputsError", err, err)
			}
			if !reflect.DeepEqual(ie.Missing, tc.missing) {
				t.Errorf("Missing = %#v, want %#v", ie.Missing, tc.missing)
			}
			if !reflect.DeepEqual(ie.Unknown, tc.unknown) {
				t.Errorf("Unknown = %#v, want %#v", ie.Unknown, tc.unknown)
			}
		})
	}
}

func TestPipelineValidateInputsNoInputsDeclared(t *testing.T) {
	p := runtime.Pipeline{Name: "P"}
	if err := p.ValidateInputs(nil); err != nil {
		t.Errorf("nil args against no declared inputs: %v", err)
	}
	if err := p.ValidateInputs(map[string]any{"x": 1}); err == nil {
		t.Error("undeclared key against a no-input pipeline should be rejected")
	}
}
