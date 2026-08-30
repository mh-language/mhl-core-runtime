package types

import "testing"

func TestParse(t *testing.T) {
	cases := []struct {
		name   string
		want   Type
		wantOk bool
	}{
		{"", Any, true},
		{"any", Any, true},
		{"string", String, true},
		{"number", Number, true},
		{"int", Number, true},
		{"integer", Number, true},
		{"float", Number, true},
		{"bool", Bool, true},
		{"boolean", Bool, true},
		{"array", Array, true},
		{"object", Object, true},
		{"sting", Type{}, false},
		{"Number", Type{}, false},
	}
	for _, c := range cases {
		got, ok := Parse(c.name)
		if ok != c.wantOk {
			t.Errorf("Parse(%q) ok = %v, want %v", c.name, ok, c.wantOk)
			continue
		}
		if ok && !got.Equal(c.want) {
			t.Errorf("Parse(%q) = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestOf(t *testing.T) {
	cases := []struct {
		name   string
		v      any
		want   Type
		wantOk bool
	}{
		{"nil", nil, Any, true},
		{"string", "hi", String, true},
		{"number", 3.5, Number, true},
		{"bool", true, Bool, true},
		{"array", []any{1.0, 2.0}, Array, true},
		{"object", map[string]any{"a": 1.0}, Object, true},
		{"unrecognized", struct{}{}, Any, false},
	}
	for _, c := range cases {
		got, ok := Of(c.v)
		if ok != c.wantOk {
			t.Errorf("Of(%s) ok = %v, want %v", c.name, ok, c.wantOk)
			continue
		}
		if ok && !got.Equal(c.want) {
			t.Errorf("Of(%s) = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestCheck(t *testing.T) {
	cases := []struct {
		name     string
		declared Type
		v        any
		wantErr  bool
	}{
		{"any accepts string", Any, "hi", false},
		{"any accepts number", Any, 1.0, false},
		{"any accepts nil", Any, nil, false},
		{"string matches", String, "hi", false},
		{"string mismatch number", String, 1.0, true},
		{"number matches", Number, 1.0, false},
		{"number mismatch string", Number, "1", true},
		{"bool matches", Bool, true, false},
		{"array matches", Array, []any{}, false},
		{"object matches", Object, map[string]any{}, false},
		{"nil satisfies string", String, nil, false},
		{"unrecognized go value", String, struct{}{}, true},
		{"unshaped array accepts mixed elements", Array, []any{"a", 1.0}, false},
		{"shaped array all elements match", ArrayOf(String), []any{"a", "b"}, false},
		{"shaped array one element wrong type", ArrayOf(String), []any{"a", 1.0}, true},
		{"nested shaped array valid", ArrayOf(ArrayOf(Number)), []any{[]any{1.0, 2.0}}, false},
		{"nested shaped array invalid", ArrayOf(ArrayOf(Number)), []any{[]any{1.0, "x"}}, true},
		{"unshaped object accepts anything", Object, map[string]any{"whatever": "x"}, false},
		{
			"shaped object all fields match",
			ObjectOf(map[string]Type{"name": String, "age": Number}),
			map[string]any{"name": "Ana", "age": 30.0},
			false,
		},
		{
			"shaped object extra field is allowed (structural, not exact)",
			ObjectOf(map[string]Type{"name": String}),
			map[string]any{"name": "Ana", "email": "a@b.com"},
			false,
		},
		{
			"shaped object missing declared field",
			ObjectOf(map[string]Type{"name": String, "age": Number}),
			map[string]any{"name": "Ana"},
			true,
		},
		{
			"shaped object field wrong type",
			ObjectOf(map[string]Type{"age": Number}),
			map[string]any{"age": "thirty"},
			true,
		},
		{
			"nested object shape valid",
			ObjectOf(map[string]Type{"meta": ObjectOf(map[string]Type{"active": Bool})}),
			map[string]any{"meta": map[string]any{"active": true}},
			false,
		},
		{
			"nested object shape invalid",
			ObjectOf(map[string]Type{"meta": ObjectOf(map[string]Type{"active": Bool})}),
			map[string]any{"meta": map[string]any{"active": "yes"}},
			true,
		},
	}
	for _, c := range cases {
		err := Check("label", c.declared, c.v)
		if (err != nil) != c.wantErr {
			t.Errorf("%s: Check err = %v, wantErr %v", c.name, err, c.wantErr)
		}
	}
}

func TestCheckType(t *testing.T) {
	cases := []struct {
		name     string
		declared Type
		actual   Type
		wantErr  bool
	}{
		{"any declared accepts anything", Any, String, false},
		{"any actual is never a mismatch", String, Any, false},
		{"any/any", Any, Any, false},
		{"matching types", String, String, false},
		{"mismatched types", String, Number, true},
		{"mismatched bool/array", Bool, Array, true},
		{"unshaped vs shaped array is never flagged (can't prove it)", ArrayOf(String), Array, false},
		{"shaped vs unshaped array is never flagged (can't prove it)", Array, ArrayOf(String), false},
		{"matching shaped arrays", ArrayOf(String), ArrayOf(String), false},
		{"mismatched shaped arrays", ArrayOf(String), ArrayOf(Number), true},
		{"unshaped vs shaped object is never flagged (can't prove it)", ObjectOf(map[string]Type{"a": String}), Object, false},
		{
			"matching shaped objects",
			ObjectOf(map[string]Type{"name": String}),
			ObjectOf(map[string]Type{"name": String}),
			false,
		},
		{
			"shaped objects missing field",
			ObjectOf(map[string]Type{"name": String, "age": Number}),
			ObjectOf(map[string]Type{"name": String}),
			true,
		},
	}
	for _, c := range cases {
		err := CheckType("label", c.declared, c.actual)
		if (err != nil) != c.wantErr {
			t.Errorf("%s: CheckType err = %v, wantErr %v", c.name, err, c.wantErr)
		}
	}
}

func TestEqual(t *testing.T) {
	cases := []struct {
		name string
		a, b Type
		want bool
	}{
		{"same flat kind", String, String, true},
		{"different flat kind", String, Number, false},
		{"unshaped array equals unshaped array", Array, Array, true},
		{"unshaped array never equals shaped array", Array, ArrayOf(String), false},
		{"shaped array never equals unshaped array", ArrayOf(String), Array, false},
		{"matching shaped arrays", ArrayOf(String), ArrayOf(String), true},
		{"mismatched shaped arrays", ArrayOf(String), ArrayOf(Number), false},
		{"nested shaped arrays match", ArrayOf(ArrayOf(String)), ArrayOf(ArrayOf(String)), true},
		{"unshaped object equals unshaped object", Object, Object, true},
		{"unshaped object never equals shaped object", Object, ObjectOf(map[string]Type{"a": String}), false},
		{
			"matching shaped objects",
			ObjectOf(map[string]Type{"a": String, "b": Number}),
			ObjectOf(map[string]Type{"a": String, "b": Number}),
			true,
		},
		{
			"shaped objects with different field count",
			ObjectOf(map[string]Type{"a": String}),
			ObjectOf(map[string]Type{"a": String, "b": Number}),
			false,
		},
		{
			"shaped objects with same field count, different types",
			ObjectOf(map[string]Type{"a": String}),
			ObjectOf(map[string]Type{"a": Number}),
			false,
		},
	}
	for _, c := range cases {
		if got := c.a.Equal(c.b); got != c.want {
			t.Errorf("%s: %v.Equal(%v) = %v, want %v", c.name, c.a, c.b, got, c.want)
		}
	}
}

func TestCoerce(t *testing.T) {
	cases := []struct {
		name     string
		declared Type
		raw      string
		want     any
		wantErr  bool
	}{
		{"any passthrough", Any, "hello", "hello", false},
		{"string passthrough", String, "hello", "hello", false},
		{"number valid", Number, "5", 5.0, false},
		{"number invalid", Number, "abc", nil, true},
		{"bool valid true", Bool, "true", true, false},
		{"bool valid false", Bool, "false", false, false},
		{"bool invalid", Bool, "yes", nil, true},
		{"array valid", Array, `["a","b"]`, []any{"a", "b"}, false},
		{"array invalid json", Array, `not json`, nil, true},
		{"array wrong shape", Array, `{"a":1}`, nil, true},
		{"object valid", Object, `{"a":1}`, map[string]any{"a": 1.0}, false},
		{"object invalid json", Object, `not json`, nil, true},
		{"object wrong shape", Object, `["a"]`, nil, true},
		{"shaped array valid", ArrayOf(String), `["a","b"]`, []any{"a", "b"}, false},
		{"shaped array wrong element type", ArrayOf(String), `["a",1]`, nil, true},
		{
			"shaped object valid",
			ObjectOf(map[string]Type{"name": String, "age": Number}),
			`{"name":"Ana","age":30}`,
			map[string]any{"name": "Ana", "age": 30.0},
			false,
		},
		{
			"shaped object missing field",
			ObjectOf(map[string]Type{"name": String, "age": Number}),
			`{"name":"Ana"}`,
			nil,
			true,
		},
		{
			"shaped object extra field allowed",
			ObjectOf(map[string]Type{"name": String}),
			`{"name":"Ana","email":"a@b.com"}`,
			map[string]any{"name": "Ana"},
			false,
		},
	}
	for _, c := range cases {
		got, err := Coerce("label", c.declared, c.raw)
		if (err != nil) != c.wantErr {
			t.Errorf("%s: Coerce err = %v, wantErr %v", c.name, err, c.wantErr)
			continue
		}
		if err != nil {
			continue
		}
		switch want := c.want.(type) {
		case []any:
			gotArr, ok := got.([]any)
			if !ok || len(gotArr) != len(want) {
				t.Errorf("%s: Coerce = %v, want %v", c.name, got, want)
				continue
			}
			for i := range want {
				if gotArr[i] != want[i] {
					t.Errorf("%s: Coerce[%d] = %v, want %v", c.name, i, gotArr[i], want[i])
				}
			}
		case map[string]any:
			gotMap, ok := got.(map[string]any)
			if !ok {
				t.Errorf("%s: Coerce = %v, want map", c.name, got)
				continue
			}
			for k, v := range want {
				if gotMap[k] != v {
					t.Errorf("%s: Coerce[%q] = %v, want %v", c.name, k, gotMap[k], v)
				}
			}
		default:
			if got != c.want {
				t.Errorf("%s: Coerce = %v, want %v", c.name, got, c.want)
			}
		}
	}
}
