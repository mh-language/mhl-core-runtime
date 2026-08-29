package parser

import "testing"

func TestParallelGroupParses(t *testing.T) {
	prog, err := Parse(`
pipeline P {
    var a = ""
    parallel Gather {
        step One { a = "1" }
        step Two { log("2") }
        step Three { log("3") }
    }
    step Merge { log(a) }
}
`)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	body := prog.Decls[0].Pipeline.Body
	if body[1].Parallel == nil {
		t.Fatalf("expected body[1] to be a ParallelGroup, got %+v", body[1])
	}
	g := body[1].Parallel
	if g.Name != "Gather" {
		t.Fatalf("group name = %q, want Gather", g.Name)
	}
	if len(g.Steps) != 3 || g.Steps[0].Name != "One" || g.Steps[2].Name != "Three" {
		t.Fatalf("group steps = %+v", g.Steps)
	}
	if body[2].Step == nil || body[2].Step.Name != "Merge" {
		t.Fatalf("expected a plain step Merge after the group, got %+v", body[2])
	}
}

// `parallel` requires a name — `parallel { ... }` must not parse.
func TestParallelGroupRequiresName(t *testing.T) {
	_, err := Parse(`
pipeline P {
    parallel {
        step One { log("1") }
    }
}
`)
	if err == nil {
		t.Fatal("expected a parse error for an unnamed parallel group")
	}
}

// A group needs at least one step (`@@+`).
func TestParallelGroupRejectsEmptyBody(t *testing.T) {
	_, err := Parse(`
pipeline P {
    parallel Gather { }
}
`)
	if err == nil {
		t.Fatal("expected a parse error for an empty parallel group")
	}
}

// A plain step declared right after a group still parses — the group's
// closing brace must not be greedy.
func TestStepAfterParallelGroupParses(t *testing.T) {
	_, err := Parse(`
pipeline P {
    parallel G {
        step A { log("a") }
    }
    step B { log("b") }
}
`)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
}
