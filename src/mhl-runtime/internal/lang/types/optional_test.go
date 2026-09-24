package types_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/lang/types"
)

func TestOptionalFieldResolvesFromShape(t *testing.T) {
	got, errs := aliasesOf(t, `type In = { diff: string, base?: string }`)
	if len(errs) != 0 {
		t.Fatalf("unexpected alias errors: %+v", errs)
	}
	in := got["In"]
	if in.IsOptional("diff") || !in.IsOptional("base") {
		t.Fatalf("want base optional and diff required, got Optional=%v", in.Optional)
	}
	if s := in.String(); s != "{base?: string, diff: string}" {
		t.Fatalf("String() = %q", s)
	}
}

func TestCheckOptionalField(t *testing.T) {
	in := types.ObjectOfOptional(
		map[string]types.Type{"diff": types.String, "base": types.String},
		map[string]bool{"base": true},
	)
	cases := []struct {
		name    string
		v       map[string]any
		wantErr string
	}{
		{"optional omitted", map[string]any{"diff": "x"}, ""},
		{"optional present", map[string]any{"diff": "x", "base": "main"}, ""},
		{"optional null", map[string]any{"diff": "x", "base": nil}, ""},
		{"optional present with wrong type", map[string]any{"diff": "x", "base": 1.0}, "in.base must be string"},
		{"required omitted", map[string]any{"base": "main"}, `missing field "diff"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := types.Check("in", in, tc.v)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("want error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestCheckTypeOptionalField(t *testing.T) {
	fields := map[string]types.Type{"a": types.String}
	required := types.ObjectOf(fields)
	optional := types.ObjectOfOptional(fields, map[string]bool{"a": true})

	if err := types.CheckType("x", optional, required); err != nil {
		t.Fatalf("a required field satisfies an optional one: %v", err)
	}
	if err := types.CheckType("x", required, optional); err == nil {
		t.Fatal("an optional field must not satisfy a required one")
	}
	if err := types.CheckType("x", optional, types.ObjectOf(map[string]types.Type{})); err != nil {
		t.Fatalf("an absent optional field is fine: %v", err)
	}
	if required.Equal(optional) {
		t.Fatal("Equal must distinguish optional from required")
	}
}

func TestJSONSchemaOptionalFieldNotRequired(t *testing.T) {
	in := types.ObjectOfOptional(
		map[string]types.Type{"diff": types.String, "base": types.String},
		map[string]bool{"base": true},
	)
	got := in.JSONSchema()["required"]
	if !reflect.DeepEqual(got, []string{"diff"}) {
		t.Fatalf("required = %v, want [diff]", got)
	}
}

func TestCheckNestedOptionalFields(t *testing.T) {
	got, errs := aliasesOf(t, `
type Finding = { file: string, line?: number }
type Out = { findings: Finding[], meta: { note?: string, id: string } }
`)
	if len(errs) != 0 {
		t.Fatalf("alias errors: %+v", errs)
	}
	out := got["Out"]
	ok := map[string]any{
		"findings": []any{map[string]any{"file": "a"}, map[string]any{"file": "b", "line": 3.0}},
		"meta":     map[string]any{"id": "x"},
	}
	if err := types.Check("out", out, ok); err != nil {
		t.Fatalf("nested optionals omitted must pass: %v", err)
	}
	bad := map[string]any{
		"findings": []any{map[string]any{"file": "a", "line": "3"}},
		"meta":     map[string]any{"id": "x"},
	}
	if err := types.Check("out", out, bad); err == nil || !strings.Contains(err.Error(), "out.findings[0].line must be number") {
		t.Fatalf("a present nested optional is still type-checked, got %v", err)
	}
	missing := map[string]any{"findings": []any{}, "meta": map[string]any{"note": "n"}}
	if err := types.Check("out", out, missing); err == nil || !strings.Contains(err.Error(), `out.meta: missing field "id"`) {
		t.Fatalf("a nested required field is still required, got %v", err)
	}
}
