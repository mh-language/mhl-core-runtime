package types

import (
	"encoding/json"
	"fmt"
	"strconv"
)

// Coerce converts raw — always a plain string, since that's all a CLI flag or
// an OS environment ever hands over — toward declared. This is the one place
// a typed boundary's value legitimately starts life with no representation of
// its own; everywhere else (tool params, pipeline inputs) the value already came
// out of the expression evaluator with a real Go type and gets Check'd, never
// Coerce'd. Any and String pass raw through unchanged. Array/Object attempt a
// JSON decode (`--input tags='["a","b"]'`) so a structured input is still
// expressible from the command line without inventing a bespoke CLI syntax.
func Coerce(label string, declared Type, raw string) (any, error) {
	switch declared.Kind {
	case AnyKind, StringKind:
		return raw, nil
	case NumberKind:
		n, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return nil, fmt.Errorf("%s: %q is not a valid number", label, raw)
		}
		return n, nil
	case BoolKind:
		b, err := strconv.ParseBool(raw)
		if err != nil {
			return nil, fmt.Errorf("%s: %q is not a valid bool (use true/false)", label, raw)
		}
		return b, nil
	case ArrayKind, ObjectKind:
		var v any
		if err := json.Unmarshal([]byte(raw), &v); err != nil {
			return nil, fmt.Errorf("%s: %q is not valid JSON for a %s value", label, raw, declared)
		}
		// Check already recurses element/field-wise, so a shaped declared
		// (string[], {name: string, ...}) gets deep validation here for
		// free — no extra logic needed beyond the Kind-switch fix itself.
		if err := Check(label, declared, v); err != nil {
			return nil, err
		}
		return v, nil
	}
	return raw, nil
}
