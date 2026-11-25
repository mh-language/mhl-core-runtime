package interpreter

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestNewSpawnSemDefaults(t *testing.T) {
	if got := cap(NewSpawnSem(0).shared); got != defaultSpawnConcurrency {
		t.Errorf("NewSpawnSem(0) cap = %d, want %d", got, defaultSpawnConcurrency)
	}
	if got := cap(NewSpawnSem(-5).shared); got != defaultSpawnConcurrency {
		t.Errorf("NewSpawnSem(-5) cap = %d, want %d", got, defaultSpawnConcurrency)
	}
	if got := cap(NewSpawnSem(7).shared); got != 7 {
		t.Errorf("NewSpawnSem(7) cap = %d, want 7", got)
	}
}

// A statement's own `concurrency N` replaces the default run-wide limit but
// not an explicitly declared `spawn: { max_concurrency }` ceiling.
func TestSpawnSemSlots(t *testing.T) {
	local := make(chan struct{}, 2)

	def := NewSpawnSem(0)
	if got := def.slots(nil); len(got) != 1 || got[0] != def.shared {
		t.Errorf("default, no clause: slots = %v, want [shared]", got)
	}
	if got := def.slots(local); len(got) != 1 || got[0] != local {
		t.Errorf("default, with clause: slots = %v, want [local] only", got)
	}

	declared := NewSpawnSem(3)
	if got := declared.slots(nil); len(got) != 1 || got[0] != declared.shared {
		t.Errorf("declared, no clause: slots = %v, want [shared]", got)
	}
	if got := declared.slots(local); len(got) != 2 || got[0] != local || got[1] != declared.shared {
		t.Errorf("declared, with clause: slots = %v, want [local shared]", got)
	}
}

func TestDeepCopyValueIsolatesNestedStructures(t *testing.T) {
	orig := map[string]any{
		"list": []any{"a", map[string]any{"k": "v"}},
	}
	cp := deepCopyValue(orig).(map[string]any)

	orig["list"].([]any)[0] = "MUTATED"
	orig["list"].([]any)[1].(map[string]any)["k"] = "MUTATED"

	if got := cp["list"].([]any)[0]; got != "a" {
		t.Errorf("nested slice element leaked mutation: %v", got)
	}
	if got := cp["list"].([]any)[1].(map[string]any)["k"]; got != "v" {
		t.Errorf("nested map value leaked mutation: %v", got)
	}
}

func TestHandleFieldBeforeAndAfterFinish(t *testing.T) {
	h := &spawnHandle{agentName: "A", done: make(chan struct{}), started: time.Now()}

	if v, _ := handleField(h, "status"); v != "pending" {
		t.Errorf("pre-finish status = %v, want pending", v)
	}
	if v, _ := handleField(h, "ok"); v != false {
		t.Errorf("pre-finish ok = %v, want false", v)
	}

	h.finish("the answer", nil)
	if v, _ := handleField(h, "status"); v != "done" {
		t.Errorf("post-finish status = %v, want done", v)
	}
	if v, _ := handleField(h, "result"); v != "the answer" {
		t.Errorf("result = %v", v)
	}
	if v, _ := handleField(h, "ok"); v != true {
		t.Errorf("ok = %v, want true", v)
	}
	if _, err := handleField(h, "bogus"); err == nil {
		t.Errorf("expected an error for an unknown field")
	}
}

func TestHandleFieldStatusReflectsCancellation(t *testing.T) {
	h := &spawnHandle{agentName: "A", done: make(chan struct{}), started: time.Now()}
	h.finish("", context.Canceled)
	if v, _ := handleField(h, "status"); v != "cancelled" {
		t.Errorf("status = %v, want cancelled", v)
	}
	if v, _ := handleField(h, "ok"); v != false {
		t.Errorf("ok = %v, want false", v)
	}
	if v, _ := handleField(h, "error"); v != context.Canceled.Error() {
		t.Errorf("error = %v", v)
	}
}

func TestFinishIsIdempotent(t *testing.T) {
	h := &spawnHandle{agentName: "A", done: make(chan struct{}), started: time.Now()}
	h.finish("first", nil)
	h.finish("second", errors.New("late"))
	if h.result != "first" || h.err != nil {
		t.Errorf("second finish should be ignored: result=%q err=%v", h.result, h.err)
	}
}
