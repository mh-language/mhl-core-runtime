package runtime

import (
	"fmt"
	"time"

	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
	"github.com/mh-language/mhl-core-runtime/internal/lang/types"
)

// CheckpointConfig is the resolved `checkpoint { ... }` block of a pipeline.
type CheckpointConfig struct {
	Enabled  bool
	Strategy string        // e.g. "per_step"
	Storage  string        // e.g. "file"
	TTL      time.Duration // e.g. 7d
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
	// Description is the optional `description: "..."` body property — a
	// human-readable summary surfaced as the MCP tool / A2A skill description
	// by the serve adapters. Empty when the pipeline declares none.
	Description   string
	Steps         []string
	Stages        []Stage
	Checkpoint    CheckpointConfig
	Spawn         SpawnConfig
	Loop          bool
	StopWhen      *ast.Expr
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
	// Inputs lists this pipeline's declared `input name: Type` members, in
	// declaration order. A malformed/unrecognized Type text resolves to
	// types.Any here (best-effort, same as every other reader in this
	// function) — internal/lang/lint is what reports the typo as a Finding.
	Inputs []PipelineInputSpec
}

// PipelineInputSpec is one `input name: Type` declaration, resolved to the
// shared types.Type vocabulary.
type PipelineInputSpec struct {
	Name string
	Type types.Type
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
// types.Any, exactly as an unrecognized keyword already does here.
func PipelineFromAST(p *ast.Pipeline, aliases map[string]types.Type) Pipeline {
	out := Pipeline{Name: p.Name, Loop: p.Loop}
	for _, m := range p.Body {
		switch {
		case m.Step != nil:
			out.Steps = append(out.Steps, m.Step.Name)
			out.Stages = append(out.Stages, Stage{Name: m.Step.Name, Steps: []string{m.Step.Name}})
		case m.Parallel != nil:
			names := make([]string, 0, len(m.Parallel.Steps))
			for _, s := range m.Parallel.Steps {
				names = append(names, s.Name)
			}
			out.Steps = append(out.Steps, names...)
			out.Stages = append(out.Stages, Stage{Name: m.Parallel.Name, Steps: names, Parallel: true})
		case m.Prop != nil && m.Prop.Name == "checkpoint":
			out.Checkpoint = checkpointFromExpr(m.Prop.Value)
		case m.Prop != nil && m.Prop.Name == "spawn":
			out.Spawn = spawnConfigFromExpr(m.Prop.Value)
		case m.Prop != nil && m.Prop.Name == "repeat":
			out.StopWhen, out.MaxIterations = repeatConfigFromExpr(m.Prop.Value)
		case m.Prop != nil && m.Prop.Name == "context":
			out.Context = contextConfigFromExpr(m.Prop.Value)
		case m.Prop != nil && m.Prop.Name == "description":
			out.Description, _ = ast.StringValue(m.Prop.Value)
		case m.Input != nil:
			t, ok := types.FromExprAlias(m.Input.Type, aliases)
			if !ok {
				t = types.Any
			}
			out.Inputs = append(out.Inputs, PipelineInputSpec{Name: m.Input.Name, Type: t})
		}
	}
	return out
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

// FindPipeline returns the named pipeline from a program, or the first one when
// name is empty.
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
			return PipelineFromAST(d.Pipeline, aliases), nil
		}
	}
	if name == "" {
		return Pipeline{}, fmt.Errorf("runtime: no pipeline declared in program")
	}
	return Pipeline{}, fmt.Errorf("runtime: pipeline %q not found", name)
}

// repeatConfigFromExpr reads a `repeat { stop_when, max_iterations }`
// property's object literal — grouping a `loop pipeline`'s two repeat-policy
// fields under one marker, the same way `checkpoint { ... }` already groups
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

// checkpointFromExpr reads a `checkpoint { ... }` property's object literal
// via the shared ast literal readers (internal/lang/ast/literal.go) — the
// same readers internal/engine/interpreter uses for agent/memory config —
// rather than keeping its own copy of "what counts as a bare literal".
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
	cfg := CheckpointConfig{}
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
