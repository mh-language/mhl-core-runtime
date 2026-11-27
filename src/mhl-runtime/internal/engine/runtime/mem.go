package runtime

import "strings"

// MemVarPrefix marks a run variable that holds a `mem` declaration's value:
// `mem count = 0` lives in RunContext.Vars under "__mem:count". Keeping `mem`
// in the run's own variables is what makes it travel with the run's state —
// it is checkpointed after every step with the `var`s (so a failed step's
// `mem` writes roll back exactly like its `var` writes, and a resume
// continues from the last completed step), it goes wherever the checkpoint
// goes (the session directory, or an `extension state` shared by replicas),
// and it expires with the session. A loop carries these keys from one
// iteration to the next (LoopRunner.Run, LoopCheckpoint.Mem), which is the
// one thing that sets a `mem` apart from a pipeline `var`.
//
// The "__" prefix keeps them out of a run's public result, like the
// runtime's other bookkeeping keys (__last_step); a parallel group's merge is
// the one place that treats them as ordinary variables (see
// runParallelStage).
const MemVarPrefix = "__mem:"

// MemVarKey is the RunContext.Vars key holding mem variable name.
func MemVarKey(name string) string { return MemVarPrefix + name }

// MemVarName reports the mem variable name key holds, if key is a mem key.
func MemVarName(key string) (string, bool) {
	if !strings.HasPrefix(key, MemVarPrefix) {
		return "", false
	}
	return key[len(MemVarPrefix):], true
}

// MemVars returns the mem keys of vars (still prefixed), or nil when there
// are none. The values are shared, not copied.
func MemVars(vars map[string]any) map[string]any {
	var out map[string]any
	for k, v := range vars {
		if strings.HasPrefix(k, MemVarPrefix) {
			if out == nil {
				out = map[string]any{}
			}
			out[k] = v
		}
	}
	return out
}

// bookkeepingKey reports whether a run variable is runtime bookkeeping a
// parallel group's merge ignores (e.g. __last_step) — every "__" key except
// a mem variable's.
func bookkeepingKey(k string) bool {
	return strings.HasPrefix(k, "__") && !strings.HasPrefix(k, MemVarPrefix)
}

// varLabel names run variable k for an error message: `mem "count"` for a
// mem variable, `pipeline var "x"` for anything else.
func varLabel(k string) (label, name string) {
	if n, ok := MemVarName(k); ok {
		return `mem "` + n + `"`, n
	}
	return `pipeline var "` + k + `"`, k
}
