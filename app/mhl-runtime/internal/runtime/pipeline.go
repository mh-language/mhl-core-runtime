package runtime

import (
	"fmt"
	"strconv"
	"time"

	"github.com/yanjustino/mhl-runtime/internal/ast"
)

// CheckpointConfig is the resolved `checkpoint { ... }` block of a pipeline.
type CheckpointConfig struct {
	Enabled  bool
	Strategy string        // e.g. "per_step"
	Storage  string        // e.g. "file"
	TTL      time.Duration // e.g. 7d
}

// Pipeline is the runtime-facing view of an ast.Pipeline: its name, ordered
// step names, and checkpoint configuration.
type Pipeline struct {
	Name       string
	Steps      []string
	Checkpoint CheckpointConfig
}

// PipelineFromAST projects an ast.Pipeline onto a runtime Pipeline, extracting
// ordered step names and the checkpoint configuration.
func PipelineFromAST(p *ast.Pipeline) Pipeline {
	out := Pipeline{Name: p.Name}
	for _, m := range p.Body {
		switch {
		case m.Step != nil:
			out.Steps = append(out.Steps, m.Step.Name)
		case m.Prop != nil && m.Prop.Name == "checkpoint":
			out.Checkpoint = checkpointFromExpr(m.Prop.Value)
		}
	}
	return out
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

func checkpointFromExpr(e *ast.Expr) CheckpointConfig {
	cfg := CheckpointConfig{}
	obj := bareObject(e)
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
			if b, ok := boolValue(f.Value); ok {
				cfg.Enabled = b
			}
		case "strategy":
			if s, ok := stringValue(f.Value); ok {
				cfg.Strategy = s
			}
		case "storage":
			if s, ok := stringValue(f.Value); ok {
				cfg.Storage = s
			}
		case "ttl":
			if d, ok := durationValue(f.Value); ok {
				cfg.TTL = d
			}
		}
	}
	return cfg
}

// --- small expression readers (bool/string/duration) -----------------------

func primaryOf(e *ast.Expr) *ast.Primary {
	pf := barePostfix(e)
	if pf == nil || len(pf.Ops) != 0 {
		return nil
	}
	return pf.Primary
}

func boolValue(e *ast.Expr) (bool, bool) {
	p := primaryOf(e)
	if p == nil || p.Bool == nil {
		return false, false
	}
	return *p.Bool == "true", true
}

func stringValue(e *ast.Expr) (string, bool) {
	p := primaryOf(e)
	if p == nil {
		return "", false
	}
	switch {
	case p.Str != nil:
		return *p.Str, true
	case p.MultiStr != nil:
		return *p.MultiStr, true
	}
	return "", false
}

func durationValue(e *ast.Expr) (time.Duration, bool) {
	p := primaryOf(e)
	if p == nil || p.Duration == "" {
		return 0, false
	}
	return parseDuration(p.Duration)
}

// parseDuration parses MHL duration literals (e.g. "2s", "45s", "24h", "7d").
// Go's time.ParseDuration lacks a day unit, so days are handled explicitly.
func parseDuration(s string) (time.Duration, bool) {
	if len(s) < 2 {
		return 0, false
	}
	unit := s[len(s)-1]
	numStr := s[:len(s)-1]
	n, err := strconv.ParseFloat(numStr, 64)
	if err != nil {
		return 0, false
	}
	switch unit {
	case 's':
		return time.Duration(n * float64(time.Second)), true
	case 'm':
		return time.Duration(n * float64(time.Minute)), true
	case 'h':
		return time.Duration(n * float64(time.Hour)), true
	case 'd':
		return time.Duration(n * float64(24*time.Hour)), true
	}
	return 0, false
}
