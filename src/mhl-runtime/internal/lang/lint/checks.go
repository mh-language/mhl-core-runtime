package lint

import (
	"errors"
	"fmt"
	"strings"

	"github.com/alecthomas/participle/v2/lexer"

	"github.com/mh-language/mhl-core-runtime/internal/features/prompt"
	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
	"github.com/mh-language/mhl-core-runtime/internal/lang/types"
)

// checkAgentCalls statically mirrors the agent/memory-call checks that
// internal/engine/interpreter.RunStep performs at run time. The runtime now
// executes if/while/try for real (a step-scoped variable environment backs
// var/assign — see internal/engine/interpreter/eval.go), so lint recurses into those
// blocks too instead of skipping them, mirroring that change.
func checkAgentCalls(file string, prog *ast.Program, aliases map[string]types.Type) []Finding {
	var findings []Finding
	for _, decl := range prog.Decls {
		if decl.Pipeline == nil {
			continue
		}
		pipelineInputs := pipelineInputTypes(decl.Pipeline, aliases)
		pipelineVars := collectPipelineVarNames(prog, decl.Pipeline)
		pipelineMemVars := collectPipelineMemNames(prog, decl.Pipeline)
		for _, member := range decl.Pipeline.Body {
			// A `parallel` group's branch steps are checked exactly like
			// plain steps — the runtime runs them through the same RunStep.
			var steps []*ast.Step
			switch {
			case member.Step != nil:
				steps = []*ast.Step{member.Step}
			case member.Parallel != nil:
				steps = member.Parallel.Steps
			default:
				continue
			}
			for _, step := range steps {
				// A pipeline-level `input`, `var`, or `mem` is a valid
				// plain-assignment target inside any of that pipeline's steps
				// too — see interpreter.execAssign's pipelineEnv and mem
				// fallbacks — so all three must count as "declared" here the
				// same way a step's own `var` does, or this would
				// false-positive "undefined variable" on every step that
				// mutates one. Seeded fresh per step (a real copy, not a
				// shared reference): a step-local var/mem downgrade must not
				// leak into another step's belief about the same
				// pipeline-level binding.
				seed := make(map[string]types.Type, len(pipelineInputs)+len(pipelineVars)+len(pipelineMemVars))
				for name, t := range pipelineInputs {
					seed[name] = t
				}
				for name, t := range pipelineVars {
					seed[name] = t
				}
				for name, t := range pipelineMemVars {
					seed[name] = t
				}
				// A `context:` block makes the read-only identifier `context`
				// resolve to an object inside every step (interpreter.isContextRef).
				if pipelineHasContextProp(decl.Pipeline) {
					seed["context"] = types.Object
				}
				declared := collectVarNames(prog, step.Body, seed, nil)
				findings = append(findings, checkStatements(file, prog, step.Body, declared, nil, aliases)...)
			}
		}
	}
	return findings
}

// checkParallelGroups statically enforces the rules a `parallel` group's
// atomic-checkpoint / concurrent-branch model depends on, mirroring the
// guards runtime.Runner.Run applies at run time:
//   - every step name in a pipeline is unique (a `parallel` branch step
//     included) — findStep/goto resolve a name to the first match, so a
//     duplicate is silently ambiguous;
//   - `goto` is not used from inside a branch step, and does not target a
//     step that lives inside a group (either would need the pipeline state
//     machine to jump into or out of a barrier);
//   - `break` is not used from inside a branch step (it unwinds the whole
//     pipeline/loop — meaningless from one concurrent branch).
func checkParallelGroups(file string, prog *ast.Program) []Finding {
	var findings []Finding
	for _, decl := range prog.Decls {
		if decl.Pipeline == nil {
			continue
		}

		// Which step names belong to a parallel group, and every step name
		// seen so far (for the uniqueness check).
		inGroup := map[string]bool{}
		seen := map[string]bool{}
		for _, m := range decl.Pipeline.Body {
			switch {
			case m.Step != nil:
				if seen[m.Step.Name] {
					findings = append(findings, Finding{File: file, Line: stepLine(m.Step), Column: 1,
						Message: fmt.Sprintf("pipeline %q declares more than one step named %q", decl.Pipeline.Name, m.Step.Name)})
				}
				seen[m.Step.Name] = true
			case m.Parallel != nil:
				for _, s := range m.Parallel.Steps {
					inGroup[s.Name] = true
					if seen[s.Name] {
						findings = append(findings, Finding{File: file, Line: m.Parallel.Pos.Line, Column: m.Parallel.Pos.Column,
							Message: fmt.Sprintf("pipeline %q declares more than one step named %q", decl.Pipeline.Name, s.Name)})
					}
					seen[s.Name] = true
				}
			}
		}

		for _, m := range decl.Pipeline.Body {
			// `goto <T>` anywhere in the pipeline may not target a grouped step.
			steps := pipelineMemberSteps(m)
			branch := m.Parallel != nil
			for _, step := range steps {
				walkGotoBreak(step.Body, func(target string, isBreak bool, pos lexer.Position) {
					switch {
					case isBreak && branch:
						findings = append(findings, Finding{File: file, Line: pos.Line, Column: pos.Column,
							Message: fmt.Sprintf("`break` cannot be used inside a parallel group (step %q of group %q)", step.Name, m.Parallel.Name)})
					case !isBreak && branch:
						findings = append(findings, Finding{File: file, Line: pos.Line, Column: pos.Column,
							Message: fmt.Sprintf("`goto` cannot be used inside a parallel group (step %q of group %q)", step.Name, m.Parallel.Name)})
					case !isBreak && inGroup[target]:
						findings = append(findings, Finding{File: file, Line: pos.Line, Column: pos.Column,
							Message: fmt.Sprintf("`goto %s` targets a step inside a parallel group", target)})
					}
				})
			}
		}
	}
	return findings
}

// knownAgentProperties is the set of `agent { ... }` property names the
// runtime actually reads (internal/engine/interpreter/agent.go,
// internal/lang/ast/agentconfig.go, agent_hooks.go). Anything else in an
// agent body parses but is silently ignored at run time — the same footgun
// class as a misspelled `retry.backoff` value, which is already a hard
// error, so an unknown property is one too. Keep in sync with
// internal/lsp/properties.go's agentPropertyItems.
var knownAgentProperties = map[string]bool{
	"engine": true, "command": true, "args": true,
	"endpoint": true, "temperature": true,
	"log": true, "trace": true,
	"retry": true, "cache": true, "rate_limit": true, "fallback": true,
	"before": true, "after": true,
}

// checkAgentProperties flags any property in an `agent { ... }` body — a
// top-level agent or an inline `fallback: [agent { ... }]` literal — whose
// name the runtime does not read, so a typo or a docs-only field
// (`api_key`, `timeout`, `system_instructions`) fails `mhl lint` instead of
// being silently dropped.
func checkAgentProperties(file string, prog *ast.Program) []Finding {
	var findings []Finding
	seen := map[*ast.Agent]bool{}
	var check func(a *ast.Agent)
	check = func(a *ast.Agent) {
		if a == nil || seen[a] {
			return
		}
		seen[a] = true
		name := a.Name
		if name == "" {
			name = "<inline>"
		}
		for _, p := range a.Props {
			if !knownAgentProperties[p.Name] {
				findings = append(findings, Finding{File: file, Line: p.Pos.Line, Column: p.Pos.Column,
					Message: fmt.Sprintf("agent %q: unknown property %q — the runtime reads only engine, command, args, endpoint, temperature, log, trace, retry, cache, rate_limit, fallback, before, after", name, p.Name)})
			}
		}
		if refs, err := ast.AgentFallbackRefs(a); err == nil {
			for _, r := range refs {
				check(r.Inline)
			}
		}
	}
	for _, decl := range prog.Decls {
		if decl.Agent != nil {
			check(decl.Agent)
		}
	}
	return findings
}

// knownPipelineProperties is the set of bare `name: ...` properties a
// `pipeline` / `workflow` body may carry (runtime.PipelineFromAST's Prop.Name
// switch), plus `description` (read by the serve adapters, not the runner).
// `input` / `var` / `mem` / `const` / `step` / `parallel` are distinct body
// members, not Property nodes, so they never reach here. Keep in sync with
// internal/lsp/properties.go's pipelinePropertyItems.
var knownPipelineProperties = map[string]bool{
	"description": true,
	"checkpoint":  true,
	"spawn":       true,
	"repeat":      true,
	"context":     true,
}

// checkPipelineProperties flags a bare property in a pipeline/workflow body
// whose name nothing reads — a typo (`checkpont:`) or a docs-only field —
// rather than silently ignoring it, matching checkAgentProperties.
func checkPipelineProperties(file string, prog *ast.Program) []Finding {
	var findings []Finding
	for _, decl := range prog.Decls {
		if decl.Pipeline == nil {
			continue
		}
		for _, m := range decl.Pipeline.Body {
			if m.Prop == nil || knownPipelineProperties[m.Prop.Name] {
				continue
			}
			findings = append(findings, Finding{File: file, Line: m.Prop.Pos.Line, Column: m.Prop.Pos.Column,
				Message: fmt.Sprintf("%s %q: unknown property %q — the body reads only description, checkpoint, spawn, repeat, context", pipelineKind(decl.Pipeline), decl.Pipeline.Name, m.Prop.Name)})
		}
	}
	return findings
}

// pipelineKind is "workflow" or "pipeline" for a diagnostic prefix.
func pipelineKind(p *ast.Pipeline) string {
	if p.IsWorkflow() {
		return "workflow"
	}
	return "pipeline"
}

// checkPipelineGoto enforces the two static rules on `goto`:
//   - it is only legal inside a `workflow` (a plain `pipeline` runs its steps
//     in declared order, each once — that guarantee is the whole reason the
//     two keywords are separate);
//   - its target must name a step declared in the same declaration — every
//     other cross-reference in the language is caught before `mhl run`, and a
//     typo'd `goto` target should be too, rather than only failing mid-run
//     (runtime.Runner.Run's own "goto target %q is not a step" error).
//
// The parallel-group restrictions on `goto` are checkParallelGroups' job and
// stay there; this check does not re-report them.
func checkPipelineGoto(file string, prog *ast.Program) []Finding {
	var findings []Finding
	for _, decl := range prog.Decls {
		if decl.Pipeline == nil {
			continue
		}
		p := decl.Pipeline

		steps := map[string]bool{}
		for _, m := range p.Body {
			for _, s := range pipelineMemberSteps(m) {
				steps[s.Name] = true
			}
		}

		for _, m := range p.Body {
			for _, step := range pipelineMemberSteps(m) {
				walkGotoBreak(step.Body, func(target string, isBreak bool, pos lexer.Position) {
					if isBreak {
						return
					}
					if !p.IsWorkflow() {
						findings = append(findings, Finding{File: file, Line: pos.Line, Column: pos.Column,
							Message: fmt.Sprintf("`goto` is only valid inside a `workflow`; pipeline %q runs its steps in order — change `pipeline` to `workflow`, or restructure the jump", p.Name)})
						return
					}
					if !steps[target] {
						findings = append(findings, Finding{File: file, Line: pos.Line, Column: pos.Column,
							Message: fmt.Sprintf("`goto %s` in workflow %q targets a step that isn't declared in it", target, p.Name)})
					}
				})
			}
		}
	}
	return findings
}

// pipelineMemberSteps returns the step(s) a pipeline body member contributes
// — one for a plain `step`, all branches for a `parallel` group, none
// otherwise.
func pipelineMemberSteps(m *ast.PipelineMember) []*ast.Step {
	switch {
	case m.Step != nil:
		return []*ast.Step{m.Step}
	case m.Parallel != nil:
		return m.Parallel.Steps
	default:
		return nil
	}
}

// stepLine reports a step's source line, falling back to 1 when the step's
// first statement carries no position (an empty body).
func stepLine(s *ast.Step) int {
	if len(s.Body) > 0 && s.Body[0].Pos.Line > 0 {
		return s.Body[0].Pos.Line
	}
	return 1
}

// walkGotoBreak invokes fn for every `goto` and `break` statement reachable
// in stmts, recursing into if/while/for/try bodies (the same structural,
// non-flow-sensitive traversal collectVarNames uses).
func walkGotoBreak(stmts []*ast.Statement, fn func(target string, isBreak bool, pos lexer.Position)) {
	for _, s := range stmts {
		switch {
		case s.Goto != nil:
			fn(s.Goto.Target, false, s.Pos)
		case s.Break != nil:
			fn("", true, s.Pos)
		case s.If != nil:
			walkGotoBreak(s.If.Then, fn)
			walkGotoBreak(s.If.Else, fn)
		case s.While != nil:
			walkGotoBreak(s.While.Body, fn)
		case s.ForIn != nil:
			walkGotoBreak(s.ForIn.Body, fn)
		case s.Try != nil:
			walkGotoBreak(s.Try.Body, fn)
			walkGotoBreak(s.Try.Catch, fn)
			walkGotoBreak(s.Try.Finally, fn)
		}
	}
}

// collectPipelineVarNames returns the names (and best statically-known
// type — see mergeVarType) p declares at its own top level with `var`
// (PipelineMember.Var, ast/pipeline.go) — mirrors interpreter.EvalPipelineVars,
// which is what actually seeds them at run time. Deliberately excludes
// `mem` (collectPipelineMemNames, below): checkLoopStopWhen (loop.go) uses
// this one alone specifically because a `mem` reference in stop_when is
// fine — only a `var` one is the footgun that check exists to catch.
func collectPipelineVarNames(prog *ast.Program, p *ast.Pipeline) map[string]types.Type {
	known := map[string]types.Type{}
	for _, member := range p.Body {
		switch {
		case member.Const != nil:
			mergeVarType(known, prog, member.Const.Name, member.Const.Value, nil)
		case member.Var != nil:
			mergeVarType(known, prog, member.Var.Name, member.Var.Value, nil) // no `self` at pipeline scope
		}
	}
	return known
}

// collectPipelineMemNames returns the names (and best statically-known
// type) p declares at its own top level with `mem` (PipelineMember.Mem) —
// mirrors interpreter.PipelineMemInit. Kept separate from
// collectPipelineVarNames (above) rather than folded into it: callers
// that need "every valid pipeline-level assignment target" (checkAgentCalls)
// combine both; checkLoopStopWhen wants var names only. Inferred the same
// way as `var` — only the declared initializer expression's shape matters
// here; `mem`'s get-or-init runtime semantics are irrelevant to a purely
// static reader.
func collectPipelineMemNames(prog *ast.Program, p *ast.Pipeline) map[string]types.Type {
	known := map[string]types.Type{}
	for _, member := range p.Body {
		if member.Mem != nil {
			mergeVarType(known, prog, member.Mem.Name, member.Mem.Value, nil)
		}
	}
	return known
}

// checkToolBlocks statically mirrors internal/engine/interpreter.evalToolCall's Block-body
// execution: it walks every declared `tool` method's Block (the same
// checkStatements/collectVarNames machinery checkAgentCalls uses for
// checkExtensions validates the static config of every extension-shaped
// declaration — `mcp_server`, `a2a_agent`, and the generic
// `extension <kind> <Name>` form, all seen through ast.AsExtension. Adding a
// new extension kind needs no edit here: only the one kind-specific rule that
// predates the extension refactor and has its own lint tests — an `mcp`
// server's `protocol:` value — is checked; everything else an extension needs
// validated it validates itself at run time (extension.Extension.Validate),
// keeping lint network-free.
func checkExtensions(file string, prog *ast.Program) []Finding {
	var findings []Finding
	for _, decl := range prog.Decls {
		kind, name, props, ok := ast.AsExtension(decl)
		if !ok || kind != "mcp" {
			continue
		}
		if _, err := ast.MCPProtocolFromProps(name, props); err != nil {
			pos := propertyPos(props, "protocol")
			findings = append(findings, Finding{
				File: file, Line: pos.Line, Column: pos.Column, Message: err.Error(),
			})
		}
	}
	return findings
}

// propertyPos returns the position of the named property, or the zero position
// if it is absent.
func propertyPos(props []*ast.Property, name string) lexer.Position {
	for _, p := range props {
		if p != nil && p.Name == name {
			return p.Pos
		}
	}
	return lexer.Position{}
}

// pipeline steps), so a tool method's own agent/memory calls, assigns, and
// return values get checked too. Single-expression Body methods have
// nothing to walk this way — checkExprCall only applies to statement
// positions with a lexer.Position to report against, and ToolMethod has
// none — so those remain unchecked beyond parsing, same as before this.
func checkToolBlocks(file string, prog *ast.Program, aliases map[string]types.Type) []Finding {
	var findings []Finding
	for _, decl := range prog.Decls {
		if decl.Tool == nil {
			continue
		}
		for _, m := range decl.Tool.Methods {
			params := toolMethodParamTypes(m, aliases)
			if m.Body != nil {
				// A single-expression body is not walked by checkStatements;
				// still validate any `match` inside it.
				findings = append(findings, checkMatchInExpr(file, prog, m.Body, params, aliases)...)
				continue
			}
			if m.Block == nil {
				continue
			}
			declared := collectVarNames(prog, m.Block, params, decl.Tool)
			findings = append(findings, checkStatements(file, prog, m.Block, declared, decl.Tool, aliases)...)
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
//
// Beyond names, this also infers each variable's best statically-known
// Type (types.Any when unknown) via mergeVarType — a single, non-flow-
// sensitive forward pass: every branch (If/While/ForIn/Try) is visited
// unconditionally, same as before, and a name's type only ever downgrades
// to Any on conflict/uncertainty, never back to something more specific.
// seed pre-populates the map (e.g. a pipeline's typed `input`s/`var`s/`mem`s
// before a step's own statements run) and may be nil for a fresh map.
func collectVarNames(prog *ast.Program, statements []*ast.Statement, seed map[string]types.Type, selfTool *ast.Tool) map[string]types.Type {
	known := seed
	if known == nil {
		known = map[string]types.Type{}
	}
	var walk func([]*ast.Statement)
	walk = func(stmts []*ast.Statement) {
		for _, s := range stmts {
			switch {
			case s.Var != nil:
				mergeVarType(known, prog, s.Var.Name, s.Var.Value, selfTool)
			case s.Const != nil:
				// A `const` name is a readable binding like `var` — infer its
				// type so a read isn't a false "undefined variable".
				// checkConstReassign separately forbids writing to it.
				mergeVarType(known, prog, s.Const.Name, s.Const.Value, selfTool)
			case s.Spawn != nil:
				// A spawn binds a task handle — no static Type for it, but
				// the name must count as declared so a later `wait s` /
				// `s.result` isn't flagged "undefined".
				known[s.Spawn.Name] = types.Any
			case s.Assign != nil:
				if name, ok := bareAssignName(s.Assign.Target); ok {
					if _, declared := known[name]; declared {
						mergeVarType(known, prog, name, s.Assign.Value, selfTool)
					}
				}
			case s.If != nil:
				walk(s.If.Then)
				walk(s.If.Else)
			case s.While != nil:
				walk(s.While.Body)
			case s.ForIn != nil:
				if _, declared := known[s.ForIn.VarName]; !declared {
					known[s.ForIn.VarName] = types.Any // element type: out of scope for v1
				}
				walk(s.ForIn.Body)
			case s.Try != nil:
				walk(s.Try.Body)
				walk(s.Try.Catch)
				walk(s.Try.Finally)
			}
		}
	}
	walk(statements)
	return known
}

// selfTool is nil except when statements is a tool method's own Block
// (checkToolBlocks), in which case it's that tool — what a `self.method(...)`
// call inside these statements (or any if/while/try nested within them)
// resolves against, mirroring interpreter.evalToolCall's childCtx.selfTool.
func checkStatements(file string, prog *ast.Program, statements []*ast.Statement, declared map[string]types.Type, selfTool *ast.Tool, aliases map[string]types.Type) []Finding {
	var findings []Finding
	for _, statement := range statements {
		findings = append(findings, checkStatement(file, prog, statement, declared, selfTool, aliases)...)
	}
	return findings
}

func checkStatement(file string, prog *ast.Program, statement *ast.Statement, declared map[string]types.Type, selfTool *ast.Tool, aliases map[string]types.Type) []Finding {
	switch {
	case statement.Var != nil:
		return checkExprCall(file, prog, statement.Pos, statement.Var.Value, declared, selfTool, aliases)
	case statement.Return != nil:
		if statement.Return.Value == nil {
			return nil
		}
		return checkExprCall(file, prog, statement.Pos, statement.Return.Value, declared, selfTool, aliases)
	case statement.Spawn != nil:
		return checkSpawnStmt(file, prog, statement, declared, selfTool, aliases)
	case statement.Wait != nil:
		return checkWaitStmt(file, statement, declared, selfTool)
	case statement.Expr != nil:
		return checkExprCall(file, prog, statement.Pos, statement.Expr.Expr, declared, selfTool, aliases)
	case statement.Assign != nil:
		findings := checkAssignTarget(file, statement, declared)
		return append(findings, checkExprCall(file, prog, statement.Pos, statement.Assign.Value, declared, selfTool, aliases)...)
	case statement.If != nil:
		findings := checkExprCall(file, prog, statement.Pos, statement.If.Cond, declared, selfTool, aliases)
		findings = append(findings, checkStatements(file, prog, statement.If.Then, declared, selfTool, aliases)...)
		findings = append(findings, checkStatements(file, prog, statement.If.Else, declared, selfTool, aliases)...)
		return findings
	case statement.While != nil:
		findings := checkExprCall(file, prog, statement.Pos, statement.While.Cond, declared, selfTool, aliases)
		findings = append(findings, checkStatements(file, prog, statement.While.Body, declared, selfTool, aliases)...)
		return findings
	case statement.ForIn != nil:
		findings := checkExprCall(file, prog, statement.Pos, statement.ForIn.Iterable, declared, selfTool, aliases)
		findings = append(findings, checkStatements(file, prog, statement.ForIn.Body, declared, selfTool, aliases)...)
		return findings
	case statement.Try != nil:
		var findings []Finding
		findings = append(findings, checkStatements(file, prog, statement.Try.Body, declared, selfTool, aliases)...)
		findings = append(findings, checkStatements(file, prog, statement.Try.Catch, declared, selfTool, aliases)...)
		findings = append(findings, checkStatements(file, prog, statement.Try.Finally, declared, selfTool, aliases)...)
		return findings
	}
	return nil
}

// checkSpawnStmt mirrors interpreter.execSpawn's static rules: `spawn` is a
// step-only statement, and its right-hand side must be an `<Agent>.run(...)`
// call — which is then validated (known agent, non-empty prompt) exactly
// like a plain `Agent.run(...)` statement.
func checkSpawnStmt(file string, prog *ast.Program, statement *ast.Statement, declared map[string]types.Type, selfTool *ast.Tool, aliases map[string]types.Type) []Finding {
	pos := statement.Pos
	if selfTool != nil {
		return []Finding{{File: file, Line: pos.Line, Column: pos.Column,
			Message: "spawn is only valid inside a pipeline step, not a tool method"}}
	}
	if _, _, ok := agentRunCall(statement.Spawn.Call); !ok {
		return []Finding{{File: file, Line: pos.Line, Column: pos.Column,
			Message: fmt.Sprintf("spawn %q: right-hand side must be an <Agent>.run(...) call", statement.Spawn.Name)}}
	}
	return checkExprCall(file, prog, pos, statement.Spawn.Call, declared, selfTool, aliases)
}

// checkWaitStmt mirrors interpreter.execWait's static rules: `wait` is a
// step-only statement and every name it lists must have been introduced in
// this step (by `spawn`, in practice — collectVarNames records the handle
// name).
func checkWaitStmt(file string, statement *ast.Statement, declared map[string]types.Type, selfTool *ast.Tool) []Finding {
	pos := statement.Pos
	if selfTool != nil {
		return []Finding{{File: file, Line: pos.Line, Column: pos.Column,
			Message: "wait is only valid inside a pipeline step, not a tool method"}}
	}
	var findings []Finding
	seen := map[string]bool{}
	for _, name := range statement.Wait.Names {
		if seen[name] {
			findings = append(findings, Finding{File: file, Line: pos.Line, Column: pos.Column,
				Message: fmt.Sprintf("wait: handle %q listed twice", name)})
			continue
		}
		seen[name] = true
		if _, ok := declared[name]; !ok {
			findings = append(findings, Finding{File: file, Line: pos.Line, Column: pos.Column,
				Message: fmt.Sprintf("wait: %q is not a spawned handle in this step", name)})
		}
	}
	return findings
}

// checkAssignTarget mirrors internal/engine/interpreter.execAssign's two static rules:
// the target must be a bare variable or an array-index chain (not a nested
// field), and its base name must have been `var`-declared somewhere in the
// step (see collectVarNames).
func checkAssignTarget(file string, statement *ast.Statement, declared map[string]types.Type) []Finding {
	name, ok := assignTargetBase(statement.Assign.Target)
	if !ok {
		return []Finding{{File: file, Line: statement.Pos.Line, Column: statement.Pos.Column,
			Message: "assignment target must be a plain variable or an array index, not a nested field"}}
	}
	if _, ok := declared[name]; !ok {
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
func checkExprCall(file string, prog *ast.Program, pos lexer.Position, expr *ast.Expr, declared map[string]types.Type, selfTool *ast.Tool, aliases map[string]types.Type) []Finding {
	// Every expression position routes through here, so this is also where a
	// `match` anywhere in the expression tree is validated (exhaustiveness,
	// duplicate/unreachable arms).
	out := checkMatchInExpr(file, prog, expr, declared, aliases)
	return append(out, checkExprCallShape(file, prog, pos, expr, declared, selfTool, aliases)...)
}

func checkExprCallShape(file string, prog *ast.Program, pos lexer.Position, expr *ast.Expr, declared map[string]types.Type, selfTool *ast.Tool, aliases map[string]types.Type) []Finding {
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
				if err := checkToolCall(selfTool, method, memCall, declared, aliases); err != nil {
					return []Finding{{File: file, Line: pos.Line, Column: pos.Column,
						Message: fmt.Sprintf("self.%s: %s", method, err)}}
				}
			default:
				if mem, found := findMemory(prog, target); found {
					if err := checkMemoryOp(mem, method, memCall, declared); err != nil {
						return []Finding{{File: file, Line: pos.Line, Column: pos.Column,
							Message: fmt.Sprintf("%s.%s: %s", target, method, err)}}
					}
					break
				}
				if tool, found := findTool(prog, target); found {
					if err := checkToolCall(tool, method, memCall, declared, aliases); err != nil {
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

	engine, _ := ast.AgentEngine(agent)
	switch {
	case engine == "" || strings.HasPrefix(engine, "cli/"):
		if _, _, err := ast.AgentCommand(agent); err != nil {
			return []Finding{{File: file, Line: agent.Pos.Line, Column: agent.Pos.Column, Message: err.Error()}}
		}
	case strings.HasPrefix(engine, "ollama/"):
		if _, _, _, err := ast.AgentOllamaConfig(agent, engine); err != nil {
			return []Finding{{File: file, Line: agent.Pos.Line, Column: agent.Pos.Column, Message: err.Error()}}
		}
	default:
		return []Finding{{File: file, Line: agent.Pos.Line, Column: agent.Pos.Column,
			Message: fmt.Sprintf("agent %q: engine %q is not supported yet", agentName, engine)}}
	}
	if _, _, _, err := ast.AgentRetryConfig(agent); err != nil {
		return []Finding{{File: file, Line: agent.Pos.Line, Column: agent.Pos.Column, Message: err.Error()}}
	}
	if _, _, _, err := ast.AgentCacheConfig(agent); err != nil {
		return []Finding{{File: file, Line: agent.Pos.Line, Column: agent.Pos.Column, Message: err.Error()}}
	}
	if _, _, _, _, err := ast.AgentLimiterConfig(agent); err != nil {
		return []Finding{{File: file, Line: agent.Pos.Line, Column: agent.Pos.Column, Message: err.Error()}}
	}
	if refs, err := ast.AgentFallbackRefs(agent); err != nil {
		return []Finding{{File: file, Line: agent.Pos.Line, Column: agent.Pos.Column, Message: err.Error()}}
	} else {
		for _, ref := range refs {
			if ref.Inline != nil {
				continue
			}
			if _, ok := findAgent(prog, ref.Name); !ok {
				return []Finding{{File: file, Line: agent.Pos.Line, Column: agent.Pos.Column,
					Message: fmt.Sprintf("agent %q fallback: agent %q is not declared", agent.Name, ref.Name)}}
			}
		}
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
// (this pattern used to be duplicated across internal/engine/interpreter and internal/engine/runtime
// — both now share internal/lang/ast's literal readers)

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
// cmd/git/fs/http/json/log/time method-call targets (language-design.md §7) that are
// never looked up against user declarations.
var nativeNamespaces = map[string]bool{"cmd": true, "git": true, "fs": true, "http": true, "json": true, "log": true, "time": true}

// checkToolCall mirrors internal/engine/interpreter.evalToolCall's validation: the method
// must exist on tool, and the call's argument count must match the
// method's declared Params exactly (tool method calls bind positionally —
// language-design.md §8's `execution.write_file(fix.file_path,
// fix.content)`, unlike `prompt Name(param: "x")`'s named convention).
// Purely structural — lint never executes a tool method body, so it can't
// validate what's inside it (e.g. a bad native-op argument) beyond this.
// known is the caller's inferred variable-type map (see varinfer.go): a
// non-literal argument that's a bare identifier with a known, non-Any type
// gets checked too, not just a literal — anything else (arithmetic,
// member/index access, an unresolved name) is still left unchecked, same
// "can't prove it, don't fail" stance checkMemoryOp already takes.
func checkToolCall(tool *ast.Tool, method string, call *ast.Call, known map[string]types.Type, aliases map[string]types.Type) error {
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
	required := ast.RequiredParamCount(m.Params)
	if len(call.Args) < required || len(call.Args) > len(m.Params) {
		return fmt.Errorf("tool %q: %s %s, got %d", tool.Name, method, paramArityText(required, len(m.Params)), len(call.Args))
	}
	for i, p := range m.Params {
		if i >= len(call.Args) {
			continue // omitted arg filled from the param's default at run time
		}
		if p.Type == nil {
			continue // untyped param: dynamic, exactly as today
		}
		paramType, ok := types.FromExprAlias(p.Type, aliases)
		if !ok {
			return fmt.Errorf("tool %q: %s: parameter %q has an unrecognized type %q", tool.Name, method, p.Name, p.Type)
		}
		if lv, err := literalValue(call.Args[i].Value); err == nil {
			if err := types.Check(fmt.Sprintf("tool %q: %s: parameter %q", tool.Name, method, p.Name), paramType, lv); err != nil {
				return err
			}
			continue
		}
		name, ok := ast.IdentValue(call.Args[i].Value)
		if !ok {
			continue
		}
		argType, ok := known[name]
		if !ok || argType.Equal(types.Any) {
			continue
		}
		if err := types.CheckType(fmt.Sprintf("tool %q: %s: parameter %q", tool.Name, method, p.Name), paramType, argType); err != nil {
			return err
		}
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
// fire when the argument in question is recognizably a literal, or — via
// known (see varinfer.go) — a bare-identifier variable whose type inference
// already pinned down. The runtime also accepts nested memory/agent calls
// as memory-op arguments (internal/engine/interpreter/eval.go's
// evalPositionalValues), whose type lint still can't know without
// executing. Any argument this can't prove is simply left unchecked here;
// the runtime still enforces its own type rules when it actually runs.
// Purely static either way — no store write, no file write, lint never has
// side effects.
func checkMemoryOp(mem *ast.Memory, method string, call *ast.Call, known map[string]types.Type) error {
	memType, _ := memoryProp(mem, "type")
	n := len(call.Args)

	checkStringArg := func(i int, label string) error {
		if i >= len(call.Args) {
			return nil
		}
		if v, err := literalValue(call.Args[i].Value); err == nil {
			if _, ok := v.(string); !ok {
				return fmt.Errorf("memory %q: %s must be a string", mem.Name, label)
			}
			return nil
		}
		name, ok := ast.IdentValue(call.Args[i].Value)
		if !ok {
			return nil // not a literal or a bare identifier — can't check statically, not an error
		}
		if t, ok := known[name]; ok && !t.Equal(types.Any) && !t.Equal(types.String) {
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
