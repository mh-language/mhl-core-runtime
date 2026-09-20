package runtime_test

import (
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/engine/runtime"
	"github.com/mh-language/mhl-core-runtime/internal/lang/parser"
)

// TestPipelineFromASTMovesEntryStepToFront exercises PipelineFromAST
// directly (no multi-file merge involved — that's interpreter/lint
// territory) to isolate the one piece of behavior this package owns for
// `partial`: an `entry step` wins over "first step physically declared",
// which Stages[0]/Steps[0] otherwise mean everywhere else in the runner.
func TestPipelineFromASTMovesEntryStepToFront(t *testing.T) {
	prog, err := parser.Parse(`
partial workflow Discovery {
    step Gate { var x = 1 }
    entry step Dispatch { var x = 1 }
    step Done { var x = 1 }
}
`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	p, err := runtime.FindPipeline(prog, "")
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if got, want := p.Steps, []string{"Dispatch", "Gate", "Done"}; !equalStrings(got, want) {
		t.Fatalf("Steps = %v, want %v", got, want)
	}
	if len(p.Stages) != 3 || p.Stages[0].Name != "Dispatch" || p.Stages[1].Name != "Gate" || p.Stages[2].Name != "Done" {
		t.Fatalf("Stages = %+v", p.Stages)
	}
}

// TestPipelineFromASTLeavesOrderAloneWithoutEntry is the control case: a
// pipeline with no `entry` step (the overwhelming majority — every
// non-partial declaration) keeps today's "first physically declared"
// order untouched.
func TestPipelineFromASTLeavesOrderAloneWithoutEntry(t *testing.T) {
	prog, err := parser.Parse(`
pipeline P {
    step A { var x = 1 }
    step B { var x = 1 }
}
`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	p, err := runtime.FindPipeline(prog, "")
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if got, want := p.Steps, []string{"A", "B"}; !equalStrings(got, want) {
		t.Fatalf("Steps = %v, want %v", got, want)
	}
}

// TestPipelineFromASTMovesEntryParallelStageToFront confirms the reorder
// works at the Stage granularity, not just for a bare step — an `entry`
// step ahead of a `parallel` group still moves in front of it.
func TestPipelineFromASTMovesEntryParallelStageToFront(t *testing.T) {
	prog, err := parser.Parse(`
partial workflow Discovery {
    parallel Gather {
        step A { var x = 1 }
        step B { var x = 1 }
    }
    entry step Dispatch { var x = 1 }
}
`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	p, err := runtime.FindPipeline(prog, "")
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if got, want := p.Steps, []string{"Dispatch", "A", "B"}; !equalStrings(got, want) {
		t.Fatalf("Steps = %v, want %v", got, want)
	}
	if len(p.Stages) != 2 || p.Stages[0].Name != "Dispatch" || p.Stages[1].Name != "Gather" || !p.Stages[1].Parallel {
		t.Fatalf("Stages = %+v", p.Stages)
	}
}
