package execsvc

import (
	"fmt"

	"github.com/mh-language/mhl-core-runtime/internal/engine/runtime"
	"github.com/mh-language/mhl-core-runtime/internal/lang/types"
)

// coerceInputs type-checks inputs against a pipeline's declared `input name:
// Type` set: a string value (the `mhl run --input k=v` / env path) is coerced
// toward its declared type, a non-string value (the JSON path an MCP/A2A caller
// takes) is type-checked as-is. It is pure — no session, no state, no agent or
// tool — so Inspect (dry-run) and Run share the exact same check.
//
// Errors are collected in declaration order rather than returned on the first
// one, so a dry-run can list every bad input at once; Run uses errs[0]. Keys
// the pipeline does not declare are passed straight through — ValidateInputs is
// what flags those, and second-guessing their type here would just duplicate
// (or contradict) that error.
func coerceInputs(pipeline runtime.Pipeline, inputs map[string]any) (coerced map[string]any, errs []error) {
	declared := make(map[string]types.Type, len(pipeline.Inputs))
	for _, in := range pipeline.Inputs {
		declared[in.Name] = in.Type
	}
	coerced = make(map[string]any, len(inputs))

	for _, in := range pipeline.Inputs { // declaration order → deterministic errs
		v, ok := inputs[in.Name]
		if !ok {
			continue
		}
		cv, err := coerceOne(fmt.Sprintf("input %q", in.Name), in.Type, v)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		coerced[in.Name] = cv
	}
	for k, v := range inputs {
		if _, isDeclared := declared[k]; !isDeclared {
			coerced[k] = v
		}
	}
	return coerced, errs
}

func coerceOne(label string, dt types.Type, v any) (any, error) {
	if raw, ok := v.(string); ok {
		return types.Coerce(label, dt, raw)
	}
	if err := types.Check(label, dt, v); err != nil {
		return nil, err
	}
	return v, nil
}
