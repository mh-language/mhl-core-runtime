package nativeops

import (
	"encoding/json"
	"fmt"
	"strings"
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

// ParseLines decodes text as newline-delimited JSON (NDJSON) — the format
// streaming CLI agents (e.g. `claude ... --output-format stream-json`,
// `codex exec --json`) emit one progress/result event per line. A
// caller can't hand that whole blob to Parse: encoding/json only ever
// decodes the first top-level value and errors on whatever follows, so a
// multi-line stream needs each line decoded on its own. Each line is
// decoded independently and appended to the result in order; a line that
// isn't valid JSON on its own (blank, or plain non-JSON log text some
// CLIs interleave into the stream) is skipped rather than failing the
// whole call, since the caller has no way to filter those out beforehand.
func ParseLines(text string) ([]any, error) {
	lines := strings.Split(text, "\n")
	out := make([]any, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var v any
		if err := json.Unmarshal([]byte(line), &v); err != nil {
			continue
		}
		out = append(out, v)
	}
	return out, nil
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
