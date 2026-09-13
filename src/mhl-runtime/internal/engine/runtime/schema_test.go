package runtime_test

import (
	"reflect"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/engine/runtime"
	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
	"github.com/mh-language/mhl-core-runtime/internal/lang/parser"
	"github.com/mh-language/mhl-core-runtime/internal/lang/types"
)

func mustParseExpr(t *testing.T, src string) *ast.Expr {
	t.Helper()
	e, err := parser.ParseExpr(src)
	if err != nil {
		t.Fatalf("ParseExpr(%q): %v", src, err)
	}
	return e
}

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

// A `input x: Type = expr` default drops the input from "required" and,
// when the default is itself a literal, surfaces it as the schema's
// "default" — the fix for MHL-Melhorias.md #8 (every input previously had
// to be supplied on every call, even ones a given action ignores).
func TestPipelineInputSchemaWithDefault(t *testing.T) {
	p := runtime.Pipeline{
		Name: "WorkItem",
		Inputs: []runtime.PipelineInputSpec{
			{Name: "action", Type: types.String},
			{Name: "item_type", Type: types.String, Default: mustParseExpr(t, `""`)},
		},
	}

	got := p.InputSchema()
	if req, _ := got["required"].([]string); !reflect.DeepEqual(req, []string{"action"}) {
		t.Errorf("required = %#v, want [action] (item_type has a default)", got["required"])
	}
	props, _ := got["properties"].(map[string]any)
	want := map[string]any{"type": "string", "default": ""}
	if !reflect.DeepEqual(props["item_type"], want) {
		t.Errorf("properties[item_type] = %#v, want %#v", props["item_type"], want)
	}
}

// A non-literal default (reads context.* or another expression lint can't
// fold) still makes the input optional; it just has no JSON-representable
// "default" to advertise.
func TestPipelineInputSchemaWithNonLiteralDefault(t *testing.T) {
	p := runtime.Pipeline{
		Name: "P",
		Inputs: []runtime.PipelineInputSpec{
			{Name: "session", Type: types.String, Default: mustParseExpr(t, `context.session_id`)},
		},
	}
	got := p.InputSchema()
	if _, hasRequired := got["required"]; hasRequired {
		t.Errorf("required = %#v, want no `required` key at all (sole input is defaulted)", got["required"])
	}
	props, _ := got["properties"].(map[string]any)
	if _, hasDefault := props["session"].(map[string]any)["default"]; hasDefault {
		t.Errorf("properties[session] = %#v, want no \"default\" key for a non-literal default", props["session"])
	}
}

func TestPipelineValidateInputsWithDefault(t *testing.T) {
	p := runtime.Pipeline{
		Name: "WorkItem",
		Inputs: []runtime.PipelineInputSpec{
			{Name: "action", Type: types.String},
			{Name: "item_type", Type: types.String, Default: mustParseExpr(t, `""`)},
		},
	}
	if err := p.ValidateInputs(map[string]any{"action": "list"}); err != nil {
		t.Errorf("ValidateInputs with item_type omitted = %v, want nil (it has a default)", err)
	}
	err := p.ValidateInputs(map[string]any{"item_type": "bug"})
	ie, ok := err.(*runtime.InvalidInputsError)
	if !ok {
		t.Fatalf("err = %T %v, want *InvalidInputsError", err, err)
	}
	if !reflect.DeepEqual(ie.Missing, []string{"action"}) {
		t.Errorf("Missing = %#v, want [action] (item_type is optional)", ie.Missing)
	}
}

// MHL-Melhorias.md #9: an enum-typed input previously projected onto
// InputSchema as a bare {"type": "string"} — Type.JSONSchema() has no
// access to the enum's declared variants, only its name. With
// EnumVariants set (FindPipeline now fills it in from the program's `enum`
// declaration), the schema advertises the closed value set as a real JSON
// Schema "enum" array.
func TestPipelineInputSchemaWithEnum(t *testing.T) {
	p := runtime.Pipeline{
		Name: "WorkItem",
		Inputs: []runtime.PipelineInputSpec{
			{Name: "action", Type: types.EnumType("ActionType"), EnumVariants: []string{"Create", "List", "Get", "Archive", "Usage"}},
		},
	}
	got := p.InputSchema()
	props, _ := got["properties"].(map[string]any)
	want := map[string]any{
		"type":        "string",
		"description": "mhl enum ActionType",
		"enum":        []any{"Create", "List", "Get", "Archive", "Usage"},
	}
	if !reflect.DeepEqual(props["action"], want) {
		t.Errorf("properties[action] = %#v, want %#v", props["action"], want)
	}
}

// An enum type with no matching `enum` declaration in the program (or no
// EnumVariants set at all) still gets Type.JSONSchema()'s bare fallback —
// no "enum" key — rather than an empty one.
func TestPipelineInputSchemaEnumWithoutVariantsOmitsEnumKey(t *testing.T) {
	p := runtime.Pipeline{
		Name: "P",
		Inputs: []runtime.PipelineInputSpec{
			{Name: "action", Type: types.EnumType("ActionType")},
		},
	}
	got := p.InputSchema()
	props, _ := got["properties"].(map[string]any)
	if _, hasEnum := props["action"].(map[string]any)["enum"]; hasEnum {
		t.Errorf("properties[action] = %#v, want no \"enum\" key", props["action"])
	}
}

// End-to-end: FindPipeline resolves a real `enum ActionType { ... }`
// declaration and fills PipelineInputSpec.EnumVariants in for an
// `input action: ActionType`, in declaration order.
func TestFindPipelineResolvesEnumInputVariants(t *testing.T) {
	prog, err := parser.Parse(`
enum ActionType { Create, List, Get, Archive, Usage }

pipeline WorkItem {
    input action: ActionType
    step S {}
}
`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	p, err := runtime.FindPipeline(prog, "")
	if err != nil {
		t.Fatalf("FindPipeline: %v", err)
	}
	if len(p.Inputs) != 1 {
		t.Fatalf("Inputs = %#v, want exactly one", p.Inputs)
	}
	want := []string{"Create", "List", "Get", "Archive", "Usage"}
	if !reflect.DeepEqual(p.Inputs[0].EnumVariants, want) {
		t.Errorf("Inputs[0].EnumVariants = %#v, want %#v", p.Inputs[0].EnumVariants, want)
	}
	schema := p.InputSchema()
	props, _ := schema["properties"].(map[string]any)
	action, _ := props["action"].(map[string]any)
	gotEnum, _ := action["enum"].([]any)
	if len(gotEnum) != len(want) {
		t.Fatalf("schema enum = %#v, want %v values", gotEnum, len(want))
	}
	for i, v := range want {
		if gotEnum[i] != v {
			t.Errorf("schema enum[%d] = %v, want %v", i, gotEnum[i], v)
		}
	}
}
