# pipeline_vs_workflow

`pipeline` and `workflow` are the two execution-unit keywords. They parse into
the same node and run the same way — typed `input`, pipeline-scoped `var`/`mem`,
`checkpoint`/`repeat` config, ordered `step`s, `parallel` groups, and the
optional `loop` prefix. The difference is a single static rule:

| | `pipeline` | `workflow` |
| --- | --- | --- |
| Steps run in declared order, each once | ✅ | ✅ |
| `parallel`, `spawn`/`wait` | ✅ | ✅ |
| `loop` prefix + `repeat: { ... }` | ✅ | ✅ |
| `goto <step>` and `goto match` | ❌ `mhl lint` rejects them | ✅ forward or backward |
| Reading guarantee | "runs top to bottom" | "may branch — read the `goto`s" |

`mhl lint` also checks that every `goto` target names a step declared in the
same `workflow`.

`goto match value { pattern -> StepName ... _ -> fail(reason) }` selects a
step using the same pattern equality and first-match order as expression
`match`. Each destination is a literal step identifier, checked by `mhl lint`;
it is not built from a string at runtime. An unmatched value without `_`
fails at runtime. The fallback can explicitly fail with a computed reason.
For reuse inside one workflow, declare the table once as
`route Generate(artifact: string) { ... }` and invoke it from any step with
`goto Generate(artifact)`. A route has exactly one typed parameter and static
step destinations. It is workflow-local and does not return a string. A tool
may return a string, but `goto` deliberately does not accept computed step
names. Expression `nameof(BriefGenerate)` checks top-level declarations, so it
cannot name a workflow step.

A `step` or `parallel` group may also carry a `timeout <duration>` header
clause (`step Build timeout 3m { ... }`): the runtime caps that step's
wall-clock and fails it like `fail()` if it runs over. It stays resumable —
the duration is measured fresh on each attempt, never persisted — and
`mhl lint` rejects a non-positive value.

- [linear_pipeline_and_branching_workflow.mh](linear_pipeline_and_branching_workflow.mh)
  — a linear `pipeline` beside a `workflow` whose `Gate` step jumps back to
  `Review`, exercising runnable state-machine behaviour built on `goto`.
- [goto_nameof_target.mh](goto_nameof_target.mh) — `goto nameof(Review)`, the
  `nameof(...)`-wrapped spelling of a goto target: pure grammar sugar for the
  bare `goto Review` form (not the real `nameof(...)` builtin evaluated as a
  call), checked identically by `mhl lint`'s goto rules.
- [goto_match_artifact_review.mh](goto_match_artifact_review.mh) — a review gate
  that routes feedback to generation and approved artifacts to commit.

Run from the repository root. The first command pauses; the second regenerates
and pauses again; the third commits:

The file also contains `test` descriptions for the initial pause, feedback
route, both commit routes, and the unknown-artifact fallback. Run them with
`mhl test sample/syntax/pipeline_vs_workflow/goto_match_artifact_review.mh`.

```sh
mhl run sample/syntax/pipeline_vs_workflow/goto_match_artifact_review.mh --session review-brief --input artifact=brief
mhl run sample/syntax/pipeline_vs_workflow/goto_match_artifact_review.mh --session review-brief --resume --input feedback=change-title
mhl run sample/syntax/pipeline_vs_workflow/goto_match_artifact_review.mh --session review-brief --resume --input approved=true
```
