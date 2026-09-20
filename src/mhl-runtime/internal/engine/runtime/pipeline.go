package runtime

import (
	"fmt"
	"strconv"
	"time"

	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
	"github.com/mh-language/mhl-core-runtime/internal/lang/types"
)

// CheckpointConfig is the resolved `checkpoint: { ... }` block of a pipeline.
//
// The block is optional: a pipeline/workflow that declares none (or an empty
// `checkpoint: {}`) is projected with DefaultCheckpointConfig — per-step
// checkpointing on — so `mhl run --resume` / `run/resume` can always continue
// an interrupted run. Declare `checkpoint: { enabled: false }` to opt out, or
// `checkpoint: { ttl: ... }` to tune retention while keeping the default
// strategy. This default is applied in PipelineFromAST only; a Pipeline value
// built directly (tests, literal construction) still starts from the zero
// CheckpointConfig{} — Enabled false — unchanged.
type CheckpointConfig struct {
	Enabled  bool
	Strategy string        // e.g. "per_step"
	Storage  string        // e.g. "file"
	TTL      time.Duration // e.g. 7d
}

// DefaultCheckpointConfig is the checkpoint configuration a pipeline/workflow
// receives from PipelineFromAST when it declares no `checkpoint: { ... }` block,
// or an empty one: per-step checkpointing enabled, so an interrupted run is
// always resumable without the author having to opt in.
func DefaultCheckpointConfig() CheckpointConfig {
	return CheckpointConfig{Enabled: true, Strategy: "per_step"}
}

// ContextConfig is the resolved `context: { ... }` block of a pipeline: it
// opts the pipeline into the read-only `context.*` accessor its steps can
// then read (context.session_id, context.started_at, context.resumed,
// context.vars). A nil *ContextConfig on Pipeline means the block was not
// declared at all and `context` stays an undefined identifier.
//
// Source selects which prior state context.vars is hydrated from:
// "latest" (default) follows the .latest pointer to the most recent session
// of this pipeline; "session:<id>" pins an explicit one. Require makes the
// run fail when Source resolves to no stored state at all, rather than
// exposing an empty context.vars.
type ContextConfig struct {
	Source  string
	Require bool
}

// SpawnConfig is the resolved `spawn { max_concurrency: N }` block of a
// pipeline: the ceiling on how many `spawn`ed agent calls run their
// subprocess/HTTP request at once, across all of the pipeline's steps.
// MaxConcurrency <= 0 means "use the interpreter default".
type SpawnConfig struct {
	MaxConcurrency int
}

// Stage is one unit the Runner advances through: either a single plain step
// (Parallel false, Steps has exactly one entry, Name == that step) or a
// `parallel` group (Parallel true, Steps holds the group's branch step
// names in declared order, Name is the group label). The Runner walks
// Stages; Steps (below, on Pipeline) stays as the flattened name list the
// legacy step-name helpers and resume-skip reporting still use.
type Stage struct {
	Name     string
	Steps    []string
	Parallel bool
}

// Pipeline is the runtime-facing view of an ast.Pipeline: its name, ordered
// stages (and the flattened step-name list), checkpoint configuration, and —
// when Loop is set — the repeat policy LoopRunner enforces (StopWhen
// re-evaluated after every iteration, MaxIterations as a hard ceiling;
// either or both may be zero-valued, same as the old standalone `loop`
// declaration allowed).
type Pipeline struct {
	Name string
	// Kind is "pipeline" or "workflow" (ast.Pipeline.Kind, projected
	// verbatim) — purely informational here (the runtime executes both
	// identically), surfaced to a session_start/session_end hook's payload
	// as session.kind so a hook shared across many declarations can tell
	// them apart.
	Kind string
	// Description is the optional `description: "..."` body property — a
	// human-readable summary surfaced as the MCP tool / A2A skill description
	// by the serve adapters. Empty when the pipeline declares none.
	Description string
	Steps       []string
	Stages      []Stage
	// StepTimeouts maps a step name — or a `parallel` group name — to the
	// duration from its optional `timeout <dur>` header clause. Nil when no
	// clause is declared anywhere (a missing key reads back as 0, i.e. no
	// cap). Runner.Run wraps that step's / group's context with
	// context.WithTimeout; an expiry fails it like an explicit fail().
	StepTimeouts map[string]time.Duration
	Checkpoint   CheckpointConfig
	Spawn        SpawnConfig
	Loop         bool
	StopWhen     *ast.Expr
	// MaxIterations is the loop's hard iteration ceiling, from either the
	// `max <N>` header clause or a `repeat { max_iterations: N }` block (the
	// block wins if both are written — a lint finding). Zero means no ceiling.
	MaxIterations int
	// InstanceID is not derived from the AST at all — PipelineFromAST never
	// sets it. It's a runtime-only field LoopRunner.Run fills in (resolved
	// or freshly generated from its own LoopCheckpoint, see loop.go) right
	// before each iteration's Runner.Run call, so Run can thread it into
	// RunContext.InstanceID for cli.go's `mem` support. Left empty for a
	// plain (non-`loop`) pipeline — Run treats that as instance "default".
	InstanceID string
	// Context is the resolved `context:` block, or nil when the pipeline
	// declares none — see ContextConfig.
	Context *ContextConfig
	// Output is the optional `output: { name: expr, ... }` mapping expression,
	// or nil when the pipeline declares none. When set, it is the sole
	// projection a run exposes to a caller (execsvc evaluates it via
	// interpreter.EvalOutputs against the final variable state); when nil, the
	// legacy behaviour applies and every non-internal `var` is returned. The
	// runner never touches this — it is evaluated one level up, in execsvc.
	Output *ast.Expr
	// Inputs lists this pipeline's declared `input name: Type` members, in
	// declaration order. A malformed/unrecognized Type text resolves to
	// types.Any here (best-effort, same as every other reader in this
	// function) — internal/lang/lint is what reports the typo as a Finding.
	Inputs []PipelineInputSpec

	// SessionStart, SessionEnd, StepStart, StepEnd, and StopFailure are the
	// optional `name: (x) -> { ... }` lifecycle hook properties — each a
	// single-parameter lambda, unevaluated here (mirrors Output above: the raw
	// *ast.Expr is stored, evaluation happens at the point execsvc.Run
	// actually fires it, via interpreter.RunPipelineHook). nil when the
	// pipeline declares none. See PipelineBodyProperties for their semantics.
	SessionStart *ast.Expr
	SessionEnd   *ast.Expr
	StepStart    *ast.Expr
	StepEnd      *ast.Expr
	StopFailure  *ast.Expr
}

// PipelineInputSpec is one `input name: Type` declaration, resolved to the
// shared types.Type vocabulary. Default is nil for a required input (no
// `= expr` written); when non-nil, the input is optional — see
// ValidateInputs/InputSchema and interpreter.EvalPipelineVars. EnumVariants
// is set only when Type.Kind == types.EnumKind and the named `enum` is
// declared in the same program — the declared variant list, in declaration
// order, so InputSchema can advertise a real JSON Schema `"enum": [...]`
// instead of the type-only `{"type": "string"}` Type.JSONSchema() falls
// back to on its own (it has no access to the enum declaration, only its
// name — see its own doc comment).
type PipelineInputSpec struct {
	Name         string
	Type         types.Type
	Default      *ast.Expr
	EnumVariants []string
}

// PipelineFromAST projects an ast.Pipeline onto a runtime Pipeline, extracting
// ordered step names, the checkpoint configuration, and — for a `loop
// pipeline` — its `repeat { stop_when, max_iterations }` block. Named
// `repeat`, not `loop`, specifically to avoid reading as `loop pipeline X {
// loop: {...} }` — the leading `loop` keyword already says this pipeline
// repeats; the block itself only needs to say how.
// PipelineFromAST projects a parsed pipeline onto the runtime Pipeline
// value. aliases resolves `type X = ...` declarations for input-type
// annotations (built by the caller from the whole program via
// types.Aliases); a nil map just means aliased input types fall back to
// types.Any, exactly as an unrecognized keyword already does here. prog is
// the whole program p was found in — its only use is looking up an enum
// input's declared variant list (enumVariants) for PipelineInputSpec /
// InputSchema; a nil prog just means an enum-typed input carries no
// EnumVariants, the same as before this existed.
//
// Checkpoint starts at DefaultCheckpointConfig (per-step, enabled) and is
// only replaced when a `checkpoint: { ... }` block is present, so omitting the
// block leaves a pipeline resumable rather than un-resumable.
//
// The `m.Prop.Name` cases below must stay a subset of
// ast.PipelineBodyProperties (the shared allow-list lint and the LSP also
// read); adding a body property means adding both its entry there and its
// case here.
func PipelineFromAST(p *ast.Pipeline, aliases map[string]types.Type, prog *ast.Program) Pipeline {
	out := Pipeline{Name: p.Name, Kind: p.Kind, Loop: p.Loop, Checkpoint: DefaultCheckpointConfig()}
	entryStage := -1
	// `max <N>` header clause — shorthand for `repeat { max_iterations: N }`.
	// Read first so an explicit `repeat` block below still wins (both being
	// set is a lint finding); a non-positive / non-integer value is ignored
	// here, the same best-effort handling the `timeout` clause gets, with
	// lint.checkLoopMax reporting it.
	if n, err := strconv.Atoi(p.Max); err == nil && n > 0 {
		out.MaxIterations = n
	}
	for _, m := range p.Body {
		switch {
		case m.Step != nil:
			out.Steps = append(out.Steps, m.Step.Name)
			out.Stages = append(out.Stages, Stage{Name: m.Step.Name, Steps: []string{m.Step.Name}})
			out.setStepTimeout(m.Step.Name, m.Step.Timeout)
			if m.Step.Entry {
				entryStage = len(out.Stages) - 1
			}
		case m.Parallel != nil:
			names := make([]string, 0, len(m.Parallel.Steps))
			for _, s := range m.Parallel.Steps {
				names = append(names, s.Name)
				out.setStepTimeout(s.Name, s.Timeout)
			}
			out.Steps = append(out.Steps, names...)
			out.Stages = append(out.Stages, Stage{Name: m.Parallel.Name, Steps: names, Parallel: true})
			out.setStepTimeout(m.Parallel.Name, m.Parallel.Timeout)
		case m.Prop != nil && m.Prop.Name == "checkpoint":
			out.Checkpoint = checkpointFromExpr(m.Prop.Value)
		case m.Prop != nil && m.Prop.Name == "spawn":
			out.Spawn = spawnConfigFromExpr(m.Prop.Value)
		case m.Prop != nil && m.Prop.Name == "repeat":
			var mi int
			out.StopWhen, mi = repeatConfigFromExpr(m.Prop.Value)
			// Only override the `max <N>` header clause when the block
			// actually carries a max_iterations — otherwise a `repeat` block
			// with just a stop_when would silently reset the ceiling to 0.
			if mi > 0 {
				out.MaxIterations = mi
			}
		case m.Prop != nil && m.Prop.Name == "context":
			out.Context = contextConfigFromExpr(m.Prop.Value)
		case m.Prop != nil && m.Prop.Name == "output":
			out.Output = m.Prop.Value
		case m.Prop != nil && m.Prop.Name == "description":
			out.Description, _ = ast.StringValue(m.Prop.Value)
		case m.Prop != nil && m.Prop.Name == "session_start":
			out.SessionStart = m.Prop.Value
		case m.Prop != nil && m.Prop.Name == "session_end":
			out.SessionEnd = m.Prop.Value
		case m.Prop != nil && m.Prop.Name == "step_start":
			out.StepStart = m.Prop.Value
		case m.Prop != nil && m.Prop.Name == "step_end":
			out.StepEnd = m.Prop.Value
		case m.Prop != nil && m.Prop.Name == "stop_failure":
			out.StopFailure = m.Prop.Value
		case m.Input != nil:
			t, ok := types.FromExprAlias(m.Input.Type, aliases)
			if !ok {
				t = types.Any
			}
			spec := PipelineInputSpec{Name: m.Input.Name, Type: t, Default: m.Input.Default}
			if t.Kind == types.EnumKind {
				spec.EnumVariants = enumVariants(prog, t.Name)
			}
			out.Inputs = append(out.Inputs, spec)
		}
	}
	// A `partial` pipeline/workflow's merged step list is in whatever order
	// its fragments happened to be pulled in, not execution order — an
	// `entry step` (ast.Step.Entry, lint-enforced to be exactly one, and
	// only inside a `partial` declaration) names the real starting point.
	// Move its Stage to the front — Runner.Run/execStage still simply start
	// at Stages[0] — and rebuild the flattened Steps list to match, rather
	// than keep two separately-reordered slices in sync by hand.
	if entryStage > 0 {
		reordered := make([]Stage, 0, len(out.Stages))
		reordered = append(reordered, out.Stages[entryStage])
		reordered = append(reordered, out.Stages[:entryStage]...)
		reordered = append(reordered, out.Stages[entryStage+1:]...)
		out.Stages = reordered

		out.Steps = out.Steps[:0]
		for _, stage := range out.Stages {
			out.Steps = append(out.Steps, stage.Steps...)
		}
	}
	return out
}

// setStepTimeout records the parsed duration of a `timeout <dur>` header
// clause under the given step or parallel-group name. A blank or
// unparseable string is ignored (internal/lang/lint reports the typo); the
// map is created lazily so a pipeline with no clause keeps a nil map.
func (p *Pipeline) setStepTimeout(name, raw string) {
	if raw == "" {
		return
	}
	d, ok := ast.ParseDuration(raw)
	if !ok || d <= 0 {
		return
	}
	if p.StepTimeouts == nil {
		p.StepTimeouts = make(map[string]time.Duration)
	}
	p.StepTimeouts[name] = d
}

// firstStage returns the pipeline's first stage, or ok=false for a pipeline
// with no steps at all.
func (p Pipeline) firstStage() (Stage, bool) {
	if len(p.Stages) == 0 {
		return Stage{}, false
	}
	return p.Stages[0], true
}

// stageAfter returns the stage declared immediately after the stage named
// name, or ok=false when it is the last one (or not found). This is the
// *default* transition Run falls back to when a stage completes normally
// without a `goto` redirecting it — see Checkpoint.NextStep's doc comment
// for why a resume never recomputes this after the fact.
func (p Pipeline) stageAfter(name string) (Stage, bool) {
	for i, s := range p.Stages {
		if s.Name == name && i+1 < len(p.Stages) {
			return p.Stages[i+1], true
		}
	}
	return Stage{}, false
}

// stageByName returns the stage whose Name is name. A plain step's stage is
// named after the step itself, so this resolves both a `goto <step>` target
// and a checkpoint's NextStep (which, since `parallel` exists, may name a
// group rather than a step).
func (p Pipeline) stageByName(name string) (Stage, bool) {
	for _, s := range p.Stages {
		if s.Name == name {
			return s, true
		}
	}
	return Stage{}, false
}

// stageForStep returns the stage that contains the step named name — the
// same as stageByName for a plain step, but for a step inside a `parallel`
// group it returns the group's stage. Used to reject a `goto` aimed inside a
// group (its target resolves to a stage whose Name != the target).
func (p Pipeline) stageForStep(name string) (Stage, bool) {
	for _, s := range p.Stages {
		for _, st := range s.Steps {
			if st == name {
				return s, true
			}
		}
	}
	return Stage{}, false
}

// stepInParallelGroup reports whether name is a branch step of some
// `parallel` group (as opposed to a plain top-level step) — a `goto` may not
// target such a step.
func (p Pipeline) stepInParallelGroup(name string) bool {
	s, ok := p.stageForStep(name)
	return ok && s.Parallel
}

// hasStep reports whether name is one of the pipeline's declared steps —
// used to fail a `goto` closed at the step it targets rather than let the
// Runner wander onto a name that was never declared.
func (p Pipeline) hasStep(name string) bool {
	for _, s := range p.Steps {
		if s == name {
			return true
		}
	}
	return false
}

// enumVariants looks up name's declared `enum` variants in prog, in
// declaration order — runtime's own small copy of the same lookup
// lint.findEnumDecl and the interpreter each keep independently (there is
// no shared source of truth for it; see lint/match.go's doc comment on its
// own copy). nil prog or no matching enum both just return nil.
func enumVariants(prog *ast.Program, name string) []string {
	if prog == nil {
		return nil
	}
	for _, decl := range prog.Decls {
		if decl.Enum != nil && decl.Enum.Name == name {
			return decl.Enum.Variants
		}
	}
	return nil
}

// FindPipeline returns the named pipeline from a program, or the first one when
// name is empty.
//
// This is where a `partial` pipeline/workflow's entry-step count is finally
// validated — deliberately not earlier, at import-resolution time (see
// interpreter.mergePartialGroup's doc comment): by the time something calls
// FindPipeline, it's actually about to run, describe, or otherwise use the
// declaration for real, so "zero entries" and "more than one" both stop
// being ambiguous with "this fragment's siblings just aren't visible from
// here" and become the real errors they are.
func FindPipeline(prog *ast.Program, name string) (Pipeline, error) {
	if prog == nil {
		return Pipeline{}, fmt.Errorf("runtime: nil program")
	}
	aliases, _ := types.Aliases(prog)
	for _, d := range prog.Decls {
		if d.Pipeline == nil {
			continue
		}
		if name == "" || d.Pipeline.Name == name {
			if d.Pipeline.Partial {
				if n := d.Pipeline.EntryStepCount(); n != 1 {
					return Pipeline{}, fmt.Errorf("partial %s %q: expected exactly one `entry step` across its fragments, found %d", d.Pipeline.Kind, d.Pipeline.Name, n)
				}
			}
			return PipelineFromAST(d.Pipeline, aliases, prog), nil
		}
	}
	if name == "" {
		return Pipeline{}, fmt.Errorf("runtime: no pipeline declared in program")
	}
	return Pipeline{}, fmt.Errorf("runtime: pipeline %q not found", name)
}

// repeatConfigFromExpr reads a `repeat { stop_when, max_iterations }`
// property's object literal — grouping a `loop pipeline`'s two repeat-policy
// fields under one marker, the same way `checkpoint: { ... }` already groups
// its own, instead of leaving them loose directly in the pipeline body.
// Either field may be absent (stopWhen stays nil, maxIterations stays 0),
// matching how a loop with no explicit ceiling or condition already behaved
// before this grouping existed — see Pipeline.StopWhen/MaxIterations's doc
// comment.
func repeatConfigFromExpr(e *ast.Expr) (stopWhen *ast.Expr, maxIterations int) {
	obj := ast.BareObject(e)
	if obj == nil {
		return nil, 0
	}
	for _, f := range obj.Fields {
		key := ""
		switch {
		case f.KeyIdent != nil:
			key = *f.KeyIdent
		case f.KeyStr != nil:
			key = *f.KeyStr
		}
		switch key {
		case "stop_when":
			stopWhen = f.Value
		case "max_iterations":
			if n, ok := ast.NumberValue(f.Value); ok {
				maxIterations = int(n)
			}
		}
	}
	return stopWhen, maxIterations
}

// checkpointFromExpr reads a `checkpoint: { ... }` property's object literal
// via the shared ast literal readers (internal/lang/ast/literal.go) — the
// same readers internal/engine/interpreter uses for agent/memory config —
// rather than keeping its own copy of "what counts as a bare literal".
//
// It starts from DefaultCheckpointConfig, so an empty `checkpoint: {}` matches
// the no-block default and a partial block (e.g. just `ttl:`) keeps per-step
// checkpointing on; only `enabled: false` turns it off.
// spawnConfigFromExpr reads a `spawn { max_concurrency: N }` block. An
// absent, non-numeric, or non-positive value leaves MaxConcurrency at 0,
// which the interpreter reads as "use the default".
func spawnConfigFromExpr(e *ast.Expr) SpawnConfig {
	cfg := SpawnConfig{}
	obj := ast.BareObject(e)
	if obj == nil {
		return cfg
	}
	for _, f := range obj.Fields {
		key := ""
		switch {
		case f.KeyIdent != nil:
			key = *f.KeyIdent
		case f.KeyStr != nil:
			key = *f.KeyStr
		}
		if key == "max_concurrency" {
			if n, ok := ast.NumberValue(f.Value); ok && n > 0 {
				cfg.MaxConcurrency = int(n)
			}
		}
	}
	return cfg
}

// contextConfigFromExpr reads a `context: { source, require }` property's
// object literal via the shared ast literal readers, the same way
// checkpointFromExpr/spawnConfigFromExpr do. A bare `context: {}` (or an
// unreadable value) still returns a non-nil config with Source defaulting to
// "latest" — the block's mere presence is what opts the pipeline into the
// `context.*` accessor.
func contextConfigFromExpr(e *ast.Expr) *ContextConfig {
	cfg := &ContextConfig{Source: "latest"}
	obj := ast.BareObject(e)
	if obj == nil {
		return cfg
	}
	for _, f := range obj.Fields {
		key := ""
		switch {
		case f.KeyIdent != nil:
			key = *f.KeyIdent
		case f.KeyStr != nil:
			key = *f.KeyStr
		}
		switch key {
		case "source":
			if s, ok := ast.StringValue(f.Value); ok && s != "" {
				cfg.Source = s
			}
		case "require":
			if b, ok := ast.BoolValue(f.Value); ok {
				cfg.Require = b
			}
		}
	}
	return cfg
}

func checkpointFromExpr(e *ast.Expr) CheckpointConfig {
	cfg := DefaultCheckpointConfig()
	obj := ast.BareObject(e)
	if obj == nil {
		return cfg
	}
	for _, f := range obj.Fields {
		key := ""
		switch {
		case f.KeyIdent != nil:
			key = *f.KeyIdent
		case f.KeyStr != nil:
			key = *f.KeyStr
		}
		switch key {
		case "enabled":
			if b, ok := ast.BoolValue(f.Value); ok {
				cfg.Enabled = b
			}
		case "strategy":
			if s, ok := ast.StringValue(f.Value); ok {
				cfg.Strategy = s
			}
		case "storage":
			if s, ok := ast.StringValue(f.Value); ok {
				cfg.Storage = s
			}
		case "ttl":
			if d, ok := ast.DurationValue(f.Value); ok {
				cfg.TTL = d
			}
		}
	}
	return cfg
}
