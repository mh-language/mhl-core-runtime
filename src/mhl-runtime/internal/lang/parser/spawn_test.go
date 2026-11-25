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

func TestSpawnConcurrencyClauseParses(t *testing.T) {
	cases := []struct {
		src, name, concurrency, each string
	}{
		{`spawn checks concurrency 2 = Echo.run(prompt: "check ${f}") for f in files`, "checks", "2", "f"},
		{`spawn checks = Echo.run(prompt: "check ${f}") for f in files`, "checks", "", "f"},
		{`spawn one concurrency 3 = Echo.run(prompt: "x")`, "one", "3", ""},
		// `concurrency` stays usable as a handle name.
		{`spawn concurrency = Echo.run(prompt: "x")`, "concurrency", "", ""},
	}
	for _, tc := range cases {
		prog, err := Parse("pipeline P {\n    step S {\n        " + tc.src + "\n    }\n}\n")
		if err != nil {
			t.Fatalf("Parse(%q): %v", tc.src, err)
		}
		sp := prog.Decls[0].Pipeline.Body[0].Step.Body[0].Spawn
		if sp == nil || sp.Name != tc.name || sp.Concurrency != tc.concurrency || sp.EachVar != tc.each {
			t.Fatalf("Parse(%q) = %+v, want name %q concurrency %q each %q", tc.src, sp, tc.name, tc.concurrency, tc.each)
		}
	}
}

// The clause forms (`timeout 30s`, `collect`) and the deprecated `key:
// value` ones parse to the same options; Colon records which one was used.
func TestWaitOptionClausesParse(t *testing.T) {
	cases := []struct {
		src      string
		keys     []string // "collect" for a bare collect, else the Key
		colons   []bool
		nextStmt bool // a statement follows the wait
	}{
		{`wait xs timeout 30s collect`, []string{"timeout", "collect"}, []bool{false, false}, false},
		{`wait xs collect timeout 30s`, []string{"collect", "timeout"}, []bool{false, false}, false},
		{`wait xs timeout: 30s on_error: "collect"`, []string{"timeout", "on_error"}, []bool{true, true}, false},
		// A statement starting with `collect` / `timeout` is not swallowed.
		{"wait xs\n        collect = 1", nil, nil, true},
		{"wait xs collect\n        collect.append(1)", []string{"collect"}, []bool{false}, true},
		{"wait xs\n        timeout = 1", nil, nil, true},
	}
	for _, tc := range cases {
		prog, err := Parse("pipeline P {\n    step S {\n        " + tc.src + "\n    }\n}\n")
		if err != nil {
			t.Fatalf("Parse(%q): %v", tc.src, err)
		}
		body := prog.Decls[0].Pipeline.Body[0].Step.Body
		w := body[0].Wait
		if w == nil {
			t.Fatalf("Parse(%q): first statement is not a wait: %+v", tc.src, body[0])
		}
		if len(w.Opts) != len(tc.keys) {
			t.Fatalf("Parse(%q): %d opts, want %d", tc.src, len(w.Opts), len(tc.keys))
		}
		for i, o := range w.Opts {
			key := o.Key
			if o.Collect {
				key = "collect"
			}
			if key != tc.keys[i] || o.Colon != tc.colons[i] {
				t.Errorf("Parse(%q) opt %d = {key %q colon %v}, want {%q %v}", tc.src, i, key, o.Colon, tc.keys[i], tc.colons[i])
			}
		}
		if tc.nextStmt && len(body) != 2 {
			t.Errorf("Parse(%q): %d statements, want the wait plus one", tc.src, len(body))
		}
	}
}
