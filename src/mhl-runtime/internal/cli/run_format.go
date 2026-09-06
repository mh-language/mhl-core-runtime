package cli

import (
	"encoding/json"
	"errors"
	"io"

	"github.com/mh-language/mhl-core-runtime/internal/engine/runtime"
	"github.com/mh-language/mhl-core-runtime/internal/execsvc"
	"github.com/mh-language/mhl-core-runtime/internal/features/auth"
)

// runJSON is the machine-readable shape `mhl run --format json` writes exactly
// once, on success or failure. Consumers key on `ok`; the failure fields
// (`error`, `kind`, `step`, `hint`, `missing`, `unknown`) are absent on
// success and the run fields (`vars`, `executed`, ...) absent on failure.
type runJSON struct {
	OK       bool           `json:"ok"`
	Pipeline string         `json:"pipeline,omitempty"`
	Session  string         `json:"session,omitempty"`
	File     string         `json:"file,omitempty"`

	// success
	Executed       []string       `json:"executed,omitempty"`
	Skipped        []string       `json:"skipped,omitempty"`
	Resumed        bool           `json:"resumed,omitempty"`
	Paused         bool           `json:"paused,omitempty"`
	Broke          bool           `json:"broke,omitempty"`
	Loop           bool           `json:"loop,omitempty"`
	Iterations     int            `json:"iterations,omitempty"`
	TerminalReason string         `json:"terminal_reason,omitempty"`
	Vars           map[string]any `json:"vars,omitempty"`

	// failure
	Error   string   `json:"error,omitempty"`
	Kind    string   `json:"kind,omitempty"`
	Step    string   `json:"step,omitempty"`
	Hint    string   `json:"hint,omitempty"`
	Missing []string `json:"missing,omitempty"`
	Unknown []string `json:"unknown,omitempty"`

	// diagnostics captured off the run's output stream (step / log() lines).
	Log string `json:"log,omitempty"`
}

// writeRunJSON emits the single JSON object for a `mhl run --format json`
// invocation. logText is whatever the run wrote to its output stream.
func writeRunJSON(out io.Writer, res *execsvc.Result, runErr error, file, logText string) {
	rj := runJSON{File: file, Log: auth.Redact(logText)}

	if runErr != nil {
		rj.OK = false
		rj.Error = auth.Redact(runErr.Error())
		classifyRunError(&rj, runErr)
		if res != nil {
			rj.Pipeline = res.PipelineName
			rj.Session = res.SessionID
		}
	} else {
		rj.OK = true
		rj.Pipeline = res.PipelineName
		rj.Session = res.SessionID
		rj.Executed = res.Executed
		rj.Skipped = res.Skipped
		rj.Resumed = res.Resumed
		rj.Paused = res.Paused
		rj.Broke = res.Broke
		rj.Loop = res.Loop
		rj.Iterations = res.Iterations
		rj.TerminalReason = res.TerminalReason
		rj.Vars = res.Vars
	}

	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	_ = enc.Encode(rj)
}

// classifyRunError fills kind / step / hint / missing / unknown from the
// typed errors internal/engine/runtime returns.
func classifyRunError(rj *runJSON, err error) {
	var stepErr *runtime.StepError
	if errors.As(err, &stepErr) {
		rj.Step = stepErr.Step
		if stepErr.Pipeline != "" && rj.Pipeline == "" {
			rj.Pipeline = stepErr.Pipeline
		}
		if stepErr.Kind == "timeout" {
			rj.Kind = "step_timeout"
			rj.Hint = "the step exceeded its `timeout` clause; raise it, or bound the slow operation itself. The budget is per attempt, never persisted."
		} else {
			rj.Kind = "step_failed"
			rj.Hint = "the step raised fail() or an operation errored — see `error`. `mhl run --resume` re-enters this step."
		}
		return
	}

	var badInputs *runtime.InvalidInputsError
	if errors.As(err, &badInputs) {
		rj.Kind = "invalid_inputs"
		rj.Pipeline = badInputs.Pipeline
		rj.Missing = badInputs.Missing
		rj.Unknown = badInputs.Unknown
		rj.Hint = "pass every declared input with --input key=value and drop any the pipeline does not declare."
		return
	}

	switch {
	case errors.Is(err, runtime.ErrCheckpointDefinitionMismatch):
		rj.Kind = "definition_mismatch"
		rj.Hint = "the pipeline changed since the checkpoint. Re-run with `--resume --force`, restore the original .mh, or drop --resume."
	case errors.Is(err, runtime.ErrCheckpointSchemaTooNew):
		rj.Kind = "state_schema_too_new"
		rj.Hint = "the checkpoint was written by a newer mhl; upgrade this binary or start a fresh run."
	default:
		rj.Kind = "other"
	}
}
