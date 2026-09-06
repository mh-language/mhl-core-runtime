package execsvc

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/mh-language/mhl-core-runtime/internal/engine/interpreter"
	"github.com/mh-language/mhl-core-runtime/internal/engine/runtime"
	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
	"github.com/mh-language/mhl-core-runtime/internal/lang/parser"
)

// Inspection is the static plan of a pipeline/workflow — everything Run would
// do up to executing a step: which steps run, in what order, which are
// concurrent, the input contract, the checkpoint policy, and whether a
// result projection is declared. It executes nothing: no session directory,
// no agent calls, no memory or state writes.
type Inspection struct {
	Pipeline            string             `json:"pipeline"`
	File                string             `json:"file,omitempty"`
	Kind                string             `json:"kind"` // "pipeline" | "workflow"
	Loop                bool               `json:"loop,omitempty"`
	MaxIterations       int                `json:"max_iterations,omitempty"`
	Steps               []string           `json:"steps"`
	Stages              []InspectStage     `json:"stages"`
	Inputs              []InspectInput     `json:"inputs,omitempty"`
	MissingInputs       []string           `json:"missing_inputs,omitempty"`
	UnknownInputs       []string           `json:"unknown_inputs,omitempty"`
	Checkpoint          InspectCheckpoint  `json:"checkpoint"`
	StepTimeouts        map[string]string  `json:"step_timeouts,omitempty"`
	HasOutputProjection bool               `json:"has_output_projection"`
	// Goto lists the `goto <target>` edges declared in the pipeline's step
	// bodies (a workflow only). Whether each target resolves is a lint
	// concern; this is the static control-flow graph.
	Goto []InspectGoto `json:"goto,omitempty"`
}

// InspectStage is one execution unit — a lone step, or a `parallel` group.
type InspectStage struct {
	Name     string   `json:"name"`
	Steps    []string `json:"steps"`
	Parallel bool     `json:"parallel,omitempty"`
}

// InspectGoto is one `goto` edge: From is the step it appears in, To its
// target step name.
type InspectGoto struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// InspectInput is one declared `input name: Type`, with whether the caller
// supplied a value.
type InspectInput struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Supplied bool   `json:"supplied"`
}

// InspectCheckpoint is the resolved checkpoint policy.
type InspectCheckpoint struct {
	Enabled  bool   `json:"enabled"`
	Strategy string `json:"strategy,omitempty"`
	Storage  string `json:"storage,omitempty"`
	TTL      string `json:"ttl,omitempty"`
}

// Inspect parses the program in req (Program or Source), resolves imports,
// selects the pipeline, and returns its static plan. req.Inputs is checked
// against the input contract but not coerced; req.Resume/Session/BaseDir and
// every execution-only field are ignored.
func Inspect(req Request) (*Inspection, error) {
	prog := req.Program
	file := req.File
	if prog == nil {
		if req.Source == "" {
			return nil, fmt.Errorf("execsvc: neither Program nor Source given")
		}
		if file == "" {
			file = req.Source
		}
		src, err := os.ReadFile(req.Source)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", req.Source, err)
		}
		prog, err = parser.Parse(string(src))
		if err != nil {
			return nil, fmt.Errorf("parsing %s: %w", req.Source, err)
		}
		if err := interpreter.ResolveImports(file, prog); err != nil {
			return nil, err
		}
	}

	pipeline, err := runtime.FindPipeline(prog, req.Workflow)
	if err != nil {
		return nil, err
	}

	ins := &Inspection{
		Pipeline:            pipeline.Name,
		File:                file,
		Kind:                pipelineKindLabel(prog, pipeline.Name),
		Loop:                pipeline.Loop,
		MaxIterations:       pipeline.MaxIterations,
		Steps:               pipeline.Steps,
		HasOutputProjection: pipeline.Output != nil,
		Checkpoint: InspectCheckpoint{
			Enabled:  pipeline.Checkpoint.Enabled,
			Strategy: pipeline.Checkpoint.Strategy,
			Storage:  pipeline.Checkpoint.Storage,
		},
	}
	if pipeline.Checkpoint.TTL > 0 {
		ins.Checkpoint.TTL = humanDuration(pipeline.Checkpoint.TTL)
	}
	ins.Goto = collectPipelineGotos(prog, pipeline.Name)
	for _, s := range pipeline.Stages {
		ins.Stages = append(ins.Stages, InspectStage{Name: s.Name, Steps: s.Steps, Parallel: s.Parallel})
	}
	for name, d := range pipeline.StepTimeouts {
		if ins.StepTimeouts == nil {
			ins.StepTimeouts = map[string]string{}
		}
		ins.StepTimeouts[name] = d.String()
	}
	for _, in := range pipeline.Inputs {
		_, supplied := req.Inputs[in.Name]
		ins.Inputs = append(ins.Inputs, InspectInput{Name: in.Name, Type: in.Type.String(), Supplied: supplied})
	}

	if err := pipeline.ValidateInputs(req.Inputs); err != nil {
		var bad *runtime.InvalidInputsError
		if errors.As(err, &bad) {
			ins.MissingInputs = bad.Missing
			ins.UnknownInputs = bad.Unknown
		} else {
			return nil, err
		}
	}
	return ins, nil
}

// pipelineKindLabel reports "workflow" or "pipeline" for the named
// declaration; defaults to "pipeline" if not found (FindPipeline already
// vouched for it).
func pipelineKindLabel(prog *ast.Program, name string) string {
	for _, d := range prog.Decls {
		if d.Pipeline != nil && d.Pipeline.Name == name {
			if d.Pipeline.IsWorkflow() {
				return "workflow"
			}
			return "pipeline"
		}
	}
	return "pipeline"
}

// humanDuration renders a duration the way a `.mh` author would write it —
// whole days as "7d", whole hours as "12h" — falling back to the Go form.
func humanDuration(d time.Duration) string {
	switch {
	case d%(24*time.Hour) == 0:
		return fmt.Sprintf("%dd", d/(24*time.Hour))
	case d%time.Hour == 0:
		return fmt.Sprintf("%dh", d/time.Hour)
	case d%time.Minute == 0:
		return fmt.Sprintf("%dm", d/time.Minute)
	default:
		return d.String()
	}
}

// collectPipelineGotos walks the named pipeline's step bodies (recursing into
// if/while/for/try) and returns every `goto` edge, in source order.
func collectPipelineGotos(prog *ast.Program, name string) []InspectGoto {
	var out []InspectGoto
	for _, d := range prog.Decls {
		if d.Pipeline == nil || d.Pipeline.Name != name {
			continue
		}
		for _, m := range d.Pipeline.Body {
			if m.Step == nil {
				continue
			}
			var targets []string
			gotoTargets(m.Step.Body, &targets)
			for _, t := range targets {
				out = append(out, InspectGoto{From: m.Step.Name, To: t})
			}
		}
	}
	return out
}

func gotoTargets(stmts []*ast.Statement, out *[]string) {
	for _, s := range stmts {
		if s == nil {
			continue
		}
		switch {
		case s.Goto != nil:
			*out = append(*out, s.Goto.Target)
		case s.If != nil:
			gotoTargets(s.If.Then, out)
			gotoTargets(s.If.Else, out)
		case s.While != nil:
			gotoTargets(s.While.Body, out)
		case s.ForIn != nil:
			gotoTargets(s.ForIn.Body, out)
		case s.Try != nil:
			gotoTargets(s.Try.Body, out)
			gotoTargets(s.Try.Catch, out)
			gotoTargets(s.Try.Finally, out)
		}
	}
}
