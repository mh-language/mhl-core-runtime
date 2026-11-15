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

// A `timeout <duration>` clause parses both on the group header and on an
// individual branch step; a branch without one has an empty Timeout.
func TestParallelGroupTimeoutParses(t *testing.T) {
	prog, err := Parse(`
pipeline P {
    parallel Review timeout 5m {
        step Lint timeout 90s { log("lint") }
        step Build { log("build") }
    }
}
`)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	g := prog.Decls[0].Pipeline.Body[0].Parallel
	if g == nil || g.Timeout != "5m" {
		t.Fatalf("group Timeout = %q, want 5m (group %+v)", g.Timeout, g)
	}
	if g.Steps[0].Timeout != "90s" {
		t.Fatalf("branch Lint Timeout = %q, want 90s", g.Steps[0].Timeout)
	}
	if g.Steps[1].Timeout != "" {
		t.Fatalf("branch Build Timeout = %q, want empty", g.Steps[1].Timeout)
	}
}

// A group may end with one `merge (param) { ... }`; it must come last and
// name its param, and `merge` stays an ordinary identifier elsewhere.
func TestParallelMergeParses(t *testing.T) {
	prog, err := Parse(`
workflow W {
    var merge = []
    parallel Fan {
        step L { merge = ["a"] }
        step R { log("r") }
        merge (branches) {
            self.merge = branches.L.merge
        }
    }
}
`)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	g := prog.Decls[0].Pipeline.Body[1].Parallel
	if g.Merge == nil || g.Merge.Param != "branches" || len(g.Merge.Body) != 1 {
		t.Fatalf("merge = %+v", g.Merge)
	}
	if ms := g.MergeStep(); ms == nil || ms.Name != "Fan.merge" || len(ms.Body) != 1 {
		t.Fatalf("MergeStep = %+v", ms)
	}
	for name, src := range map[string]string{
		"merge not last": `workflow W { parallel Fan { merge (b) { log("m") } step L { log("l") } } }`,
		"var after a step": `workflow W { parallel Fan { step L { log("l") } var x = 1 } }`,
		"two merges":     `workflow W { parallel Fan { step L { log("l") } merge (b) { log("m") } merge (c) { log("n") } } }`,
	} {
		if _, err := Parse(src); err == nil {
			t.Errorf("%s: want a parse error", name)
		}
	}
}

// A group may declare its own vars before its branches, and a merge may
// omit its param (`merge { ... }` or `merge () { ... }`).
func TestParallelGroupVarsParse(t *testing.T) {
	prog, err := Parse(`
workflow W {
    parallel Fan {
        var left = ""
        var right = left
        step L { left = "a" }
        step R { right = "b" }
        merge { log(left, right) }
    }
    parallel Again {
        step X { log("x") }
        merge () { log("m") }
    }
}
`)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	fan := prog.Decls[0].Pipeline.Body[0].Parallel
	if len(fan.Vars) != 2 || fan.Vars[0].Decl.Name != "left" || fan.Vars[1].Decl.Name != "right" {
		t.Fatalf("vars = %+v", fan.Vars)
	}
	if fan.Merge == nil || fan.Merge.Param != "" || len(fan.Merge.Body) != 1 {
		t.Fatalf("merge = %+v", fan.Merge)
	}
	if m := prog.Decls[0].Pipeline.Body[1].Parallel.Merge; m == nil || m.Param != "" {
		t.Fatalf("merge () = %+v", m)
	}
}
