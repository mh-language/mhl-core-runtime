package parser

import "testing"

func TestSpawnStatementParses(t *testing.T) {
	prog, err := Parse(`
pipeline P {
    spawn: { max_concurrency: 3 }
    step S {
        spawn rev = Reviewer.run(prompt: "check")
        wait rev
    }
}
`)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	body := prog.Decls[0].Pipeline.Body
	// pipeline-level `spawn:` is a Property, not a Step or a SpawnStmt.
	if body[0].Prop == nil || body[0].Prop.Name != "spawn" {
		t.Fatalf("expected pipeline-level spawn Property, got %+v", body[0])
	}
	stmts := body[1].Step.Body
	if stmts[0].Spawn == nil || stmts[0].Spawn.Name != "rev" {
		t.Fatalf("expected SpawnStmt binding rev, got %+v", stmts[0])
	}
	if stmts[1].Wait == nil || len(stmts[1].Wait.Names) != 1 || stmts[1].Wait.Names[0] != "rev" {
		t.Fatalf("expected `wait rev`, got %+v", stmts[1].Wait)
	}
}

func TestWaitStatementFormsParse(t *testing.T) {
	cases := []struct {
		src        string
		wantAny    bool
		wantQuorum string
		wantNames  int
		wantOpts   int
	}{
		{`wait a, b, c`, false, "", 3, 0},
		{`wait any a, b`, true, "", 2, 0},
		{`wait 2 of a, b, c`, false, "2", 3, 0},
		{`wait a, b timeout: 30s`, false, "", 2, 1},
		{`wait a, b timeout: 5s on_error: "collect"`, false, "", 2, 2},
	}
	for _, c := range cases {
		t.Run(c.src, func(t *testing.T) {
			prog, err := Parse("pipeline P { step S {\n" + c.src + "\n} }")
			if err != nil {
				t.Fatalf("Parse(%q): %v", c.src, err)
			}
			w := prog.Decls[0].Pipeline.Body[0].Step.Body[0].Wait
			if w == nil {
				t.Fatalf("no WaitStmt parsed from %q", c.src)
			}
			if w.Any != c.wantAny || w.Quorum != c.wantQuorum ||
				len(w.Names) != c.wantNames || len(w.Opts) != c.wantOpts {
				t.Fatalf("%q -> Any=%v Quorum=%q Names=%v Opts=%v", c.src, w.Any, w.Quorum, w.Names, w.Opts)
			}
		})
	}
}

// `spawn xs = Agent.run(...) for item in <expr>` parses into a SpawnStmt
// with EachVar/Iterable set, and a following statement is not swallowed.
func TestSpawnFanOutFormParses(t *testing.T) {
	prog, err := Parse(`
pipeline P {
    step S {
        spawn reviews = R.run(prompt: "on ${item}") for item in angles
        wait reviews
        log("done")
    }
}
`)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	stmts := prog.Decls[0].Pipeline.Body[0].Step.Body
	sp := stmts[0].Spawn
	if sp == nil || sp.Name != "reviews" {
		t.Fatalf("expected SpawnStmt binding reviews, got %+v", stmts[0])
	}
	if sp.EachVar != "item" || sp.Iterable == nil {
		t.Fatalf("expected `for item in <expr>` clause, got EachVar=%q Iterable=%v", sp.EachVar, sp.Iterable)
	}
	if stmts[1].Wait == nil || stmts[2].Expr == nil {
		t.Fatalf("statements after the fan-out spawn were swallowed: %+v", stmts)
	}
}

// The plain `spawn x = Agent.run(...)` form still parses with no fan-out clause.
func TestSpawnPlainFormHasNoFanOut(t *testing.T) {
	prog, err := Parse(`pipeline P { step S { spawn a = A.run(prompt: "x") } }`)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	sp := prog.Decls[0].Pipeline.Body[0].Step.Body[0].Spawn
	if sp == nil || sp.EachVar != "" || sp.Iterable != nil {
		t.Fatalf("plain spawn should have no fan-out clause, got %+v", sp)
	}
}

// A step body that mutates a var after the new keywords still parses — the
// keywords must not be greedy enough to swallow the following statement.
func TestStatementAfterWaitParses(t *testing.T) {
	_, err := Parse(`
pipeline P {
    step S {
        spawn a = A.run(prompt: "x")
        wait a
        log(a.result)
    }
}
`)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
}
