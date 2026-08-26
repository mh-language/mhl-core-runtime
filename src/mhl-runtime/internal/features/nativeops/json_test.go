package nativeops_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/features/nativeops"
)

func TestParseObjectProducesMapStringAny(t *testing.T) {
	got, err := nativeops.Parse(`{"a":1,"b":"x"}`)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	want := map[string]any{"a": 1.0, "b": "x"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v, want %#v", got, want)
	}
}

func TestParseArrayProducesSliceAny(t *testing.T) {
	got, err := nativeops.Parse(`[1,2,3]`)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	want := []any{1.0, 2.0, 3.0}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v, want %#v", got, want)
	}
}

func TestParseNestedStructuredOutputShape(t *testing.T) {
	got, err := nativeops.Parse(`{"type":"result","structured_output":{"message":"Pong! What can I help you with?"}}`)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	obj, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("got %T, want map[string]any", got)
	}
	structuredOutput, ok := obj["structured_output"].(map[string]any)
	if !ok {
		t.Fatalf("structured_output = %#v, want map[string]any", obj["structured_output"])
	}
	if structuredOutput["message"] != "Pong! What can I help you with?" {
		t.Errorf("message = %v", structuredOutput["message"])
	}
}

func TestParseNullReturnsNilNoError(t *testing.T) {
	got, err := nativeops.Parse("null")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got != nil {
		t.Errorf("got %#v, want nil", got)
	}
}

func TestParseScalars(t *testing.T) {
	cases := []struct {
		text string
		want any
	}{
		{"42", 42.0},
		{`"hi"`, "hi"},
		{"true", true},
	}
	for _, c := range cases {
		got, err := nativeops.Parse(c.text)
		if err != nil {
			t.Fatalf("Parse(%q): %v", c.text, err)
		}
		if got != c.want {
			t.Errorf("Parse(%q) = %#v, want %#v", c.text, got, c.want)
		}
	}
}

func TestParseInvalidJSONErrors(t *testing.T) {
	_, err := nativeops.Parse("{not json")
	if err == nil {
		t.Fatal("expected an error for malformed JSON")
	}
	if !strings.Contains(err.Error(), "json.parse:") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestParseEmptyStringErrors(t *testing.T) {
	_, err := nativeops.Parse("")
	if err == nil {
		t.Fatal("expected an error for an empty string")
	}
}

func TestStringifyRoundTripsThroughParse(t *testing.T) {
	original := map[string]any{
		"id":       1.0,
		"title":    "auth",
		"dependsOn": []any{2.0, 3.0},
		"passed":   false,
	}
	text, err := nativeops.Stringify(original)
	if err != nil {
		t.Fatalf("Stringify: %v", err)
	}
	got, err := nativeops.Parse(text)
	if err != nil {
		t.Fatalf("Parse(Stringify(...)): %v", err)
	}
	if !reflect.DeepEqual(got, original) {
		t.Errorf("got %#v, want %#v", got, original)
	}
}

func TestStringifyScalarsAndNil(t *testing.T) {
	cases := []struct {
		value any
		want  string
	}{
		{42.0, "42"},
		{"hi", `"hi"`},
		{true, "true"},
		{nil, "null"},
		{[]any{1.0, 2.0}, "[1,2]"},
	}
	for _, c := range cases {
		got, err := nativeops.Stringify(c.value)
		if err != nil {
			t.Fatalf("Stringify(%#v): %v", c.value, err)
		}
		if got != c.want {
			t.Errorf("Stringify(%#v) = %q, want %q", c.value, got, c.want)
		}
	}
}

func TestStringifyUnsupportedValueErrors(t *testing.T) {
	_, err := nativeops.Stringify(make(chan int))
	if err == nil {
		t.Fatal("expected an error for a value json.Marshal cannot encode")
	}
	if !strings.Contains(err.Error(), "json.stringify:") {
		t.Errorf("unexpected error: %v", err)
	}
}
