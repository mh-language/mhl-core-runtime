package runtime

import (
	"fmt"
	"time"

	"github.com/yanjustino/mhl-runtime/internal/lang/ast"
)

// CheckpointConfig is the resolved `checkpoint { ... }` block of a pipeline.
type CheckpointConfig struct {
	Enabled  bool
	Strategy string        // e.g. "per_step"
	Storage  string        // e.g. "file"
	TTL      time.Duration // e.g. 7d
}

// Pipeline is the runtime-facing view of an ast.Pipeline: its name, ordered
// step names, checkpoint configuration, and — when Loop is set — the repeat
// policy LoopRunner enforces (StopWhen re-evaluated after every iteration,
// MaxIterations as a hard ceiling; either or both may be zero-valued, same
// as the old standalone `loop` declaration allowed).
type Pipeline struct {
	Name          string
	Steps         []string
	Checkpoint    CheckpointConfig
	Loop          bool
	StopWhen      *ast.Expr
	MaxIterations int
}

// PipelineFromAST projects an ast.Pipeline onto a runtime Pipeline, extracting
// ordered step names, the checkpoint configuration, and — for a `loop
// pipeline` — its `repeat { stop_when, max_iterations }` block. Named
// `repeat`, not `loop`, specifically to avoid reading as `loop pipeline X {
// loop: {...} }` — the leading `loop` keyword already says this pipeline
// repeats; the block itself only needs to say how.
func PipelineFromAST(p *ast.Pipeline) Pipeline {
	out := Pipeline{Name: p.Name, Loop: p.Loop}
	for _, m := range p.Body {
		switch {
		case m.Step != nil:
			out.Steps = append(out.Steps, m.Step.Name)
		case m.Prop != nil && m.Prop.Name == "checkpoint":
			out.Checkpoint = checkpointFromExpr(m.Prop.Value)
		case m.Prop != nil && m.Prop.Name == "repeat":
			out.StopWhen, out.MaxIterations = repeatConfigFromExpr(m.Prop.Value)
		}
	}
	return out
}

// firstStep returns the pipeline's first declared step, or ok=false for an
// empty pipeline.
func (p Pipeline) firstStep() (string, bool) {
	if len(p.Steps) == 0 {
		return "", false
	}
	return p.Steps[0], true
}

// stepAfter returns the step declared immediately after name in Steps, or
// ok=false when name is the last one (or not found at all). This is the
// *default* transition Run falls back to when a step completes normally
// without a `goto` redirecting it — see Checkpoint.NextStep's doc comment
// for why a resume never recomputes this after the fact.
func (p Pipeline) stepAfter(name string) (string, bool) {
	for i, s := range p.Steps {
		if s == name && i+1 < len(p.Steps) {
			return p.Steps[i+1], true
		}
	}
	return "", false
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
	for _, d := range prog.Decls {
		if d.Pipeline == nil {
			continue
		}
		if name == "" || d.Pipeline.Name == name {
			return PipelineFromAST(d.Pipeline), nil
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
