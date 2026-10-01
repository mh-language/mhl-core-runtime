# test_run_workflow

`Name.run(inputs?: object)` on a declared `pipeline`/`workflow` name, valid only inside a
`test`'s `describe` block — closes the gap where `test`/`describe` could only exercise
`tool`s and expressions, never a whole workflow's control flow (`step`s, `goto`, `break`,
`pause()`, `fail()`).

Runs the pipeline's steps to completion (or to a `break`/`pause`/failure) inside an isolated,
throwaway sandbox — a temp directory and an in-memory checkpoint store, both discarded when
the call returns — and reports the outcome as a plain object instead of raising, so a test
can assert on a workflow expected to fail or break exactly as easily as one expected to
succeed:

```
{
    ok: bool,
    state: "completed" | "paused" | "broke" | "failed",
    executed: string[],   // step names that ran, in order
    vars: object,          // final (or {} on failure) variable state
    output: object,        // what a real caller gets: the `output:` projection
                           // (checked against a result type) or every var;
                           // null unless state is "completed" / "broke"
    error: string,          // "" unless state == "failed"
    step: string,            // the failing step's name, "" unless state == "failed"
    break_reason: any,        // null unless state == "broke"
    pause_reason: any,        // null unless state == "paused"
}
```

Only a problem with the *call itself* — a missing/invalid required input, a
`loop pipeline`/`loop workflow` (not supported yet) — raises a catchable error; everything the
pipeline's own steps do is reported in the returned object.

- [run_workflow_follows_goto.mh](run_workflow_follows_goto.mh) — `executed` reflects a
  `goto` jump, skipping the step in between
- [run_workflow_reports_break.mh](run_workflow_reports_break.mh) — `state: "broke"`,
  `break_reason`, and `ok: true` (a clean stop, not a failure)
- [run_workflow_reports_pause.mh](run_workflow_reports_pause.mh) — `state: "paused"`,
  `pause_reason`, and steps after the pausing one never run
- [run_workflow_applies_input_default.mh](run_workflow_applies_input_default.mh) —
  `inputs: {...}` supplies declared inputs; an omitted defaulted one falls back to its
  default, a supplied one overrides it
- [run_workflow_reports_step_failure.mh](run_workflow_reports_step_failure.mh) — `fail()`
  inside a step reports `ok: false`, `state: "failed"`, `step`, and `error` — no try/catch
  needed
- [run_workflow_missing_required_input_raises.mh](run_workflow_missing_required_input_raises.mh)
  — a missing required input raises (a problem with the call, not the workflow's own logic)
- [run_workflow_typed_signature.mh](run_workflow_typed_signature.mh) — a typed signature
  `workflow Review(req: ReviewInput): ReviewOutput`: inputs arrive as the `req` object, an
  omitted optional field (`base?: string`) reads as null, and `output` is the checked projection
- [run_workflow_output_projection.mh](run_workflow_output_projection.mh) — `output` is the
  `output:` projection when declared (renamed/computed keys, internal vars dropped), every var
  otherwise; `vars` stays the full state
- [run_workflow_output_contract_fails.mh](run_workflow_output_contract_fails.mh) — a projection
  that breaks the declared result type reports `ok: false`, `state: "failed"`, `output: null`
