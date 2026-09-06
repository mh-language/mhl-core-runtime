package runtime_test

import (
	"context"
	"errors"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/engine/runtime"
)

func TestCheckpointableVarsAcceptsJSONNativeValues(t *testing.T) {
	ok := map[string]any{
		"s":      "x",
		"n":      float64(3),
		"i":      7,
		"b":      true,
		"nil":    nil,
		"arr":    []any{1, "two", map[string]any{"k": 3}},
		"obj":    map[string]any{"nested": []any{true, nil}},
		"anyMap": map[any]any{"k": "v"},
	}
	if err := runtime.CheckpointableVars(ok); err != nil {
		t.Fatalf("JSON-native vars rejected: %v", err)
	}
}

func TestCheckpointableVarsRejectsClosureLikeValues(t *testing.T) {
	cases := map[string]map[string]any{
		"top-level func": {"fn": func() {}},
		"func in array":  {"list": []any{1, func() {}}},
		"func in object": {"obj": map[string]any{"cb": func() int { return 1 }}},
		"channel":        {"ch": make(chan int)},
	}
	for name, vars := range cases {
		t.Run(name, func(t *testing.T) {
			err := runtime.CheckpointableVars(vars)
			if !errors.Is(err, runtime.ErrNonSerializableVar) {
				t.Fatalf("want ErrNonSerializableVar, got %v", err)
			}
		})
	}
}

// A run that assigns a non-serializable value to a pipeline-scoped variable
// fails at the checkpoint after that step — not silently, and not on a later
// --resume.
func TestRunFailsWhenPipelineVarIsNotCheckpointable(t *testing.T) {
	root := t.TempDir()
	p := parsePipeline(t)

	runner := runtime.NewRunner(root)
	_, err := runner.Run(context.Background(), p, nil, func(_ context.Context, step string, ctx *runtime.RunContext) error {
		ctx.Vars["callback"] = func() {} // a closure-like value in the pipeline vars
		return nil
	}, false)
	if !errors.Is(err, runtime.ErrNonSerializableVar) {
		t.Fatalf("want ErrNonSerializableVar from the checkpoint, got %v", err)
	}
}
