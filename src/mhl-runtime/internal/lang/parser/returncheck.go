package parser

import (
	"github.com/alecthomas/participle/v2"
	"github.com/alecthomas/participle/v2/lexer"

	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
)

// checkAmbiguousBareReturn rejects a "return"/"break" whose optional value
// expression starts on a later source line than the keyword itself —
// `if (flag) return` immediately followed, on the next line, by more code.
//
// Why this is necessary at all: mhlLexer elides newlines as ordinary
// whitespace (see lexer.go's Whitespace rule), so the grammar has no
// statement terminator to rely on. ReturnStmt/BreakStmt's own grammar
// (`'return' @@?` / `'break' @@?`) greedily tries to parse whatever
// expression follows as that statement's value — including a completely
// separate statement one or more lines below, whenever it happens to start
// with something expression-shaped (almost every real statement does: a
// call, an assignment target, ...). The result silently reinterprets two
// statements as one:
//
//	if (flag) return
//	log.info("REACHED")
//
// does not parse as "return early when flag; otherwise log" — it parses as
// one statement, "if flag, return the result of calling log.info(...)",
// which also means calling it (for its side effect) unconditionally as
// part of evaluating that return value, and the intended early-exit never
// happens. mhl lint has nothing to say about this today: the parse
// succeeds and lint has no reason to suspect it isn't what the author
// meant. The equivalent guard-clause pattern with no if at all —
//
//	return
//	log.info("UNREACHABLE")
//
// — has the identical problem for the same reason.
//
// The fix mirrors Go's own automatic-semicolon-insertion rule for return:
// a value is only ever accepted when its own first token starts on the
// same line the "return"/"break" keyword did (ast.Statement.Pos, always
// exactly the keyword's own position since it's the first token of that
// Statement; ast.Expr.Pos, the value's own leading token). A value that
// starts on a later line is always this ambiguity, never a legitimate
// multi-line return value — an author who wants a genuinely multi-line
// value keeps something immediately after the keyword on the same line
// (an opening "{"/"["/"(", or the first token of a long expression), which
// this check does not touch; only a value with *nothing* preceding it on
// the keyword's own line is rejected.
//
// Runs across every place a "return"/"break" can appear: tool method
// bodies, prompt/agent/router/memory/extension property values (a
// session_start/before/select/... hook is a lambda living in exactly this
// position), pipeline steps and parallel-group branch steps, route match
// arms, and any lambda literal reachable from any of those — including one
// passed as a call argument (`.filter((x) -> { ... })`), the single most
// common place a lambda appears in mhl source.
func checkAmbiguousBareReturn(prog *ast.Program) error {
	for _, decl := range prog.Decls {
		if err := checkDeclForAmbiguousReturn(decl); err != nil {
			return err
		}
	}
	return nil
}

func checkDeclForAmbiguousReturn(decl *ast.Declaration) error {
	switch {
	case decl.Agent != nil:
		return checkPropsForAmbiguousReturn(decl.Agent.Props)
	case decl.Router != nil:
		return checkPropsForAmbiguousReturn(decl.Router.Props)
	case decl.Memory != nil:
		return checkPropsForAmbiguousReturn(decl.Memory.Props)
	case decl.Extension != nil:
		return checkPropsForAmbiguousReturn(decl.Extension.Props)
	case decl.Prompt != nil:
		if err := checkExprForAmbiguousReturn(decl.Prompt.Body); err != nil {
			return err
		}
		return checkParamDefaultsForAmbiguousReturn(decl.Prompt.Params)
	case decl.Tool != nil:
		for _, member := range decl.Tool.Members {
			switch {
			case member.Const != nil:
				if err := checkExprForAmbiguousReturn(member.Const.Value); err != nil {
					return err
				}
			case member.Var != nil:
				if err := checkExprForAmbiguousReturn(member.Var.Value); err != nil {
					return err
				}
			case member.Method != nil:
				if err := checkParamDefaultsForAmbiguousReturn(member.Method.Params); err != nil {
					return err
				}
				if err := checkExprForAmbiguousReturn(member.Method.Body); err != nil {
					return err
				}
				if err := checkStatementsForAmbiguousReturn(member.Method.Block); err != nil {
					return err
				}
			}
		}
	case decl.Pipeline != nil:
		return checkPipelineMembersForAmbiguousReturn(decl.Pipeline.Body)
	case decl.Test != nil:
		for _, d := range decl.Test.Describes {
			if err := checkStatementsForAmbiguousReturn(d.Body); err != nil {
				return err
			}
		}
	}
	return nil
}

func checkPipelineMembersForAmbiguousReturn(members []*ast.PipelineMember) error {
	for _, m := range members {
		switch {
		case m.Input != nil:
			if err := checkExprForAmbiguousReturn(m.Input.Default); err != nil {
				return err
			}
		case m.Const != nil:
			if err := checkExprForAmbiguousReturn(m.Const.Value); err != nil {
				return err
			}
		case m.Var != nil:
			if err := checkExprForAmbiguousReturn(m.Var.Value); err != nil {
				return err
			}
		case m.Mem != nil:
			if err := checkExprForAmbiguousReturn(m.Mem.Value); err != nil {
				return err
			}
		case m.Parallel != nil:
			for _, step := range m.Parallel.Steps {
				if err := checkStatementsForAmbiguousReturn(step.Body); err != nil {
					return err
				}
			}
		case m.Route != nil:
			if err := checkParamDefaultsForAmbiguousReturn(m.Route.Params); err != nil {
				return err
			}
			for _, arm := range m.Route.Arms {
				if err := checkExprForAmbiguousReturn(arm.Pattern); err != nil {
					return err
				}
				if err := checkExprForAmbiguousReturn(arm.Fail); err != nil {
					return err
				}
			}
		case m.Step != nil:
			if err := checkStatementsForAmbiguousReturn(m.Step.Body); err != nil {
				return err
			}
		case m.Prop != nil:
			if err := checkExprForAmbiguousReturn(m.Prop.Value); err != nil {
				return err
			}
		}
	}
	return nil
}

func checkPropsForAmbiguousReturn(props []*ast.Property) error {
	for _, p := range props {
		if err := checkExprForAmbiguousReturn(p.Value); err != nil {
			return err
		}
	}
	return nil
}

func checkParamDefaultsForAmbiguousReturn(params []*ast.Param) error {
	for _, p := range params {
		if err := checkExprForAmbiguousReturn(p.Default); err != nil {
			return err
		}
	}
	return nil
}

// checkStatementsForAmbiguousReturn is the core recursive statement-list
// walker: every "[]*ast.Statement" field the grammar defines (Lambda.Block,
// ToolMethod.Block, Step.Body, IfStmt.Then/Else, WhileStmt.Body,
// ForInStmt.Body, TryStmt.Body/Catch/Finally, Describe.Body) reaches
// return/break validation, nested control flow, and expression scanning
// (for a call argument's own lambda, an if/while condition, ...) through
// this one function.
func checkStatementsForAmbiguousReturn(stmts []*ast.Statement) error {
	for _, stmt := range stmts {
		if err := checkStatementForAmbiguousReturn(stmt); err != nil {
			return err
		}
	}
	return nil
}

func checkStatementForAmbiguousReturn(stmt *ast.Statement) error {
	switch {
	case stmt.Return != nil:
		if err := checkValueSameLine(stmt.Pos, stmt.Return.Value, "return"); err != nil {
			return err
		}
		return checkExprForAmbiguousReturn(stmt.Return.Value)
	case stmt.Break != nil:
		if err := checkValueSameLine(stmt.Pos, stmt.Break.Reason, "break"); err != nil {
			return err
		}
		return checkExprForAmbiguousReturn(stmt.Break.Reason)
	case stmt.Const != nil:
		return checkExprForAmbiguousReturn(stmt.Const.Value)
	case stmt.Var != nil:
		return checkExprForAmbiguousReturn(stmt.Var.Value)
	case stmt.Assign != nil:
		return checkExprForAmbiguousReturn(stmt.Assign.Value)
	case stmt.Expr != nil:
		return checkExprForAmbiguousReturn(stmt.Expr.Expr)
	case stmt.Spawn != nil:
		if err := checkExprForAmbiguousReturn(stmt.Spawn.Call); err != nil {
			return err
		}
		return checkExprForAmbiguousReturn(stmt.Spawn.Iterable)
	case stmt.If != nil:
		if err := checkExprForAmbiguousReturn(stmt.If.Cond); err != nil {
			return err
		}
		if err := checkStatementsForAmbiguousReturn(stmt.If.Then); err != nil {
			return err
		}
		return checkStatementsForAmbiguousReturn(stmt.If.Else)
	case stmt.While != nil:
		if err := checkExprForAmbiguousReturn(stmt.While.Cond); err != nil {
			return err
		}
		return checkStatementsForAmbiguousReturn(stmt.While.Body)
	case stmt.ForIn != nil:
		if err := checkExprForAmbiguousReturn(stmt.ForIn.Iterable); err != nil {
			return err
		}
		return checkStatementsForAmbiguousReturn(stmt.ForIn.Body)
	case stmt.Try != nil:
		if err := checkStatementsForAmbiguousReturn(stmt.Try.Body); err != nil {
			return err
		}
		if err := checkStatementsForAmbiguousReturn(stmt.Try.Catch); err != nil {
			return err
		}
		return checkStatementsForAmbiguousReturn(stmt.Try.Finally)
	}
	return nil
}

// checkValueSameLine is checkAmbiguousBareReturn's actual rule: value (a
// return's or break's optional expression) is only legitimate when its own
// first token starts on keywordPos's line — the line "return"/"break"
// itself is on, since keywordPos is always the enclosing Statement's own
// Pos (the first token of that statement, i.e. the keyword).
func checkValueSameLine(keywordPos lexer.Position, value *ast.Expr, keyword string) error {
	if value == nil || value.Pos.Line == keywordPos.Line {
		return nil
	}
	return participle.Errorf(keywordPos,
		"a bare %q with nothing after it on the same line reads the following statement as %q's own value, not as a separate statement — "+
			"if %q is an inline if/while/for branch (\"if (cond) %s\"), wrap just that branch in braces (\"if (cond) { %s }\"); "+
			"otherwise give %q an explicit value on the same line, or restructure so nothing follows it directly",
		keyword, keyword, keyword, keyword, keyword, keyword)
}

// checkExprForAmbiguousReturn walks expr's full precedence tree — Or, And,
// Eq, Cmp, Add, Mul, Unary, Postfix (call arguments, index/slice bounds),
// Primary (object/array literals, if-expressions, match arms, a
// parenthesized sub-expression) — for every reachable Lambda literal, and
// validates each one's own Block/Body the same way any other callable is
// validated. Mirrors internal/lang/lint/loop.go's walkExprIdents, which
// walks the identical shape for a different purpose (no single existing
// walker serves both; lint can't be imported from here, and wouldn't want
// this specific traversal anyway).
func checkExprForAmbiguousReturn(expr *ast.Expr) error {
	if expr == nil {
		return nil
	}
	if err := checkOrForAmbiguousReturn(expr.Or); err != nil {
		return err
	}
	for _, op := range expr.Tail {
		if err := checkOrForAmbiguousReturn(op.Rhs); err != nil {
			return err
		}
	}
	return nil
}

func checkOrForAmbiguousReturn(e *ast.OrExpr) error {
	if e == nil {
		return nil
	}
	if err := checkAndForAmbiguousReturn(e.Head); err != nil {
		return err
	}
	for _, op := range e.Tail {
		if err := checkAndForAmbiguousReturn(op.Rhs); err != nil {
			return err
		}
	}
	return nil
}

func checkAndForAmbiguousReturn(e *ast.AndExpr) error {
	if e == nil {
		return nil
	}
	if err := checkEqForAmbiguousReturn(e.Head); err != nil {
		return err
	}
	for _, op := range e.Tail {
		if err := checkEqForAmbiguousReturn(op.Rhs); err != nil {
			return err
		}
	}
	return nil
}

func checkEqForAmbiguousReturn(e *ast.EqExpr) error {
	if e == nil {
		return nil
	}
	if err := checkCmpForAmbiguousReturn(e.Head); err != nil {
		return err
	}
	for _, op := range e.Tail {
		if err := checkCmpForAmbiguousReturn(op.Rhs); err != nil {
			return err
		}
	}
	return nil
}

func checkCmpForAmbiguousReturn(e *ast.CmpExpr) error {
	if e == nil {
		return nil
	}
	if err := checkAddForAmbiguousReturn(e.Head); err != nil {
		return err
	}
	for _, op := range e.Tail {
		if err := checkAddForAmbiguousReturn(op.Rhs); err != nil {
			return err
		}
	}
	return nil
}

func checkAddForAmbiguousReturn(e *ast.AddExpr) error {
	if e == nil {
		return nil
	}
	if err := checkMulForAmbiguousReturn(e.Head); err != nil {
		return err
	}
	for _, op := range e.Tail {
		if err := checkMulForAmbiguousReturn(op.Rhs); err != nil {
			return err
		}
	}
	return nil
}

func checkMulForAmbiguousReturn(e *ast.MulExpr) error {
	if e == nil {
		return nil
	}
	if err := checkUnaryForAmbiguousReturn(e.Head); err != nil {
		return err
	}
	for _, op := range e.Tail {
		if err := checkUnaryForAmbiguousReturn(op.Rhs); err != nil {
			return err
		}
	}
	return nil
}

func checkUnaryForAmbiguousReturn(e *ast.Unary) error {
	if e == nil {
		return nil
	}
	return checkPostfixForAmbiguousReturn(e.Operand)
}

func checkPostfixForAmbiguousReturn(p *ast.Postfix) error {
	if p == nil {
		return nil
	}
	if err := checkPrimaryForAmbiguousReturn(p.Primary); err != nil {
		return err
	}
	for _, op := range p.Ops {
		switch {
		case op.Call != nil:
			for _, arg := range op.Call.Args {
				if err := checkExprForAmbiguousReturn(arg.Value); err != nil {
					return err
				}
			}
		case op.Slice != nil:
			if op.Slice.Low != nil {
				if err := checkExprForAmbiguousReturn(op.Slice.Low.Value); err != nil {
					return err
				}
			}
			if op.Slice.High != nil {
				if err := checkExprForAmbiguousReturn(op.Slice.High.Value); err != nil {
					return err
				}
			}
		case op.Index != nil:
			if err := checkExprForAmbiguousReturn(op.Index); err != nil {
				return err
			}
		case op.OptIndex != nil:
			if err := checkExprForAmbiguousReturn(op.OptIndex); err != nil {
				return err
			}
		}
	}
	return nil
}

func checkPrimaryForAmbiguousReturn(p *ast.Primary) error {
	if p == nil {
		return nil
	}
	switch {
	case p.Object != nil:
		for _, f := range p.Object.Fields {
			if err := checkExprForAmbiguousReturn(f.Value); err != nil {
				return err
			}
		}
	case p.Array != nil:
		for _, item := range p.Array.Items {
			if err := checkExprForAmbiguousReturn(item); err != nil {
				return err
			}
		}
	case p.IfExpr != nil:
		if err := checkExprForAmbiguousReturn(p.IfExpr.Cond); err != nil {
			return err
		}
		if err := checkExprForAmbiguousReturn(p.IfExpr.Then); err != nil {
			return err
		}
		if err := checkExprForAmbiguousReturn(p.IfExpr.Else); err != nil {
			return err
		}
	case p.Match != nil:
		if err := checkExprForAmbiguousReturn(p.Match.Subject); err != nil {
			return err
		}
		for _, arm := range p.Match.Arms {
			if err := checkExprForAmbiguousReturn(arm.Pattern); err != nil {
				return err
			}
			if err := checkExprForAmbiguousReturn(arm.Body); err != nil {
				return err
			}
		}
	case p.Lambda != nil:
		if err := checkParamDefaultsForAmbiguousReturn(p.Lambda.Params); err != nil {
			return err
		}
		if err := checkExprForAmbiguousReturn(p.Lambda.Body); err != nil {
			return err
		}
		return checkStatementsForAmbiguousReturn(p.Lambda.Block)
	case p.Agent != nil:
		return checkPropsForAmbiguousReturn(p.Agent.Props)
	case p.Sub != nil:
		return checkExprForAmbiguousReturn(p.Sub)
	}
	return nil
}
