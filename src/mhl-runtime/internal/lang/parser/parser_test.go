package parser

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
)

// fixturesDir points at the §3 example fixtures relative to this package.
const fixturesDir = "../../../test/fixtures"

// TestFixturesParse is the fixture-driven conformance suite (IC-1 / AC-1): it
// parses every §3 example block (3.1-3.6) and asserts zero parse errors, with
// a non-nil AST produced for each.
func TestFixturesParse(t *testing.T) {
	entries, err := os.ReadDir(fixturesDir)
	if err != nil {
		t.Fatalf("reading fixtures dir: %v", err)
	}

	var mhlFiles []string
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".mh" {
			mhlFiles = append(mhlFiles, e.Name())
		}
	}
	if len(mhlFiles) < 6 {
		t.Fatalf("expected at least 6 §3 fixtures, found %d", len(mhlFiles))
	}

	for _, name := range mhlFiles {
		name := name
		t.Run(name, func(t *testing.T) {
			src, err := os.ReadFile(filepath.Join(fixturesDir, name))
			if err != nil {
				t.Fatalf("reading fixture %s: %v", name, err)
			}
			prog, err := Parse(string(src))
			if err != nil {
				t.Fatalf("expected zero parse errors for %s, got: %v", name, err)
			}
			if prog == nil {
				t.Fatalf("expected a non-nil AST for %s", name)
			}
			if len(prog.Decls) == 0 {
				t.Fatalf("expected at least one declaration in %s", name)
			}
		})
	}
}

func TestImportParsesOptionalAliases(t *testing.T) {
	prog, err := Parse(`import {FeatureStore as store, RunConfig as config, PlanReader as planner} from "modules/tools/feature.tool.mh"`)
	if err != nil {
		t.Fatalf("parse import aliases: %v", err)
	}
	if got := len(prog.Decls); got != 1 || prog.Decls[0].Import == nil {
		t.Fatalf("expected one import declaration, got %d", got)
	}
	items := prog.Decls[0].Import.Items
	if len(items) != 3 {
		t.Fatalf("import items = %d, want 3", len(items))
	}
	want := [][2]string{{"FeatureStore", "store"}, {"RunConfig", "config"}, {"PlanReader", "planner"}}
	for i, item := range items {
		if item.Name != want[i][0] || item.Alias != want[i][1] {
			t.Errorf("item %d = {%q as %q}, want {%q as %q}", i, item.Name, item.Alias, want[i][0], want[i][1])
		}
	}
}

// TestControlFlowParses guards RF-1's if/while/try-catch coverage using an
// inline pipeline snippet (try/catch is not present in the §3 examples).
func TestControlFlowParses(t *testing.T) {
	src := `
pipeline Flow {
    step Work {
        try {
            var x = 1
            if (x > 0) {
                x = x + 1
            } else {
                x = 0
            }
            while (x < 10) {
                x = x + 1
            }
        } catch (err) {
            log.write(err)
        } finally {
            cleanup()
        }
    }
}
`
	prog, err := Parse(src)
	if err != nil {
		t.Fatalf("expected control-flow snippet to parse, got: %v", err)
	}
	if prog == nil || len(prog.Decls) != 1 {
		t.Fatalf("expected exactly one pipeline declaration")
	}
}

// TestStepTimeoutClauseParses covers the optional `timeout <duration>`
// header clause on a step (internal/lang/ast/pipeline.go Step.Timeout): the
// duration lexes as a single token, a step without the clause has an empty
// Timeout, and a plain step after one still parses.
func TestStepTimeoutClauseParses(t *testing.T) {
	prog, err := Parse(`
pipeline P {
    step A timeout 3m { log("a") }
    step B { log("b") }
    step C timeout 500ms { log("c") }
}
`)
	if err != nil {
		t.Fatalf("expected the timeout clause to parse, got: %v", err)
	}
	body := prog.Decls[0].Pipeline.Body
	if body[0].Step == nil || body[0].Step.Timeout != "3m" {
		t.Fatalf("step A Timeout = %q, want 3m", body[0].Step.Timeout)
	}
	if body[1].Step.Timeout != "" {
		t.Fatalf("step B Timeout = %q, want empty", body[1].Step.Timeout)
	}
	if body[2].Step.Timeout != "500ms" {
		t.Fatalf("step C Timeout = %q, want 500ms", body[2].Step.Timeout)
	}
}

// TestInlineControlFlowBodyParses covers the brace-less inline form of
// if/else, while, and for-in bodies (internal/lang/ast/pipeline.go): each
// accepts a single bare statement in place of a `{ ... }` block, and both
// forms populate the same []*Statement field with the same shape (one
// statement either way), so nothing downstream needs to distinguish them.
func TestInlineControlFlowBodyParses(t *testing.T) {
	src := `
pipeline InlineDemo {
    step S {
        if (true) log("yes") else log("no")
        while (x < 3) x = x + 1
        for (var i in items) log(i)
    }
}
`
	prog, err := Parse(src)
	if err != nil {
		t.Fatalf("expected inline control-flow bodies to parse, got: %v", err)
	}
	if len(prog.Decls) != 1 || prog.Decls[0].Pipeline == nil {
		t.Fatalf("expected exactly one pipeline declaration")
	}
	step := prog.Decls[0].Pipeline.Body[0].Step
	if step == nil || len(step.Body) != 3 {
		t.Fatalf("expected exactly 3 statements in step body, got: %#v", step)
	}

	ifStmt := step.Body[0].If
	if ifStmt == nil || len(ifStmt.Then) != 1 || ifStmt.Then[0].Expr == nil {
		t.Fatalf("expected if's Then to be a single inline expr statement, got: %#v", ifStmt)
	}
	if len(ifStmt.Else) != 1 || ifStmt.Else[0].Expr == nil {
		t.Fatalf("expected if's Else to be a single inline expr statement, got: %#v", ifStmt)
	}

	whileStmt := step.Body[1].While
	if whileStmt == nil || len(whileStmt.Body) != 1 || whileStmt.Body[0].Assign == nil {
		t.Fatalf("expected while's Body to be a single inline assign statement, got: %#v", whileStmt)
	}

	forInStmt := step.Body[2].ForIn
	if forInStmt == nil || len(forInStmt.Body) != 1 || forInStmt.Body[0].Expr == nil {
		t.Fatalf("expected for-in's Body to be a single inline expr statement, got: %#v", forInStmt)
	}
}

// TestCompoundAddAssignParses checks that `x += expr` lexes `+=` as one
// operator and populates AssignStmt.Op, while a plain `x = expr` keeps Op
// as "=".
func TestCompoundAddAssignParses(t *testing.T) {
	src := `
pipeline CompoundDemo {
    step S {
        var acc = []
        acc += ["x"]
        acc = ["y"]
    }
}
`
	prog, err := Parse(src)
	if err != nil {
		t.Fatalf("expected `+=` to parse, got: %v", err)
	}
	body := prog.Decls[0].Pipeline.Body[0].Step.Body
	if len(body) != 3 {
		t.Fatalf("expected 3 statements, got %d", len(body))
	}
	if body[1].Assign == nil || body[1].Assign.Op != "+=" {
		t.Fatalf("expected second statement to be a `+=` assign, got: %#v", body[1])
	}
	if body[2].Assign == nil || body[2].Assign.Op != "=" {
		t.Fatalf("expected third statement to be a plain `=` assign, got: %#v", body[2])
	}
}

// exprPrimary drills down a *ast.Expr to its Primary, for tests that only
// care about a single bare postfix expression (no operators).
func exprPrimary(t *testing.T, e *ast.Expr) *ast.Primary {
	t.Helper()
	return e.Or.Head.Head.Head.Head.Head.Head.Operand.Primary
}

// TestLambdaVsParenExprDisambiguation is the one genuinely uncertain point
// of Primary.Lambda|Sub (internal/ast/expr.go): a single-parameter lambda
// like "(item) -> expr" starts identically to a parenthesized expression
// "(item)", both beginning with "(". The grammar tries Lambda first, so it
// only commits when the trailing "->" is actually present; otherwise it
// backtracks to Sub. Zero-arg "()" and multi-arg "(a, b)" never collide
// with Sub at all (Sub always holds exactly one Expr).
func TestLambdaVsParenExprDisambiguation(t *testing.T) {
	src := `
pipeline P {
    step S {
        var singleParam = (item) -> item.passes == true
        var parenOnly = (item)
        var zeroArg = () -> 1
        var multiArg = (a, b) -> a + b
        var blockBody = (item) -> {
            var x = item
            return x
        }
        Linq.where(items, (item) -> item.passes == true)
    }
}
`
	prog, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	stmts := prog.Decls[0].Pipeline.Body[0].Step.Body

	singleParam := exprPrimary(t, stmts[0].Var.Value)
	if singleParam.Lambda == nil || len(singleParam.Lambda.Params) != 1 {
		t.Errorf("singleParam: expected a 1-param Lambda, got %+v", singleParam)
	}

	parenOnly := exprPrimary(t, stmts[1].Var.Value)
	if parenOnly.Lambda != nil || parenOnly.Sub == nil {
		t.Errorf("parenOnly: expected Sub (parenthesized expr), got Lambda=%v Sub=%v", parenOnly.Lambda, parenOnly.Sub)
	}

	zeroArg := exprPrimary(t, stmts[2].Var.Value)
	if zeroArg.Lambda == nil || len(zeroArg.Lambda.Params) != 0 {
		t.Errorf("zeroArg: expected a 0-param Lambda, got %+v", zeroArg)
	}

	multiArg := exprPrimary(t, stmts[3].Var.Value)
	if multiArg.Lambda == nil || len(multiArg.Lambda.Params) != 2 {
		t.Errorf("multiArg: expected a 2-param Lambda, got %+v", multiArg)
	}

	blockBody := exprPrimary(t, stmts[4].Var.Value)
	if blockBody.Lambda == nil || blockBody.Lambda.Body != nil || len(blockBody.Lambda.Block) != 2 {
		t.Errorf("blockBody: expected a block-bodied Lambda with 2 statements, got %+v", blockBody.Lambda)
	}

	// A lambda nested inside a call argument (the motivating Linq.where
	// case) must parse too, not just at the top level of a var statement.
	call := exprPrimary(t, stmts[5].Expr.Expr)
	nested := stmts[5].Expr.Expr.Or.Head.Head.Head.Head.Head.Head.Operand
	arg := nested.Ops[1].Call.Args[1].Value
	nestedPrimary := exprPrimary(t, arg)
	if nestedPrimary.Lambda == nil || nestedPrimary.Lambda.Body == nil {
		t.Errorf("nested lambda in call args: expected a single-expr Lambda, got %+v (outer ident=%q)", nestedPrimary.Lambda, call.Ident)
	}
}

func TestNullLiteralParses(t *testing.T) {
	prog, err := Parse(`
pipeline P {
    step S {
        var x = null
    }
}
`)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	p := exprPrimary(t, prog.Decls[0].Pipeline.Body[0].Step.Body[0].Var.Value)
	if !p.Null {
		t.Errorf("expected Null to be true, got %+v", p)
	}
}

// TestNullIsNotSwallowedAsIdent guards the alternation order in Primary
// (internal/ast/expr.go): Null must be tried before Ident, or "null" would
// parse as a plain identifier reference instead of the literal.
func TestNullIsNotSwallowedAsIdent(t *testing.T) {
	prog, err := Parse(`
pipeline P {
    step S {
        var x = null
    }
}
`)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	p := exprPrimary(t, prog.Decls[0].Pipeline.Body[0].Step.Body[0].Var.Value)
	if p.Ident != "" {
		t.Errorf("expected null to not be captured as Ident, got %q", p.Ident)
	}
}

func TestForInParses(t *testing.T) {
	src := `
pipeline Flow {
    step Work {
        for (var item in items) {
            log(item)
        }
    }
}
`
	prog, err := Parse(src)
	if err != nil {
		t.Fatalf("expected for-in snippet to parse, got: %v", err)
	}
	stmt := prog.Decls[0].Pipeline.Body[0].Step.Body[0]
	if stmt.ForIn == nil {
		t.Fatalf("expected a ForIn statement, got %+v", stmt)
	}
	if stmt.ForIn.VarName != "item" {
		t.Errorf("VarName = %q, want %q", stmt.ForIn.VarName, "item")
	}
	if len(stmt.ForIn.Body) != 1 {
		t.Errorf("Body length = %d, want 1", len(stmt.ForIn.Body))
	}
}

// TestParseExprParsesASingleExpression covers ParseExpr, added for "${...}"
// string interpolation (internal/engine/interpreter.interpolate): the snippet inside the
// delimiters is one expression, not a whole .mh file, so it needs its own
// entry point rooted at ast.Expr instead of ast.Program.
func TestParseExprParsesASingleExpression(t *testing.T) {
	expr, err := ParseExpr(`session_mem.get("cfg::retries") + 1`)
	if err != nil {
		t.Fatalf("ParseExpr: %v", err)
	}
	if expr == nil || expr.Or == nil {
		t.Fatalf("expected a non-nil expression tree")
	}
}

// TestOptionalChainAndCoalesceParse covers the `?.` optional member trailer
// (ast.Trailer.Optional) and the `??` null-coalescing operator
// (ast.Expr.Tail): `?.` lexes as one token and marks its trailer Optional
// while still filling Member; `??` binds looser than `||`, so its operands
// are whole OrExprs and `a ?? b ?? c` is a two-entry Tail.
func TestOptionalChainAndCoalesceParse(t *testing.T) {
	expr, err := ParseExpr(`metric?.target?.trim() ?? fallback ?? ""`)
	if err != nil {
		t.Fatalf("ParseExpr: %v", err)
	}
	if len(expr.Tail) != 2 {
		t.Fatalf("expected 2 `??` continuations, got %d", len(expr.Tail))
	}
	for i, op := range expr.Tail {
		if op.Op != "??" {
			t.Errorf("Tail[%d].Op = %q, want \"??\"", i, op.Op)
		}
	}
	pf := expr.Or.Head.Head.Head.Head.Head.Head.Operand
	if pf.Primary.Ident != "metric" {
		t.Fatalf("expected primary ident \"metric\", got %q", pf.Primary.Ident)
	}
	if len(pf.Ops) != 3 {
		t.Fatalf("expected 3 trailers (?.target, ?.trim, ()), got %d", len(pf.Ops))
	}
	if !pf.Ops[0].Optional || pf.Ops[0].Member != "target" {
		t.Errorf("Ops[0] = %#v, want optional member \"target\"", pf.Ops[0])
	}
	if !pf.Ops[1].Optional || pf.Ops[1].Member != "trim" {
		t.Errorf("Ops[1] = %#v, want optional member \"trim\"", pf.Ops[1])
	}
	if pf.Ops[2].Call == nil {
		t.Errorf("Ops[2] should be a call trailer, got %#v", pf.Ops[2])
	}

	// A plain `.` member stays non-optional.
	plain, err := ParseExpr(`a.b`)
	if err != nil {
		t.Fatalf("ParseExpr(a.b): %v", err)
	}
	if plain.Or.Head.Head.Head.Head.Head.Head.Operand.Ops[0].Optional {
		t.Errorf("plain `.b` should not be Optional")
	}
}

// TestOptionalDynamicIndexParse covers the `?.[expr]` trailer
// (ast.Trailer.OptIndex): `?.` still lexes as one token, then `[expr]`
// captures the dynamic key — distinct from both `?.name` (OptIndex nil,
// Member set) and the plain `[expr]` index (Index set, OptIndex nil).
func TestOptionalDynamicIndexParse(t *testing.T) {
	expr, err := ParseExpr(`obj?.[key]?.["x"]`)
	if err != nil {
		t.Fatalf("ParseExpr: %v", err)
	}
	pf := expr.Or.Head.Head.Head.Head.Head.Head.Operand
	if len(pf.Ops) != 2 {
		t.Fatalf("expected 2 trailers, got %d", len(pf.Ops))
	}
	if pf.Ops[0].OptIndex == nil || pf.Ops[0].Member != "" || pf.Ops[0].Index != nil {
		t.Errorf("Ops[0] = %#v, want an OptIndex trailer", pf.Ops[0])
	}
	if pf.Ops[1].OptIndex == nil {
		t.Errorf("Ops[1] = %#v, want an OptIndex trailer", pf.Ops[1])
	}

	// A plain `[expr]` index is still Index, not OptIndex.
	plain, err := ParseExpr(`obj[key]`)
	if err != nil {
		t.Fatalf("ParseExpr(obj[key]): %v", err)
	}
	op := plain.Or.Head.Head.Head.Head.Head.Head.Operand.Ops[0]
	if op.Index == nil || op.OptIndex != nil {
		t.Errorf("plain `[key]` should be Index, got %#v", op)
	}
}

func TestParseExprMalformedYieldsError(t *testing.T) {
	expr, err := ParseExpr(`1 +`)
	if err == nil {
		t.Fatalf("expected a parse error for malformed source, got nil")
	}
	if expr != nil {
		t.Fatalf("expected nil AST on parse error, got: %#v", expr)
	}
}

// TestToolMethodBodyVsBlockDisambiguation is the one genuinely uncertain
// point of the ToolMethod.Body|Block grammar (internal/ast/program.go): a
// `-> { ... }` tool method body could be either a single object-literal
// expression or a statement block, both starting with the same "{" token.
// The grammar tries Body (Expr) first, so object-shaped content still
// parses as an expression — Block only wins when the content isn't shaped
// like "key: value" pairs.
func TestToolMethodBodyVsBlockDisambiguation(t *testing.T) {
	src := `
tool T {
    obj_body() -> {a: 1, b: 2}
    empty_body() -> {}
    block_body(items) -> {
        var count = 0
        while (count < items.size()) {
            count = count + 1
        }
        return count
    }
}
`
	prog, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	var tool *ast.Tool
	for _, decl := range prog.Decls {
		if decl.Tool != nil {
			tool = decl.Tool
		}
	}
	if tool == nil {
		t.Fatal("no tool declaration found")
	}
	for _, m := range tool.Methods {
		switch m.Name {
		case "obj_body", "empty_body":
			if m.Body == nil || m.Block != nil {
				t.Errorf("%s: expected an expression Body (object literal), got Body=%v Block=%v", m.Name, m.Body, m.Block)
			}
		case "block_body":
			if m.Body != nil || len(m.Block) != 3 {
				t.Errorf("block_body: expected a 3-statement Block, got Body=%v Block len=%d", m.Body, len(m.Block))
			}
		default:
			t.Errorf("unexpected method %q", m.Name)
		}
	}
}

// TestToolMethodReturnTypeParses covers the `): Type ->` return-type
// annotation — Param already had `: Type` for its own parameters; Returns
// mirrors that shape at the method-declaration level, and must stay
// optional so an unannotated method (untyped return, exactly as before this
// syntax existed) still parses.
func TestToolMethodReturnTypeParses(t *testing.T) {
	src := `
tool T {
    double(n: number): number -> n * 2
    untyped(a, b) -> a + b
}
`
	prog, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	tool := prog.Decls[0].Tool
	if tool.Methods[0].Name != "double" || tool.Methods[0].Returns == nil || tool.Methods[0].Returns.String() != "number" {
		t.Errorf("expected double to have Returns=%q, got %#v", "number", tool.Methods[0])
	}
	if tool.Methods[1].Name != "untyped" || tool.Methods[1].Returns != nil {
		t.Errorf("expected untyped to have empty Returns, got %#v", tool.Methods[1])
	}
	if tool.Methods[0].Pos.Line == 0 {
		t.Errorf("expected ToolMethod.Pos to be populated, got %#v", tool.Methods[0].Pos)
	}
}

// TestTestBlockParses covers the `test { describe { ... } }` grammar
// (internal/lang/ast/test.go): a describe block's Body reuses the exact
// statement grammar a pipeline step's Body does, so both a flat assertion
// call and a control-flow statement wrapping one parse into it.
func TestTestBlockParses(t *testing.T) {
	src := `
test CodeAuditPipelineTest {
    describe conditional_statements {
        are_equal(1, 1)
        is_true(true)
        incomplete("pending")
        if (true) log("yes") else log("no")
    }
}
`
	prog, err := Parse(src)
	if err != nil {
		t.Fatalf("expected test block to parse, got: %v", err)
	}
	if len(prog.Decls) != 1 || prog.Decls[0].Test == nil {
		t.Fatalf("expected exactly one test declaration, got: %#v", prog.Decls)
	}
	test := prog.Decls[0].Test
	if test.Name != "CodeAuditPipelineTest" {
		t.Errorf("expected test name %q, got %q", "CodeAuditPipelineTest", test.Name)
	}
	if len(test.Describes) != 1 {
		t.Fatalf("expected exactly one describe block, got %d", len(test.Describes))
	}
	describe := test.Describes[0]
	if describe.Name != "conditional_statements" {
		t.Errorf("expected describe name %q, got %q", "conditional_statements", describe.Name)
	}
	if len(describe.Body) != 4 {
		t.Fatalf("expected 4 statements, got %d", len(describe.Body))
	}
	if describe.Body[0].Expr == nil {
		t.Errorf("expected first statement to be a bare expression statement (are_equal), got: %#v", describe.Body[0])
	}
	if describe.Body[3].If == nil {
		t.Errorf("expected fourth statement to be an if statement, got: %#v", describe.Body[3])
	}
}

// TestIfExprParses covers the ternary-like if-expression form (IfExpr,
// internal/lang/ast/expr.go): `var result = if (cond) whenTrue else
// whenFalse`, plus using it directly as a call argument and chaining via
// `else if`.
func TestIfExprParses(t *testing.T) {
	src := `
pipeline P {
    step S {
        var result = if (true) valorTrue else valorFalse
        log(if (x > 10) "big" else "small")
        var y = if (n < 0) 0 - 1 else if (n == 0) 0 else 1
    }
}
`
	prog, err := Parse(src)
	if err != nil {
		t.Fatalf("expected if-expression to parse, got: %v", err)
	}
	step := prog.Decls[0].Pipeline.Body[0].Step
	if step == nil || len(step.Body) != 3 {
		t.Fatalf("expected exactly 3 statements in step body, got: %#v", step)
	}

	varDecl := step.Body[0].Var
	if varDecl == nil {
		t.Fatalf("expected first statement to be a var declaration")
	}
	ifExpr := exprPrimary(t, varDecl.Value).IfExpr
	if ifExpr == nil {
		t.Fatalf("expected var's value to be an if-expression, got: %#v", varDecl.Value)
	}
	if got := exprPrimary(t, ifExpr.Then).Ident; got != "valorTrue" {
		t.Errorf("expected Then to be identifier %q, got %q", "valorTrue", got)
	}
	if got := exprPrimary(t, ifExpr.Else).Ident; got != "valorFalse" {
		t.Errorf("expected Else to be identifier %q, got %q", "valorFalse", got)
	}

	// `else if` is just Else holding another bare IfExpr — no dedicated
	// "else if" grammar needed.
	chained := step.Body[2].Var
	if chained == nil {
		t.Fatalf("expected third statement to be a var declaration")
	}
	outer := exprPrimary(t, chained.Value).IfExpr
	if outer == nil {
		t.Fatalf("expected var's value to be an if-expression")
	}
	if got := exprPrimary(t, outer.Else).IfExpr; got == nil {
		t.Errorf("expected outer if-expression's Else to itself be an if-expression (else if), got: %#v", outer.Else)
	}
}

// TestPromptFromSourceParses covers the Body|Source alternation
// (internal/lang/ast/prompt.go): a `prompt ... from "path"` declaration
// parses with Source set to the raw path and Body left nil — the file it
// points at is loaded and Body populated later, during import resolution
// (internal/engine/interpreter/imports.go, internal/lang/lint/imports.go),
// not here.
func TestPromptFromSourceParses(t *testing.T) {
	prog, err := Parse(`export prompt SetupPrompt(name: string) from "./setup.prompt.md"`)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(prog.Decls) != 1 || prog.Decls[0].Prompt == nil {
		t.Fatalf("expected exactly one prompt declaration, got: %#v", prog.Decls)
	}
	p := prog.Decls[0].Prompt
	if p.Source != "./setup.prompt.md" {
		t.Errorf("Source = %q, want %q", p.Source, "./setup.prompt.md")
	}
	if p.Body != nil {
		t.Errorf("expected Body to be nil for a from-sourced prompt, got: %#v", p.Body)
	}
	if len(p.Params) != 1 || p.Params[0].Name != "name" {
		t.Errorf("expected one param %q, got: %#v", "name", p.Params)
	}
}

// TestPromptInlineBodyStillParses guards the other side of the Body|Source
// alternation: a plain inline body must keep parsing exactly as before,
// with Source left empty.
func TestPromptInlineBodyStillParses(t *testing.T) {
	prog, err := Parse(`
prompt Greeting(name: string) {
    "hello ${name}"
}
`)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	p := prog.Decls[0].Prompt
	if p == nil {
		t.Fatalf("expected a prompt declaration")
	}
	if p.Source != "" {
		t.Errorf("expected empty Source for an inline-body prompt, got %q", p.Source)
	}
	if p.Body == nil {
		t.Errorf("expected a non-nil Body for an inline-body prompt")
	}
}

// TestMemDeclParses covers the `mem x = expr` pipeline-body declaration
// (PipelineMember.Mem) alongside a plain `var`, a `repeat` block referencing
// the mem name, and a `count.reset()` method call in a step body — the
// shapes interpreter/exec.go's mem support depends on.
func TestMemDeclParses(t *testing.T) {
	prog, err := Parse(`
loop pipeline P {
    var attempts = 0
    mem count = 0

    repeat: {
        stop_when: count == 10
    }

    step S {
        count = count + 1
        count.reset()
    }
}
`)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	body := prog.Decls[0].Pipeline.Body
	if body[0].Var == nil || body[0].Var.Name != "attempts" {
		t.Fatalf("expected first member to be var attempts, got %#v", body[0])
	}
	if body[1].Mem == nil || body[1].Mem.Name != "count" {
		t.Fatalf("expected second member to be mem count, got %#v", body[1])
	}
	if body[1].Var != nil {
		t.Errorf("mem decl must not also populate Var")
	}
}

// TestPipelineKindParses covers the `pipeline` / `workflow` declaration
// keyword (ast.Pipeline.Kind), including the `loop` prefix on either.
func TestPipelineKindParses(t *testing.T) {
	cases := []struct {
		src      string
		wantKind string
		wantLoop bool
		wantIsWF bool
	}{
		{"pipeline P { step S { log(\"s\") } }", "pipeline", false, false},
		{"workflow W { step S { goto S } }", "workflow", false, true},
		{"loop pipeline LP { step S { log(\"s\") } }", "pipeline", true, false},
		{"loop workflow LW { step S { goto S } }", "workflow", true, true},
	}
	for _, c := range cases {
		prog, err := Parse(c.src)
		if err != nil {
			t.Fatalf("Parse(%q): %v", c.src, err)
		}
		p := prog.Decls[0].Pipeline
		if p == nil {
			t.Fatalf("Parse(%q): no pipeline decl", c.src)
		}
		if p.Kind != c.wantKind || p.Loop != c.wantLoop || p.IsWorkflow() != c.wantIsWF {
			t.Errorf("Parse(%q): Kind=%q Loop=%v IsWorkflow=%v, want Kind=%q Loop=%v IsWorkflow=%v",
				c.src, p.Kind, p.Loop, p.IsWorkflow(), c.wantKind, c.wantLoop, c.wantIsWF)
		}
	}
}

// TestPipelineMaxClauseParses covers the optional `max <N>` header clause on
// a `loop pipeline` / `loop workflow` declaration (ast.Pipeline.Max) —
// shorthand for `repeat { max_iterations: N }`.
func TestPipelineMaxClauseParses(t *testing.T) {
	cases := []struct {
		src     string
		wantMax string
	}{
		{"loop workflow Refine max 3 { step S { log(\"s\") } }", "3"},
		{"loop pipeline P max 10 { step S { log(\"s\") } }", "10"},
		{"loop workflow Refine { step S { log(\"s\") } }", ""},
		{"workflow W { step S { log(\"s\") } }", ""},
	}
	for _, c := range cases {
		prog, err := Parse(c.src)
		if err != nil {
			t.Fatalf("Parse(%q): %v", c.src, err)
		}
		if got := prog.Decls[0].Pipeline.Max; got != c.wantMax {
			t.Errorf("Parse(%q): Max=%q, want %q", c.src, got, c.wantMax)
		}
	}
}

// A `loop workflow` may still be named `max` — the keyword is reserved only
// in the header-clause position, after the name.
func TestPipelineNamedMaxStillParses(t *testing.T) {
	prog, err := Parse(`loop workflow max max 3 { step S { log("s") } }`)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	p := prog.Decls[0].Pipeline
	if p.Name != "max" || p.Max != "3" {
		t.Fatalf("Name=%q Max=%q, want name %q with clause %q", p.Name, p.Max, "max", "3")
	}
}

// TestImportKeywordRemoved confirms `import "..." as x` no longer parses —
// cross-file composition is `import { ... } from "..."` only.
func TestImportKeywordRemoved(t *testing.T) {
	_, err := Parse(`import "./other.mh" as other`)
	if err == nil {
		t.Fatal("expected a parse error for the removed `import` keyword, got nil")
	}
}

// TestMalformedYieldsError is the failure path: a syntactically invalid .mh
// source must yield a descriptive error rather than a partial/incorrect AST.
func TestMalformedYieldsError(t *testing.T) {
	// `extension` requires a kind, a name, and a body; this is truncated.
	prog, err := Parse(`extension mcp {`)
	if err == nil {
		t.Fatalf("expected a parse error for malformed source, got nil")
	}
	if prog != nil {
		t.Fatalf("expected nil AST on parse error, got: %#v", prog)
	}
	if err.Error() == "" {
		t.Fatalf("expected a descriptive parse error message")
	}
}

// TestTypedDeclPosPopulates confirms PipelineInput/Param's additive Pos
// field is populated by participle (no parser tag needed), so lint findings
// anchored to these nodes can carry a real line/column.
func TestTypedDeclPosPopulates(t *testing.T) {
	prog, err := Parse(`
pipeline P {
    input issue_id: string
    step S {}
}

tool T {
    read_file(path: string) -> fs.read(path)
}
`)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	in := prog.Decls[0].Pipeline.Body[0].Input
	if in == nil || in.Pos.Line == 0 {
		t.Fatalf("expected PipelineInput.Pos to be populated, got %#v", in)
	}
	param := prog.Decls[1].Tool.Methods[0].Params[0]
	if param.Pos.Line == 0 {
		t.Fatalf("expected Param.Pos to be populated, got %#v", param)
	}
}

// TestExtensionDeclParses confirms `extension <kind> <Name> { ... }` binds a
// kind, a name and a property-bag body, and that ast.AsExtension yields that
// view. `extension a2a` gets duration-literal properties like any other kind.
func TestExtensionDeclParses(t *testing.T) {
	prog, err := Parse(`
extension mcp GitHub {
    transport: "http"
    url: "https://api.githubcopilot.com/mcp/"
}

extension a2a Translator {
    url: "https://translator.example.com/a2a"
    headers: {
        "Authorization": "Bearer " + env("A2A_TOKEN")
    }
    poll_interval: 1s
    poll_timeout: 120s
}
`)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(prog.Decls) != 2 || prog.Decls[0].Extension == nil || prog.Decls[1].Extension == nil {
		t.Fatalf("expected two extension declarations, got %#v", prog.Decls)
	}
	e := prog.Decls[0].Extension
	if e.Kind != "mcp" || e.Name != "GitHub" {
		t.Fatalf("expected kind=mcp name=GitHub, got kind=%q name=%q", e.Kind, e.Name)
	}
	if len(e.Props) != 2 || e.Props[0].Name != "transport" {
		t.Fatalf("expected 2 props starting with transport, got %#v", e.Props)
	}

	kind, name, props, ok := ast.AsExtension(prog.Decls[0])
	if !ok || kind != "mcp" || name != "GitHub" || len(props) != 2 {
		t.Fatalf("AsExtension = %q %q %d %v", kind, name, len(props), ok)
	}

	a := prog.Decls[1].Extension
	if a.Kind != "a2a" || a.Name != "Translator" || len(a.Props) != 4 {
		t.Fatalf("unexpected a2a extension: %#v", a)
	}
}
