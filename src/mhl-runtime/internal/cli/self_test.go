package cli_test

import (
	"strings"
	"testing"
)

// TestSelfReadsAndWritesPipelineScopeBypassingStepShadowing is self.name's
// core contract: it always means the pipeline's own input/var/mem, even
// when a step-local `var` of the same name would otherwise shadow a bare
// reference — added specifically so a `partial` pipeline's fragments can
// signal "this name is shared pipeline state, not declared in this file"
// instead of a dev having to discover that by reading every fragment.
func TestSelfReadsAndWritesPipelineScopeBypassingStepShadowing(t *testing.T) {
	out, err := runTestFile(t, `
workflow Demo {
    input name: string
    var greeting = ""
    var shadowed = "pipeline-level"

    step Build {
        var shadowed = "step-local"
        self.greeting = "hello " + self.name
        if (shadowed != "step-local") { fail("bare name should read the step-local shadow") }
        if (self.shadowed != "pipeline-level") { fail("self.name should bypass the step-local shadow") }
        complete()
    }
}

test t {
    describe self_bypasses_shadowing {
        var result = Demo.run(inputs: {name: "ana"})
        is_true(result.ok)
        are_equal(result.vars.greeting, "hello ana")
    }
}
`)
	if err != nil {
		t.Fatalf("test: %v\noutput:\n%s", err, out)
	}
	if !strings.Contains(out, "2 passed, 0 failed, 0 incomplete") {
		t.Errorf("unexpected output:\n%s", out)
	}
}

// TestSelfOutsidePipelineStepFails: self.<name> (the property form, as
// opposed to self.method() inside a tool) is meaningless anywhere but a
// pipeline/workflow step — a describe block never sets
// interpreter.evalCtx.pipelineName, so it must raise a clear, catchable
// error rather than silently resolving to something.
func TestSelfOutsidePipelineStepFails(t *testing.T) {
	out, err := runTestFile(t, `
test t {
    describe self_needs_a_pipeline_step {
        var caught = false
        try {
            log(self.whatever)
        } catch (e) {
            caught = true
        }
        is_true(caught)
    }
}
`)
	if err != nil {
		t.Fatalf("test: %v\noutput:\n%s", err, out)
	}
	if !strings.Contains(out, "1 passed, 0 failed, 0 incomplete") {
		t.Errorf("unexpected output:\n%s", out)
	}
}

// TestSelfUndeclaredNameFails proves self.<name> doesn't silently resolve
// to nil for a typo — both a bad read and a bad write raise clearly.
func TestSelfUndeclaredNameFails(t *testing.T) {
	out, err := runTestFile(t, `
workflow Demo {
    var real_name = ""
    step Build {
        self.real_name = "ok"
        self.typo_name = "boom"
    }
}

test t {
    describe self_typo_fails {
        var result = Demo.run()
        is_false(result.ok)
        is_true(result.error.contains("typo_name"))
    }
}
`)
	if err != nil {
		t.Fatalf("test: %v\noutput:\n%s", err, out)
	}
	if !strings.Contains(out, "2 passed, 0 failed, 0 incomplete") {
		t.Errorf("unexpected output:\n%s", out)
	}
}

// TestSelfMethodCallStillDispatchesAlongsideSelfPropertyReads is the
// regression for a real bug caught only by testing against production
// code: self.<name> (property read/write, added for pipeline scope) and
// self.method(...) (the pre-existing tool-method call) look identical for
// their first trailer — both are "self" followed by a Member — and an
// early version of the property-read branch matched len(p.Ops) >= 1
// without excluding "and the next trailer is a call", so it hijacked
// self.method(...) calls too. A tool's own self.method() call, invoked
// from a pipeline step that also uses self.<name>, must keep dispatching
// to the sibling method, not fail with a pipeline-scope error.
func TestSelfMethodCallStillDispatchesAlongsideSelfPropertyReads(t *testing.T) {
	out, err := runTestFile(t, `
tool Ids {
    is_valid(id: string): bool -> id.size() > 0
    ensure_valid(id: string): string -> {
        if (!self.is_valid(id)) fail("invalid id")
        return id
    }
}

workflow Demo {
    input id: string
    var checked_id = ""
    step Build {
        self.checked_id = Ids.ensure_valid(self.id)
        complete()
    }
}

test t {
    describe self_method_call_and_self_property_coexist {
        var result = Demo.run(inputs: {id: "abc"})
        is_true(result.ok)
        are_equal(result.vars.checked_id, "abc")
    }
}
`)
	if err != nil {
		t.Fatalf("test: %v\noutput:\n%s", err, out)
	}
	if !strings.Contains(out, "2 passed, 0 failed, 0 incomplete") {
		t.Errorf("unexpected output:\n%s", out)
	}
}
