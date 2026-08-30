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
| `goto <step>` | ❌ `mhl lint` rejects it | ✅ forward or backward |
| Reading guarantee | "runs top to bottom" | "may branch — read the `goto`s" |

`mhl lint` also checks that every `goto` target names a step declared in the
same `workflow`.

- [linear_pipeline_and_branching_workflow.mh](linear_pipeline_and_branching_workflow.mh)
  — a linear `pipeline` beside a `workflow` whose `Gate` step jumps back to
  `Review`. Runnable state-machine behaviour built on `goto` is exercised by
  [../../mhl.workflow.development/](../../mhl.workflow.development/README.md).
