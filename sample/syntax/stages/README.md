# stages

A stage is a named phase of a workflow or pipeline — what a Control-M folder or an Argo
nested DAG is to its jobs. A workflow that needs stages is a large one, so a stage is
declared the way a large workflow is organised: split by phase, one `partial` fragment per
file, the stage on the fragment's header.

```mh-fragment
// story/implementation.mh
partial workflow Story stage Implementation {
    step Implement { ... }
    step Review { ... }
}
```

Every step and parallel group of the fragment belongs to the stage — the file is the phase,
with no block and no extra indentation. The steps run exactly as without it: same names,
same order, same checkpoints. A stage adds two things:

- `goto Name` enters the stage at its first member (a step, or a whole parallel group),
  from any fragment. From outside a stage, that is the way in: `mhl lint` warns (without
  failing) about a `goto` to one of its steps other than the first — the stage's name is its
  interface, and it decides where it begins;
- `mhl run --resume --from Name` (MCP `run/resume {runId, from}`) redoes the stage in a
  stopped run: it re-enters at the stage's first step, with the variables as of the stop
  and any pending gate dropped. A plain `--resume` continues at the step the run stopped at;
- observers see it: `step.stage` in a `step_start`/`step_end` hook, `stage` on
  `mhl run --format jsonl` step events (and the stage list on `run_start`), `run/status`,
  the `--dry-run` plan, `mhl graph` (`stages`, `Node.stage`) and a box in mhl studio.

A stage can also declare its own inputs: the run asks for them on entering it, not at the
start (see `stage_inputs_pause_at_entry.mh` below).

`mhl lint` rejects a stage on a declaration that is not `partial`, a stage named like a
step, group or route, and a stage declared on fragments that are not imported one right
after the other. A fragment without a stage is fine: its steps are outside any.

- [stages_in_a_pipeline.mh](stages_in_a_pipeline.mh) + [report/](report/collect.mh) — stages
  in a `pipeline`: Collect and Analyse fragments, then [report/publish.mh](report/publish.mh)
  with no stage. A pipeline falls through from one fragment to the next, so the stages only
  name the phases — the same steps written flat run to the same output.
- [goto_stage_enters_a_parallel_group.mh](goto_stage_enters_a_parallel_group.mh) +
  [survey/](survey/ask.mh) — a stage whose first member is a `parallel` group with a
  `merge`: `goto Ask` from [survey/decide.mh](survey/decide.mh) re-runs the whole group
  (every branch, then the merge), not one branch.
- [review_cycle_with_stages_and_gates.mh](review_cycle_with_stages_and_gates.mh) +
  [story/](story/specification.mh) — stages and typed gates together, the shape of a
  delivery cycle with human review: Specification, Implementation and Consolidation, one file
  each, the decision types in [story/review.mh](story/review.mh). Each review is a
  `pause(..., decision: T)` whose answer says which phase to restart
  (`goto Implementation`, `goto Specification`); Rejected sits in the entry fragment, outside
  every phase. Its tests drive every path with `Story.run(decisions: [...])`.
- [stage_inputs_pause_at_entry.mh](stage_inputs_pause_at_entry.mh) +
  [onboarding/](onboarding/plan.mh) — stages with their own inputs. An `input` in a stage's
  fragment belongs to it: the run starts with `customer` only, pauses on entering Plan until
  `plan` is given, then on entering Billing until `card` is (`awaiting_inputs`). Each resume
  passes only that stage's inputs (`--resume --input plan=pro`); giving them all up front
  runs straight through, and `coupon` (a default) never pauses.
- [stages_across_fragments.mh](stages_across_fragments.mh) +
  [delivery/build.mh](delivery/build.mh) + [delivery/ship.mh](delivery/ship.mh) — Build and
  Ship fragments handing over by stage name (`goto Build`, `goto Ship`), Ship sending the run
  back to Build in another file. `mhl graph stages_across_fragments.mh --merged` lists both
  stages with the file each came from; `mhl run ... --dry-run` lists them in the plan.
