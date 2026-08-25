package lint

import (
	"errors"
	"fmt"
	"strings"

	"github.com/alecthomas/participle/v2/lexer"

	"github.com/yanjustino/mhl-runtime/internal/features/prompt"
	"github.com/yanjustino/mhl-runtime/internal/lang/ast"
)

// checkAgentCalls statically mirrors the agent/memory-call checks that
// internal/engine/interpreter.RunStep performs at run time. The runtime now
// executes if/while/try for real (a step-scoped variable environment backs
// var/assign — see internal/engine/interpreter/eval.go), so lint recurses into those
// blocks too instead of skipping them, mirroring that change.
func checkAgentCalls(file string, prog *ast.Program) []Finding {
	var findings []Finding
	for _, decl := range prog.Decls {
		if decl.Pipeline == nil {
			continue
		}
		pipelineVars := collectPipelineVarNames(decl.Pipeline)
		for _, member := range decl.Pipeline.Body {
			if member.Step == nil {
				continue
			}
			// A pipeline-level `var` (PipelineMember.Var) is a valid plain-
			// assignment target inside any of that pipeline's steps too —
			// see interpreter.execAssign's pipelineEnv fallback — so it
			// must count as "declared" here the same way a step's own
			// `var` does, or this would false-positive "undefined
			// variable" on every step that mutates one.
			declared := collectVarNames(member.Step.Body)
			for name := range pipelineVars {
				declared[name] = true
			}
			findings = append(findings, checkStatements(file, prog, member.Step.Body, declared, nil)...)
		}
	}
	return findings
}

// collectPipelineVarNames returns the names p declares at its own top
// level (PipelineMember.Var, ast/pipeline.go) — mirrors
// interpreter.EvalPipelineVars, which is what actually seeds them at run
// time.
func collectPipelineVarNames(p *ast.Pipeline) map[string]bool {
	names := map[string]bool{}
	for _, member := range p.Body {
		if member.Var != nil {
			names[member.Var.Name] = true
		}
	}
	return names
}

// checkToolBlocks statically mirrors internal/engine/interpreter.evalToolCall's Block-body
// execution: it walks every declared `tool` method's Block (the same
// checkStatements/collectVarNames machinery checkAgentCalls uses for
// pipeline steps), so a tool method's own agent/memory calls, assigns, and
// return values get checked too. Single-expression Body methods have
// nothing to walk this way — checkExprCall only applies to statement
// positions with a lexer.Position to report against, and ToolMethod has
// none — so those remain unchecked beyond parsing, same as before this.
func checkToolBlocks(file string, prog *ast.Program) []Finding {
	var findings []Finding
	for _, decl := range prog.Decls {
		if decl.Tool == nil {
			continue
		}
		for _, m := range decl.Tool.Methods {
			if m.Block == nil {
				continue
			}
			declared := collectVarNames(m.Block)
			findings = append(findings, checkStatements(file, prog, m.Block, declared, decl.Tool)...)
		}
	}
	return findings
}

// collectVarNames returns every name declared with `var` anywhere in
// statements, recursing into if/while/try bodies. The runtime's variable
// environment (internal/engine/interpreter.Env) is flat and step-scoped, not block-scoped,
// so a `var` anywhere in the step makes that name a valid assign target
// anywhere else in the step. This is a structural approximation, not a
// flow-sensitive one — it can't tell whether the var statement actually
// runs before a given assign at runtime (e.g. one declared inside an `if`
// branch that isn't taken); that's the same kind of approximation the rest
// of lint already makes (see checkExprCall's doc comment).
func collectVarNames(statements []*ast.Statement) map[string]bool {
	names := map[string]bool{}
	var walk func([]*ast.Statement)
	walk = func(stmts []*ast.Statement) {
		for _, s := range stmts {
			switch {
			case s.Var != nil:
				names[s.Var.Name] = true
			case s.If != nil:
				walk(s.If.Then)
				walk(s.If.Else)
			case s.While != nil:
				walk(s.While.Body)
			case s.ForIn != nil:
				names[s.ForIn.VarName] = true
				walk(s.ForIn.Body)
			case s.Try != nil:
				walk(s.Try.Body)
				walk(s.Try.Catch)
				walk(s.Try.Finally)
			}
		}
	}
	walk(statements)
	return names
}

// selfTool is nil except when statements is a tool method's own Block
// (checkToolBlocks), in which case it's that tool — what a `self.method(...)`
// call inside these statements (or any if/while/try nested within them)
// resolves against, mirroring interpreter.evalToolCall's childCtx.selfTool.
func checkStatements(file string, prog *ast.Program, statements []*ast.Statement, declared map[string]bool, selfTool *ast.Tool) []Finding {
	var findings []Finding
	for _, statement := range statements {
		findings = append(findings, checkStatement(file, prog, statement, declared, selfTool)...)
	}
	return findings
}

func checkStatement(file string, prog *ast.Program, statement *ast.Statement, declared map[string]bool, selfTool *ast.Tool) []Finding {
	switch {
	case statement.Var != nil:
		return checkExprCall(file, prog, statement.Pos, statement.Var.Value, selfTool)
	case statement.Return != nil:
		if statement.Return.Value == nil {
			return nil
		}
		return checkExprCall(file, prog, statement.Pos, statement.Return.Value, selfTool)
	case statement.Expr != nil:
		return checkExprCall(file, prog, statement.Pos, statement.Expr.Expr, selfTool)
	case statement.Assign != nil:
		findings := checkAssignTarget(file, statement, declared)
		return append(findings, checkExprCall(file, prog, statement.Pos, statement.Assign.Value, selfTool)...)
	case statement.If != nil:
		findings := checkExprCall(file, prog, statement.Pos, statement.If.Cond, selfTool)
		findings = append(findings, checkStatements(file, prog, statement.If.Then, declared, selfTool)...)
		findings = append(findings, checkStatements(file, prog, statement.If.Else, declared, selfTool)...)
		return findings
	case statement.While != nil:
		findings := checkExprCall(file, prog, statement.Pos, statement.While.Cond, selfTool)
		findings = append(findings, checkStatements(file, prog, statement.While.Body, declared, selfTool)...)
		return findings
	case statement.ForIn != nil:
		findings := checkExprCall(file, prog, statement.Pos, statement.ForIn.Iterable, selfTool)
		findings = append(findings, checkStatements(file, prog, statement.ForIn.Body, declared, selfTool)...)
		return findings
	case statement.Try != nil:
		var findings []Finding
		findings = append(findings, checkStatements(file, prog, statement.Try.Body, declared, selfTool)...)
		findings = append(findings, checkStatements(file, prog, statement.Try.Catch, declared, selfTool)...)
		findings = append(findings, checkStatements(file, prog, statement.Try.Finally, declared, selfTool)...)
		return findings
	}
	return nil
}

// checkAssignTarget mirrors internal/engine/interpreter.execAssign's two static rules:
// the target must be a bare variable or an array-index chain (not a nested
// field), and its base name must have been `var`-declared somewhere in the
// step (see collectVarNames).
func checkAssignTarget(file string, statement *ast.Statement, declared map[string]bool) []Finding {
	name, ok := assignTargetBase(statement.Assign.Target)
	if !ok {
		return []Finding{{File: file, Line: statement.Pos.Line, Column: statement.Pos.Column,
			Message: "assignment target must be a plain variable or an array index, not a nested field"}}
	}
	if !declared[name] {
		return []Finding{{File: file, Line: statement.Pos.Line, Column: statement.Pos.Column,
			Message: fmt.Sprintf("undefined variable %q", name)}}
	}
	return nil
}

// assignTargetBase mirrors internal/engine/interpreter.execAssign's own
// assignTargetBase: a bare identifier, or an identifier followed only by
// array-index trailers (`arr[i]`, `matrix[i][j]`) — any Member or Call
// trailer in the chain makes it not an assignable target.
func assignTargetBase(p *ast.Postfix) (string, bool) {
	if p == nil || p.Primary == nil || p.Primary.Ident == "" {
		return "", false
	}
	for _, op := range p.Ops {
		if op.Index == nil {
			return "", false
		}
	}
	return p.Primary.Ident, true
}

// checkExprCall applies the narrow "is this a bare Agent.run(...) or
// memory.method(...) call" shape check to expr — the same check previously
// only applied to var/expression statements, now applied uniformly to
// if/while conditions and assign values too, since the runtime's evaluator
// (internal/engine/interpreter/eval.go) treats all of these as ordinary expressions. Like
// before, only an *unwrapped* call is recognized (barePostfix requires no
// surrounding operator) — `if (session_mem.get("x") == "y")` is not
// checked, matching the runtime's own narrow, literal-shape recognition for
// which calls get special dispatch vs. generic evaluation.
func checkExprCall(file string, prog *ast.Program, pos lexer.Position, expr *ast.Expr, selfTool *ast.Tool) []Finding {
	call, agentName, ok := agentRunCall(expr)
	if !ok {
		if memCall, target, method, mOk := methodCall(expr); mOk {
			switch {
			case nativeNamespaces[target]:
				// Reserved cmd/git/fs/http calls do real I/O (subprocess,
				// filesystem, network) — lint never executes anything, so
				// there's nothing to statically check beyond the shape
				// already matched here.
			case target == "self":
				if selfTool == nil {
					return []Finding{{File: file, Line: pos.Line, Column: pos.Column,
						Message: "self is only valid inside a tool method"}}
				}
				if err := checkToolCall(selfTool, method, memCall); err != nil {
					return []Finding{{File: file, Line: pos.Line, Column: pos.Column,
						Message: fmt.Sprintf("self.%s: %s", method, err)}}
				}
			default:
				if mem, found := findMemory(prog, target); found {
					if err := checkMemoryOp(mem, method, memCall); err != nil {
						return []Finding{{File: file, Line: pos.Line, Column: pos.Column,
							Message: fmt.Sprintf("%s.%s: %s", target, method, err)}}
					}
					break
				}
				if tool, found := findTool(prog, target); found {
					if err := checkToolCall(tool, method, memCall); err != nil {
						return []Finding{{File: file, Line: pos.Line, Column: pos.Column,
							Message: fmt.Sprintf("%s.%s: %s", target, method, err)}}
					}
					break
				}
				if isMemoryMethod(method) {
					return []Finding{{File: file, Line: pos.Line, Column: pos.Column,
						Message: fmt.Sprintf("memory %q not found", target)}}
				}
			}
		}
		return nil
	}

	agent, ok := findAgent(prog, agentName)
	if !ok {
		return []Finding{{File: file, Line: pos.Line, Column: pos.Column,
			Message: fmt.Sprintf("agent %q not found", agentName)}}
	}

	engine, _ := agentEngine(agent)
	switch {
	case engine == "" || strings.HasPrefix(engine, "cli/"):
		if _, _, err := agentCommand(agent); err != nil {
			return []Finding{{File: file, Line: agent.Pos.Line, Column: agent.Pos.Column, Message: err.Error()}}
		}
	case strings.HasPrefix(engine, "ollama/"):
		if _, _, _, err := agentOllamaConfig(agent, engine); err != nil {
			return []Finding{{File: file, Line: agent.Pos.Line, Column: agent.Pos.Column, Message: err.Error()}}
		}
	default:
		return []Finding{{File: file, Line: agent.Pos.Line, Column: agent.Pos.Column,
			Message: fmt.Sprintf("agent %q: engine %q is not supported yet", agentName, engine)}}
	}

	promptText, resolved, present, err := resolvePromptArgument(prog, call)
	if err != nil {
		return []Finding{{File: file, Line: pos.Line, Column: pos.Column,
			Message: fmt.Sprintf("%s.run: %s", agentName, err)}}
	}
	if !present || (resolved && promptText == "") {
		return []Finding{{File: file, Line: pos.Line, Column: pos.Column,
			Message: fmt.Sprintf("%s.run requires a non-empty prompt", agentName)}}
	}

	return nil
}

// --- lint-specific expression helpers (shared literal reading now lives in internal/lang/ast) -----
// (this pattern used to be duplicated across internal/engine/interpreter, internal/engine/runtime
// and internal/features/skills — all now share internal/lang/ast's literal readers)

func agentRunCall(expr *ast.Expr) (*ast.Call, string, bool) {
	postfix := ast.BarePostfix(expr)
	if postfix == nil || postfix.Primary == nil || postfix.Primary.Ident == "" || len(postfix.Ops) != 2 {
		return nil, "", false
	}
	if postfix.Ops[0].Member != "" && postfix.Ops[0].Member == "run" && postfix.Ops[1].Call != nil {
		return postfix.Ops[1].Call, postfix.Primary.Ident, true
	}
	return nil, "", false
}

func findAgent(prog *ast.Program, name string) (*ast.Agent, bool) {
	name = resolveName(prog, name)
	for _, decl := range prog.Decls {
		if decl.Agent != nil && decl.Agent.Name == name {
			return decl.Agent, true
		}
	}
	return nil, false
}

func findMemory(prog *ast.Program, name string) (*ast.Memory, bool) {
	name = resolveName(prog, name)
	for _, decl := range prog.Decls {
		if decl.Memory != nil && decl.Memory.Name == name {
			return decl.Memory, true
		}
	}
	return nil, false
}

func findTool(prog *ast.Program, name string) (*ast.Tool, bool) {
	name = resolveName(prog, name)
	for _, decl := range prog.Decls {
		if decl.Tool != nil && decl.Tool.Name == name {
			return decl.Tool, true
		}
	}
	return nil, false
}

// nativeNamespaces mirrors internal/engine/interpreter.nativeNamespaces: the reserved
// cmd/git/fs/http/json/log method-call targets (language-design.md §7) that are
// never looked up against user declarations.
var nativeNamespaces = map[string]bool{"cmd": true, "git": true, "fs": true, "http": true, "json": true, "log": true}

// checkToolCall mirrors internal/engine/interpreter.evalToolCall's validation: the method
// must exist on tool, and the call's argument count must match the
// method's declared Params exactly (tool method calls bind positionally —
// language-design.md §8's `execution.write_file(fix.file_path,
// fix.content)`, unlike `prompt Name(param: "x")`'s named convention).
// Purely structural — lint never executes a tool method body, so it can't
// validate what's inside it (e.g. a bad native-op argument) beyond this.
func checkToolCall(tool *ast.Tool, method string, call *ast.Call) error {
	var m *ast.ToolMethod
	for _, cand := range tool.Methods {
		if cand.Name == method {
			m = cand
			break
		}
	}
	if m == nil {
		return fmt.Errorf("tool %q has no method %q", tool.Name, method)
	}
	if len(call.Args) != len(m.Params) {
		return fmt.Errorf("tool %q: %s requires %d argument(s), got %d", tool.Name, method, len(m.Params), len(call.Args))
	}
	return nil
}

// methodCall recognizes `target.method(...)` — a bare identifier, one
// member access, then one call, e.g. `session_mem.set("key", "value")`.
func methodCall(expr *ast.Expr) (call *ast.Call, target, method string, ok bool) {
	postfix := ast.BarePostfix(expr)
	if postfix == nil || postfix.Primary == nil || postfix.Primary.Ident == "" || len(postfix.Ops) != 2 {
		return nil, "", "", false
	}
	if postfix.Ops[0].Member != "" && postfix.Ops[1].Call != nil {
		return postfix.Ops[1].Call, postfix.Primary.Ident, postfix.Ops[0].Member, true
	}
	return nil, "", "", false
}

func isMemoryMethod(method string) bool {
	return method == "set" || method == "get" || method == "append"
}

// memoryProp reads a named property off a memory declaration (e.g. "type",
// "store", "path").
func memoryProp(mem *ast.Memory, name string) (string, bool) {
	for _, prop := range mem.Props {
		if prop.Name == name {
			return ast.StringValue(prop.Value)
		}
	}
	return "", false
}

// maxValueDepth caps how deeply nested an array/object literal passed to a
// memory operation may be, so a malformed or pathological .mh file can't
// blow the stack during evaluation. Mirrors internal/engine/interpreter.maxValueDepth.
const maxValueDepth = 10

// errTooDeep marks a literalValue error as specifically the depth-limit
// error (as opposed to "this isn't a literal at all"), so checkMemoryOp can
// tell the two apart with errors.Is.
var errTooDeep = errors.New("value nesting too deep")

// literalValue mirrors internal/engine/interpreter.evalPrimary's literal cases: it
// evaluates expr as a JSON-compatible literal (string, number, bool, array,
// or object), recursively for array/object up to maxValueDepth levels of
// nesting, with no identifier/call resolution — purely static, same as
// everywhere else in lint. Since the runtime now accepts variables and
// nested calls as memory-op arguments too (not just literals), an error
// here only ever means "this argument isn't statically checkable" or "this
// literal is too deeply nested" (errors.Is(err, errTooDeep)) — never a hard
// lint failure on its own; see checkMemoryOp for how each is used.
func literalValue(expr *ast.Expr) (any, error) {
	return literalValueAt(expr, 0)
}

func literalValueAt(expr *ast.Expr, depth int) (any, error) {
	postfix := ast.BarePostfix(expr)
	if postfix == nil || postfix.Primary == nil || len(postfix.Ops) != 0 {
		return nil, fmt.Errorf("value must be a literal (string, number, bool, array, or object)")
	}
	p := postfix.Primary
	switch {
	case p.Str != nil:
		return *p.Str, nil
	case p.MultiStr != nil:
		return *p.MultiStr, nil
	case p.Number != nil:
		return *p.Number, nil
	case p.Bool != nil:
		return *p.Bool == "true", nil
	case p.Array != nil:
		if depth >= maxValueDepth {
			return nil, fmt.Errorf("value nesting exceeds the maximum depth of %d: %w", maxValueDepth, errTooDeep)
		}
		items := make([]any, 0, len(p.Array.Items))
		for _, item := range p.Array.Items {
			v, err := literalValueAt(item, depth+1)
			if err != nil {
				return nil, err
			}
			items = append(items, v)
		}
		return items, nil
	case p.Object != nil:
		if depth >= maxValueDepth {
			return nil, fmt.Errorf("value nesting exceeds the maximum depth of %d: %w", maxValueDepth, errTooDeep)
		}
		obj := make(map[string]any, len(p.Object.Fields))
		for _, f := range p.Object.Fields {
			var key string
			switch {
			case f.KeyStr != nil:
				key = *f.KeyStr
			case f.KeyIdent != nil:
				key = *f.KeyIdent
			}
			v, err := literalValueAt(f.Value, depth+1)
			if err != nil {
				return nil, err
			}
			obj[key] = v
		}
		return obj, nil
	default:
		return nil, fmt.Errorf("value must be a literal (string, number, bool, array, or object)")
	}
}

// checkMemoryOp mirrors internal/engine/interpreter.executeMemoryOp's validation as far as
// it can statically: argument *counts* are always checkable (they don't
// need any argument's value), but a "this must be a string" check can only
// fire when the argument in question is recognizably a literal — the
// runtime now also accepts variables and nested memory/agent calls as
// memory-op arguments (internal/engine/interpreter/eval.go's evalPositionalValues), whose
// type lint can't know without executing. Non-literal arguments are simply
// left unchecked here; the runtime still enforces its own type rules when
// it actually runs. Purely static either way — no store write, no file
// write, lint never has side effects.
func checkMemoryOp(mem *ast.Memory, method string, call *ast.Call) error {
	memType, _ := memoryProp(mem, "type")
	n := len(call.Args)

	checkStringArg := func(i int, label string) error {
		if i >= len(call.Args) {
			return nil
		}
		v, err := literalValue(call.Args[i].Value)
		if err != nil {
			return nil // not a literal — can't check statically, not an error
		}
		if _, ok := v.(string); !ok {
			return fmt.Errorf("memory %q: %s must be a string", mem.Name, label)
		}
		return nil
	}
	checkValueDepth := func(i int) error {
		if i >= len(call.Args) {
			return nil
		}
		if _, err := literalValue(call.Args[i].Value); err != nil && errors.Is(err, errTooDeep) {
			return err
		}
		return nil
	}

	switch memType {
	case "kv":
		memStore, _ := memoryProp(mem, "store")
		if memStore != "memory" {
			return fmt.Errorf("memory %q: store %q is not supported yet", mem.Name, memStore)
		}
		switch method {
		case "set":
			if n != 2 {
				return fmt.Errorf("memory %q: set requires (key, value)", mem.Name)
			}
			if err := checkStringArg(0, "key"); err != nil {
				return err
			}
			return checkValueDepth(1)
		case "get":
			if n != 1 && n != 2 {
				return fmt.Errorf("memory %q: get requires (key) or (key, default)", mem.Name)
			}
			return checkStringArg(0, "key")
		default:
			return fmt.Errorf("memory %q: kv memory has no method %q", mem.Name, method)
		}
	case "json":
		path, hasPath := memoryProp(mem, "path")
		if !hasPath || path == "" {
			return fmt.Errorf("memory %q has no path", mem.Name)
		}
		switch method {
		case "set":
			if n != 2 {
				return fmt.Errorf("memory %q: set requires (key, value)", mem.Name)
			}
			if err := checkStringArg(0, "key"); err != nil {
				return err
			}
			return checkValueDepth(1)
		case "get":
			if n != 1 && n != 2 {
				return fmt.Errorf("memory %q: get requires (key) or (key, default)", mem.Name)
			}
			return checkStringArg(0, "key")
		default:
			return fmt.Errorf("memory %q: json memory has no method %q", mem.Name, method)
		}
	case "append_log":
		path, hasPath := memoryProp(mem, "path")
		if !hasPath || path == "" {
			return fmt.Errorf("memory %q has no path", mem.Name)
		}
		if method != "append" {
			return fmt.Errorf("memory %q: append_log memory has no method %q", mem.Name, method)
		}
		if n != 1 {
			return fmt.Errorf("memory %q: append requires (text)", mem.Name)
		}
		if v, err := literalValue(call.Args[0].Value); err == nil {
			if _, ok := v.(string); !ok {
				return fmt.Errorf("memory %q: append_log entries must be plain text (a string), not a structured value", mem.Name)
			}
		}
	case "jsonl":
		path, hasPath := memoryProp(mem, "path")
		if !hasPath || path == "" {
			return fmt.Errorf("memory %q has no path", mem.Name)
		}
		if method != "append" {
			return fmt.Errorf("memory %q: jsonl memory has no method %q", mem.Name, method)
		}
		if n != 1 {
			return fmt.Errorf("memory %q: append requires (text)", mem.Name)
		}
	case "":
		return fmt.Errorf("memory %q has no type", mem.Name)
	default:
		return fmt.Errorf("memory %q: type %q is not supported yet", mem.Name, memType)
	}
	return nil
}

func agentCommand(agent *ast.Agent) (string, []string, error) {
	command := ""
	var args []string
	for _, prop := range agent.Props {
		switch prop.Name {
		case "command":
			command, _ = ast.StringValue(prop.Value)
		case "args":
			var ok bool
			args, ok = ast.StringArrayValue(prop.Value)
			if !ok {
				return "", nil, fmt.Errorf("agent %q args must be an array of strings", agent.Name)
			}
		}
	}
	if command == "" {
		return "", nil, fmt.Errorf("agent %q has no command", agent.Name)
	}
	return command, args, nil
}

// agentEngine reads the agent's engine property. ok is false when the
// property is absent or not a string.
func agentEngine(agent *ast.Agent) (string, bool) {
	for _, prop := range agent.Props {
		if prop.Name == "engine" {
			return ast.StringValue(prop.Value)
		}
	}
	return "", false
}

// agentOllamaConfig mirrors internal/engine/interpreter.agentOllamaConfig: it reads the
// endpoint/temperature configuration for an ollama/* engine agent, purely
// statically (no network call — lint never executes anything). model is
// derived from engine (the part after "ollama/"). temperature is nil when
// the agent declares no temperature property.
func agentOllamaConfig(agent *ast.Agent, engine string) (endpoint, model string, temperature *float64, err error) {
	model = strings.TrimPrefix(engine, "ollama/")
	for _, prop := range agent.Props {
		switch prop.Name {
		case "endpoint":
			endpoint, _ = ast.StringValue(prop.Value)
		case "temperature":
			t, ok := ast.NumberValue(prop.Value)
			if !ok {
				return "", "", nil, fmt.Errorf("agent %q temperature must be a number", agent.Name)
			}
			temperature = &t
		}
	}
	if endpoint == "" {
		return "", "", nil, fmt.Errorf("agent %q has no endpoint", agent.Name)
	}
	return endpoint, model, temperature, nil
}

// resolvePromptArgument statically validates what it can prove about call's
// `prompt:` argument: a plain string literal, or a reference to a declared
// `prompt Name(...) { "..." }` template (recursively resolving any nested
// prompt-reference argument — see renderPromptCallArgs). present reports
// whether a `prompt:` argument exists at all — lint's only real "requires a
// non-empty prompt" signal. Since internal/engine/interpreter.
// resolvePromptArgument now accepts *any* expression evaluating to a string
// there (a variable, string concatenation, ...), not just these two literal
// shapes, present-but-not-`resolved` isn't an error lint can raise: lint
// never evaluates anything, so it has no way to know such an expression is
// empty (or even that it isn't itself a valid prompt) — only the runtime
// does. resolved is true only when text was actually computed.
func resolvePromptArgument(prog *ast.Program, call *ast.Call) (text string, resolved, present bool, err error) {
	for _, arg := range call.Args {
		if arg.Name != "prompt" {
			continue
		}
		present = true
		if s, ok := ast.StringValue(arg.Value); ok {
			return s, true, true, nil
		}
		promptCall, name, ok := promptRefCall(arg.Value)
		if !ok {
			return "", false, true, nil
		}
		rendered, r, err := renderPromptCall(prog, name, promptCall)
		if err != nil {
			return "", false, true, err
		}
		return rendered, r, true, nil
	}
	return "", false, false, nil
}

// renderPromptCall resolves the declared prompt template named name and
// statically validates call's arguments against it (renderPromptCallArgs) —
// every argument named, matching a declared parameter, and every declared
// parameter supplied — regardless of whether each argument's *value* is
// itself statically known. resolved is true only when every argument's
// value was known too, in which case text is the fully rendered template;
// mirrors internal/engine/interpreter.renderPromptCallDynamic's structural
// checks (prompt.Render's own parameter validation), just without the
// runtime values needed to compute text when resolved is false.
func renderPromptCall(prog *ast.Program, name string, call *ast.Call) (text string, resolved bool, err error) {
	decl, ok := findPrompt(prog, name)
	if !ok {
		return "", false, fmt.Errorf("prompt %q not found", name)
	}
	values, resolved, err := renderPromptCallArgs(prog, decl, call)
	if err != nil {
		return "", false, fmt.Errorf("prompt %q: %w", name, err)
	}
	if !resolved {
		return "", false, nil
	}
	rendered, err := prompt.Render(decl, values)
	if err != nil {
		return "", false, fmt.Errorf("prompt %q: %w", name, err)
	}
	return rendered, true, nil
}

func promptRefCall(expr *ast.Expr) (*ast.Call, string, bool) {
	postfix := ast.BarePostfix(expr)
	if postfix == nil || postfix.Primary == nil || postfix.Primary.Ident == "" || len(postfix.Ops) != 1 {
		return nil, "", false
	}
	if postfix.Ops[0].Call != nil {
		return postfix.Ops[0].Call, postfix.Primary.Ident, true
	}
	return nil, "", false
}

func findPrompt(prog *ast.Program, name string) (*ast.Prompt, bool) {
	name = resolveName(prog, name)
	for _, decl := range prog.Decls {
		if decl.Prompt != nil && decl.Prompt.Name == name {
			return decl.Prompt, true
		}
	}
	return nil, false
}

// renderPromptCallArgs validates call's arguments against decl's declared
// parameters — every argument named and matching a declared parameter
// exactly once, and every declared parameter supplied — the same
// structural checks prompt.Render itself makes before ever touching
// placeholder text (internal/features/prompt/render.go), decoupled here
// from whether each argument's *value* is statically known. A value that's
// a string literal or a (recursively resolved) nested prompt reference is
// collected into values; any other expression shape (a variable, string
// concatenation, `feature.title`, ...) can be anything at runtime now
// (renderPromptCallDynamic, internal/engine/interpreter/prompt_ops.go), so
// lint accepts the shape — it's still a validly-named argument — but
// reports resolved=false rather than guessing at a value.
func renderPromptCallArgs(prog *ast.Program, decl *ast.Prompt, call *ast.Call) (values map[string]string, resolved bool, err error) {
	declared := make(map[string]bool, len(decl.Params))
	for _, p := range decl.Params {
		declared[p.Name] = true
	}
	supplied := make(map[string]bool, len(call.Args))
	values = make(map[string]string, len(call.Args))
	resolved = true
	for _, arg := range call.Args {
		if arg.Name == "" {
			return nil, false, fmt.Errorf("arguments must be named")
		}
		if !declared[arg.Name] {
			return nil, false, fmt.Errorf("unexpected argument %q", arg.Name)
		}
		if supplied[arg.Name] {
			return nil, false, fmt.Errorf("argument %q supplied more than once", arg.Name)
		}
		supplied[arg.Name] = true
		if s, ok := ast.StringValue(arg.Value); ok {
			values[arg.Name] = s
			continue
		}
		nestedCall, nestedName, ok := promptRefCall(arg.Value)
		if !ok {
			resolved = false
			continue
		}
		rendered, nestedResolved, err := renderPromptCall(prog, nestedName, nestedCall)
		if err != nil {
			return nil, false, err
		}
		if !nestedResolved {
			resolved = false
			continue
		}
		values[arg.Name] = rendered
	}
	for name := range declared {
		if !supplied[name] {
			return nil, false, fmt.Errorf("missing value for parameter %q", name)
		}
	}
	return values, resolved, nil
}
