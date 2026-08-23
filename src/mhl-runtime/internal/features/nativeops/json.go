package nativeops

import (
	"encoding/json"
	"fmt"
)

// Parse decodes text as JSON into MHL's native value representation:
// object → map[string]any, array → []any, number → float64, string →
// string, bool → bool, null → nil — exactly the shapes evalPrimary already
// produces for {...}/[...] literals (internal/engine/interpreter/eval.go),
// so the result needs no translation before `.field`/`[i]` access, keys(),
// values(), or size() can be used on it.
func Parse(text string) (any, error) {
	var v any
	if err := json.Unmarshal([]byte(text), &v); err != nil {
		return nil, fmt.Errorf("json.parse: %w", err)
	}
	return v, nil
}

// Stringify encodes an already-evaluated MHL value (the same shapes Parse
// produces: map[string]any, []any, float64, string, bool, nil) back into
// JSON text — the inverse of Parse, needed to persist a value a pipeline
// built or mutated in memory (e.g. a feature list) back to disk via
// fs.write.
func Stringify(v any) (string, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("json.stringify: %w", err)
	}
	return string(raw), nil
}
