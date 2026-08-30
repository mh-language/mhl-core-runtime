package types_test

import (
	"reflect"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/lang/types"
)

func TestJSONSchemaScalars(t *testing.T) {
	cases := []struct {
		in   types.Type
		want map[string]any
	}{
		{types.Any, map[string]any{}},
		{types.String, map[string]any{"type": "string"}},
		{types.Number, map[string]any{"type": "number"}},
		{types.Bool, map[string]any{"type": "boolean"}},
		{types.Array, map[string]any{"type": "array"}},
		{types.Object, map[string]any{"type": "object", "additionalProperties": true}},
		{types.EnumType("Status"), map[string]any{"type": "string", "description": "mhl enum Status"}},
	}
	for _, c := range cases {
		if got := c.in.JSONSchema(); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s.JSONSchema() = %#v, want %#v", c.in, got, c.want)
		}
	}
}

func TestJSONSchemaShapedArray(t *testing.T) {
	got := types.ArrayOf(types.String).JSONSchema()
	want := map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("string[] schema = %#v, want %#v", got, want)
	}
}

func TestJSONSchemaShapedObject(t *testing.T) {
	got := types.ObjectOf(map[string]types.Type{
		"name": types.String,
		"age":  types.Number,
	}).JSONSchema()

	if got["type"] != "object" || got["additionalProperties"] != true {
		t.Fatalf("object schema base wrong: %#v", got)
	}
	req, _ := got["required"].([]string)
	want := []string{"age", "name"} // sorted
	if !reflect.DeepEqual(req, want) {
		t.Errorf("required = %#v, want %#v (every declared field, sorted)", req, want)
	}
	props, _ := got["properties"].(map[string]any)
	if !reflect.DeepEqual(props["name"], map[string]any{"type": "string"}) ||
		!reflect.DeepEqual(props["age"], map[string]any{"type": "number"}) {
		t.Errorf("properties wrong: %#v", props)
	}
}
