package cli

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/mh-language/mhl-core-runtime/internal/execsvc"
	"github.com/mh-language/mhl-core-runtime/internal/lang/lint"
)

// dryRunScope is the fixed disclaimer every --dry-run output carries: it says
// what was and was not checked, so nobody reads the plan as a prediction of a
// model-dependent run.
const dryRunScope = "dry-run: validated parse, imports, lint and the input contract, and projected the static step plan. No step ran, no agent or tool was called, and any branching that depends on a step's output is not evaluated."

// dryRunPipeline validates file and prints its static plan without executing
// anything. It exits non-zero when lint finds a problem or the input contract
// is not satisfied.
func dryRunPipeline(out io.Writer, file string, inputs map[string]any, format string) error {
	findings := lint.File(file)

	ins, err := execsvc.Inspect(execsvc.Request{Source: file, Inputs: inputs})
	if err != nil {
		if format == "json" {
			enc := json.NewEncoder(out)
			enc.SetIndent("", "  ")
			_ = enc.Encode(map[string]any{"ok": false, "dry_run": true, "file": file, "error": err.Error(), "note": dryRunScope})
		}
		return err
	}

	warnings := make([]string, 0, len(findings))
	for _, f := range findings {
		if f.Line > 0 {
			warnings = append(warnings, fmt.Sprintf("%s:%d:%d %s", f.File, f.Line, f.Column, f.Message))
		} else {
			warnings = append(warnings, fmt.Sprintf("%s %s", f.File, f.Message))
		}
	}
	blocked := len(warnings) > 0 || len(ins.MissingInputs) > 0 || len(ins.UnknownInputs) > 0

	if format == "json" {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		_ = enc.Encode(struct {
			OK       bool                  `json:"ok"`
			DryRun   bool                  `json:"dry_run"`
			Warnings []string              `json:"warnings,omitempty"`
			Note     string                `json:"note"`
			*execsvc.Inspection
		}{OK: !blocked, DryRun: true, Warnings: warnings, Note: dryRunScope, Inspection: ins})
		if blocked {
			return fmt.Errorf("dry-run found %d problem(s)", len(warnings)+len(ins.MissingInputs)+len(ins.UnknownInputs))
		}
		return nil
	}

	printDryRunText(out, ins, warnings)
	if blocked {
		return fmt.Errorf("dry-run found %d problem(s)", len(warnings)+len(ins.MissingInputs)+len(ins.UnknownInputs))
	}
	return nil
}

func printDryRunText(out io.Writer, ins *execsvc.Inspection, warnings []string) {
	kind := ins.Kind
	if ins.Loop {
		kind = "loop " + kind
	}
	fmt.Fprintf(out, "%s %s  (%s)\n", kind, ins.Pipeline, ins.File)

	fmt.Fprintf(out, "  steps (%d):\n", len(ins.Steps))
	for i, st := range ins.Stages {
		if st.Parallel {
			fmt.Fprintf(out, "    %2d. parallel %s: %v\n", i+1, st.Name, st.Steps)
		} else {
			line := fmt.Sprintf("    %2d. %s", i+1, st.Name)
			if d, ok := ins.StepTimeouts[st.Name]; ok {
				line += "  (timeout " + d + ")"
			}
			fmt.Fprintln(out, line)
		}
	}

	if len(ins.Inputs) > 0 {
		fmt.Fprintln(out, "  inputs:")
		for _, in := range ins.Inputs {
			mark := " "
			if in.Supplied {
				mark = "x"
			}
			fmt.Fprintf(out, "    [%s] %s: %s\n", mark, in.Name, in.Type)
		}
	}
	if len(ins.MissingInputs) > 0 {
		fmt.Fprintf(out, "  MISSING inputs: %v\n", ins.MissingInputs)
	}
	if len(ins.UnknownInputs) > 0 {
		fmt.Fprintf(out, "  UNKNOWN inputs: %v\n", ins.UnknownInputs)
	}

	cp := ins.Checkpoint
	if cp.Enabled {
		line := "  checkpoint: enabled"
		if cp.Strategy != "" {
			line += " (" + cp.Strategy
			if cp.TTL != "" {
				line += ", ttl " + cp.TTL
			}
			line += ")"
		}
		fmt.Fprintln(out, line)
	} else {
		fmt.Fprintln(out, "  checkpoint: disabled")
	}
	if ins.Loop && ins.MaxIterations > 0 {
		fmt.Fprintf(out, "  max iterations: %d\n", ins.MaxIterations)
	}
	if ins.HasOutputProjection {
		fmt.Fprintln(out, "  output: explicit projection declared")
	}
	if len(ins.Goto) > 0 {
		fmt.Fprintln(out, "  goto edges:")
		for _, g := range ins.Goto {
			fmt.Fprintf(out, "    %s -> %s\n", g.From, g.To)
		}
	}

	if len(warnings) > 0 {
		fmt.Fprintf(out, "  lint (%d):\n", len(warnings))
		for _, w := range warnings {
			fmt.Fprintf(out, "    ! %s\n", w)
		}
	}
	fmt.Fprintln(out, dryRunScope)
}
