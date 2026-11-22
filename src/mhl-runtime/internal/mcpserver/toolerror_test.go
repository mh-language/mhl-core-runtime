package mcpserver

import (
	"errors"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/engine/runtime"
)

// An input of the wrong type is reported like a missing one — kind
// invalid_inputs — naming it under `invalid`, so a client can branch on it.
func TestToolErrorResultNamesAnInputOfTheWrongType(t *testing.T) {
	err := &runtime.InputValueError{Pipeline: "W", Input: "cmd", Type: "string[]", Err: errors.New(`input "cmd": "ls" is not valid JSON for a string[] value`)}
	r := toolErrorResult(err)
	sc, _ := r["structuredContent"].(map[string]any)
	if r["isError"] != true || sc["kind"] != "invalid_inputs" {
		t.Fatalf("result = %v", r)
	}
	if inv, _ := sc["invalid"].([]string); len(inv) != 1 || inv[0] != "cmd" {
		t.Fatalf("invalid = %v", sc["invalid"])
	}
}
