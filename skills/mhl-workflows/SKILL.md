---
name: mhl-workflows
description: Design mhl pipeline and workflow declarations. Use for step structure, pipeline vs workflow, goto and route, loop and repeat, parallel, spawn and wait, pause and resume, checkpoints, typed signatures, output projection and lifecycle hooks.
---

# mhl workflows

The executable unit of mhl. Assumes the `mhl-language` skill. The design question behind
every rule here: **what happens when this step runs twice?** mhl checkpoints between
steps and gives at-least-once execution, so a step can always repeat.

## Choose the declaration

| Need | Declaration |
|---|---|
| Steps run top to bottom, each once | `pipeline` |
| Branching, retry cycles, a state machine | `workflow` (allows `goto`) |
| Repeat until a condition holds | `loop pipeline` or `loop workflow` |
| Same flow, two entry points differing in a few inputs | `workflow B = A with { level: Level.x }` |
| Shared flow with no entry point of its own | `internal workflow` |
| One workflow split across files | `partial workflow` |

Start with `pipeline`. Move to `workflow` when a real branch appears: `pipeline` is a
promise to the reader that it runs top to bottom.

## Anatomy

Keep this order: `description`, inputs, `const`/`var`/`mem`, hooks, `output`, steps.
Apply the [code layout conventions](../mhl-language/SKILL.md#code-layout) inside
steps as well: expand conditional bodies and separate guards, data preparation,
agent/tool calls, result validation, artifact writes, and the final control-flow
exit with blank lines when these are distinct phases. Keep related assignments
together, leave one blank line between sibling steps, and keep step headers on
one line. A step with a single action stays a simple three-line block.

```mh
type ReviewInput = { diff: string, base?: string }
type ReviewOutput = { approved: bool, summary: string }

agent Reviewer {
    command: "echo"
    args: ["${prompt}"]
}

tool Verdict {
    is_clean(review: string): bool -> !review.contains("BLOCKER")
}

workflow ReviewChange(req: ReviewInput): ReviewOutput {
    description: "Reviews a diff and reports whether it can merge."

    var review = ""
    var approved = false

    output: {
        approved: approved
        summary: review
    }

    step Analyze timeout 5m {
        var base = req.base ?? "main"

        self.review = Reviewer.run(prompt: "Review against ${base}: ${req.diff}")
    }

    step Decide {
        self.approved = Verdict.is_clean(review)

        if (!self.approved) {
            goto Reject
        }

        complete()
    }

    step Reject {
        log.warn("blocked:", review)
    }
}

test review_change {
    describe clean_diff_is_approved {
        var result = ReviewChange.run(inputs: {diff: "fix typo"})

        are_equal(result.state, "completed")
        are_equal(result.executed, ["Analyze", "Decide"])
        are_equal(result.output.approved, true)
    }

    describe blocker_takes_the_reject_branch {
        var result = ReviewChange.run(inputs: {diff: "BLOCKER here", base: "dev"})

        are_equal(result.executed, ["Analyze", "Decide", "Reject"])
        are_equal(result.output.approved, false)
    }
}
```

- **Signature.** `workflow X(req: In): Out` makes the fields of `In` the inputs, bound as
  one read-only object, and `Out` requires an explicit `output:`, checked when the run
  ends. Use named aliases in the header; inline shapes confuse the editor. Without a
  signature, declare `input x: T` per line. The two forms do not mix.
- **`output:`** lists exactly what a caller receives. Without it, every var leaks to
  callers. It is evaluated once at the end, so keep side effects out of it.
- **`description:`** becomes the MCP tool description. Without it, callers see a generic
  sentence.
- **`///` above an `input`** documents it: it becomes the argument's `description` in the
  MCP `inputSchema`, and appears in `--dry-run`, the editor's hover and mhl studio's forms.
  Say what a caller must pass (`/// The repository to review, an absolute path.`), not the
  type, which the schema already gives. A plain `//` stays a private note.

## Step design

A step is the unit of progress, resume and timeout. Cut steps along these lines:

- One step per externally visible effect, so a resume repeats as little as possible.
  Split prepare, approve and apply. An expensive agent call deserves its own step.
- Put logic in `tool` methods and leave the step as orchestration: calls, assignments,
  and the control-flow exit. `goto`, `pause` and `complete` have to stay in the step.
- Share state through pipeline `var`s written with `self.x`. They must be JSON-shaped: a
  closure or spawn handle in one fails the run at the next checkpoint. Extract
  `handle.result` into a `var` before the step ends.
- Add `timeout <duration>` to any step that blocks on an agent or the network. An agent
  has no `timeout` property; the step clause is what aborts it.

## Branching

```mh
enum Kind { Bug, Feature }

workflow Triage {
    input kind: Kind
    input attempts_allowed: number = 2

    var attempts = 0

    // A route is a reusable, lint-checked dispatch table.
    route Handle(kind: Kind) {
        Kind.Bug     -> FixBug
        Kind.Feature -> PlanFeature
        _ -> fail("unhandled kind: ${kind}")
    }

    step Dispatch {
        goto Handle(kind)
    }

    step FixBug {
        self.attempts = self.attempts + 1

        if (self.attempts < attempts_allowed) {
            goto FixBug
        }

        goto Done
    }

    step PlanFeature {
        goto Done
    }

    // A convergence step always calls complete(): it stays terminal
    // wherever it lands in the merged step order.
    step Done {
        complete()
    }
}

test triage {
    describe bug_retries_to_its_limit {
        var result = Triage.run(inputs: {kind: "Bug"})

        are_equal(result.executed, ["Dispatch", "FixBug", "FixBug", "Done"])
    }
}
```

- Prefer `goto match value { "a" -> StepA  _ -> fail("...") }` or a `route` over chains of
  `if`. Lint checks every destination.
- To start the run with a dispatch, mark the route `entry route Handle(kind: Kind) { ... }`
  instead of writing a `Dispatch` step that only calls it. The run begins at the route, its
  parameter bound to the input of the same name and type; it runs as a step named `Handle`
  (so `executed` starts with it) and stays callable with `goto Handle(x)`. One per workflow.
- Give every backward `goto` a counter in a pipeline `var`. The only built-in limit is
  10000 visits per step, which fails the run with reason `max_step_visits`.
- Steps fall through to the next declared step. That is normal; only end a step with
  `complete()` when it must be terminal.

## Loops

```mh
loop workflow PollBuild {
    description: "Polls until the build reports done."

    mem checks = 0
    mem status = "pending"

    repeat: {
        stop_when: status == "done"
        max_iterations: 5
    }

    step Check timeout 30s {
        checks = checks + 1
        status = if (checks >= 3) "done" else "pending"

        if (status == "failed") {
            break "build failed"
        }
    }
}
```

- `stop_when` reads `mem` and `memory` only. A pipeline `var` is invisible there and
  resets every iteration, so anything carried between iterations must be `mem`.
- `mem` also outlives the run: a second `mhl run` starts from the stored value. Call
  `x.reset()` in a step when each run must start fresh, or scope a `memory` path with
  `${context.session_id}`.
- Always set a ceiling. `max_iterations` reached is a soft stop that fires `session_end`,
  not `stop_failure`, so check `stop_when`'s condition if the outcome matters.
- `loop workflow Refine max 3 { ... }` is the short form when the ceiling is the whole
  policy. It cannot be combined with `repeat { max_iterations }`.

## Concurrency

| Need | Use |
|---|---|
| Several agent calls inside one step | `spawn` + `wait` |
| One agent over a list | `spawn xs = A.run(prompt: "${item}") for item in items` |
| Several whole steps at once | `parallel Name { step A {...} step B {...} }` |
| Branches that hand results to a join | `parallel Name { var a = "" ... step A {...} merge { ... } }` |
| Branches that fill one var | `parallel Name { ... merge (branches) { ... } }` |

```mh
agent Checker {
    command: "echo"
    args: ["${prompt}"]
}

pipeline FanOut {
    input files: string[] = ["a.go", "b.go"]
    spawn: { max_concurrency: 2 }

    var results = []
    var left = ""
    var right = ""

    step Check timeout 2m {
        spawn checks = Checker.run(prompt: "check ${item}") for item in files
        wait checks timeout: 90s

        self.results = checks.map((handle) -> handle.result)
    }

    parallel Gather timeout 2m {
        step Left {
            left = Checker.run(prompt: "left")
        }

        step Right {
            right = Checker.run(prompt: "right")
        }
    }

    step Merge {
        log(results.size(), left, right)
    }
}

test fan_out {
    describe every_file_is_checked {
        var result = FanOut.run()

        are_equal(result.vars.results, ["check a.go", "check b.go"])
        are_equal(result.vars.right, "right")
    }
}
```

- `spawn` accepts only `<Agent>.run(...)`, not a router or a tool.
- `wait a, b` fails fast. `wait any a, b` takes the first success. `wait 2 of a, b, c` is a
  quorum. `on_error: "collect"` keeps the step alive; then read `.ok`, `.error`, `.status`.
- `parallel` branches get copies of the pipeline vars, merged at the barrier. Two branches
  assigning the same var different values is a run failure: give each branch its own var, or
  end the group with a `merge` (below). `goto` and `break` are not allowed inside a group,
  and a group re-runs entirely on resume. A `goto` may name the group itself
  (`goto Verify`), which re-runs every branch; never a branch step.

A group may declare its own vars before its branches. They are fresh each time the group
starts, read and written as `self.x` by its branches and merge, and gone after the group.
Give each branch one to fill, and combine them in a plain `merge { ... }`:

```mh
agent Claude {
    command: "echo"
    args: ["claude:", "${prompt}"]
}

agent Codex {
    command: "echo"
    args: ["codex:", "${prompt}"]
}

workflow Panel {
    input question: string = "caching"

    var answers = []

    parallel Ask {
        var from_claude = ""
        var from_codex = ""

        step AskClaude {
            self.from_claude = Claude.run(prompt: question)
        }

        step AskCodex {
            self.from_codex = Codex.run(prompt: question)
        }

        merge {
            self.answers.append(self.from_claude)
            self.answers.append(self.from_codex)
        }
    }
}

test panel {
    describe both_answers_are_kept {
        var result = Panel.run()

        are_equal(result.vars.answers, ["claude: caching", "codex: caching"])
    }
}
```

When branches should add to the same var instead, a `merge (branches) { ... }` block, last
in the group, combines them. It runs once after every branch, in the workflow's scope, and
`branches.<Step>.<var>` is what that branch left. A var the branches wrote differently must
be assigned there. Vars they agree on are already merged.

```mh
agent Claude {
    command: "echo"
    args: ["claude:", "${prompt}"]
}

agent Codex {
    command: "echo"
    args: ["codex:", "${prompt}"]
}

workflow Consult {
    input question: string = "caching"

    var replies = []

    parallel Ask {
        step AskClaude {
            self.replies = [...self.replies, Claude.run(prompt: question)]
        }

        step AskCodex {
            self.replies = [...self.replies, Codex.run(prompt: question)]
        }

        merge (branches) {
            self.replies = [...branches.AskClaude.replies, ...branches.AskCodex.replies]
        }
    }

    step Report {
        log(replies)
    }
}

test consult {
    describe both_replies_are_kept {
        var result = Consult.run()

        are_equal(result.vars.replies, ["claude: caching", "codex: caching"])
    }
}
```

- A merge may end the group with `goto X` (or `goto Ask` to re-run it) or `complete()`, like
  the end of a step. `break` and `pause()` are rejected there. `branches` is read-only and
  must not reuse an input or var name.
- `self.replies.append(x)` as a statement means `self.replies = self.replies.append(x)`.
  In a branch, that write counts like any other, so two branches appending need a merge.
- Concatenating the branches' lists repeats whatever the var held before the group. Prefer
  group vars, which start fresh every time the group runs.
- A group var must not reuse a workflow input or var name. It does not exist after the group.

## Human in the loop

`pause("reason")` parks the run and keeps its state. Resuming **re-enters the same step
from its first line** with the merged inputs, and pipeline vars keep what was already
written. So a gate does nothing before its `pause`, or it does that work twice.

```mh
workflow Deploy {
    description: "Prepares a release, waits for approval, then applies it."

    input approved: bool = false

    var plan = ""

    step Prepare {
        self.plan = "deploy v2"
    }

    // The gate holds only the decision.
    step Gate {
        if (!approved) {
            pause("approve: ${plan}")
        }
    }

    step Apply {
        log.info("applying", plan)
    }
}

test deploy {
    describe unapproved_run_parks_at_the_gate {
        var result = Deploy.run()

        are_equal(result.state, "paused")
        are_equal(result.executed, ["Prepare", "Gate"])
        are_equal(result.pause_reason, "approve: deploy v2")
    }

    describe approval_runs_to_the_end {
        var result = Deploy.run(inputs: {approved: true})

        are_equal(result.executed, ["Prepare", "Gate", "Apply"])
    }
}
```

Resume with `mhl run deploy.mh --input approved=true --resume`, or `mhl_run_resume` over
MCP. The decision arrives as an input, so give it a default that means "not yet".

### Typed gates

When a person has to choose between outcomes, or a gate can be reached more than once
(a review loop), make the pause a **gate**: `pause(reason, decision: T)` returns the
decision. `T` is a declared `type` or `enum`, or `string`/`number`/`bool`. The run reports
`awaiting` (the step and `T` as JSON Schema); a resume passes the answer with
`--decision '<json>'` or `run/resume {runId, decision}`. It is checked against `T` before
anything runs and answers **that gate once**: the next gate reached pauses again. So an old
approval can never approve a later round, and no `*_round` inputs are needed. A plain
`pause(reason)` is unchanged. A gate is not allowed in a `loop` workflow yet.

```mh
enum Action { Approve, Revise, Reject }
type Review = { action: Action, feedback?: string }

workflow Publish {
    description: "Drafts until a reviewer approves or rejects."

    var drafts = 0
    var status = "drafting"

    step Draft {
        self.drafts = drafts + 1
    }

    step Review {
        var d = pause("review draft ${drafts}", decision: Review)

        goto match d.action {
            Action.Approve -> Approved
            Action.Revise -> Draft
            Action.Reject -> Rejected
        }
    }

    step Rejected {
        self.status = "rejected"
        complete()
    }

    step Approved {
        self.status = "approved after ${drafts}"
    }
}

test publish {
    describe the_gate_reports_what_it_waits_for {
        var result = Publish.run()

        are_equal(result.state, "paused")
        are_equal(result.awaiting.type, "Review")
    }

    describe each_decision_answers_one_round {
        var result = Publish.run(decisions: [{action: "Revise"}, {action: "Approve"}])

        are_equal(result.state, "completed")
        are_equal(result.output.status, "approved after 2")
    }
}
```

In a test, `Name.run(decisions: [...])` resumes the run with the next decision each time
it stops at a gate. Resume a real run with
`mhl run publish.mh --resume --decision '{"action": "Approve"}'`.

## Failure and resume

- Checkpointing is on by default. `--resume` continues from the failed step. Declare
  `checkpoint: { ttl: 30d }` only to tune it, and `checkpoint: { enabled: false }` only
  for a flow that must never half-replay.
- A step that fails or dies is re-run whole. Make external effects **idempotent**: pass
  your own idempotency key, check before acting, or give a repeated agent call a `cache`.
- Editing the workflow invalidates its checkpoints: `--resume` is refused with a
  definition mismatch. `--resume --force` overrides that when you know the edit is safe.
- Use `fail()` for a real failure, `break "reason"` for a clean early stop, and `try`
  around a step's body only when there is a recovery to perform.

## Lifecycle hooks

`session_start`, `session_end`, `step_start`, `step_end` and `stop_failure` take one
parameter and exist to **observe** a run: logging, audit, metrics. Returning a value is a
runtime error, an error inside one aborts the run (except in `stop_failure`, where it is
only logged), and none of them can change the flow. `session_end` does not fire on
`pause()` or on a failure. Hooks do not fire under `Name.run()` in tests.

## Splitting across files

```mh-fragment
// discovery.mh
import "discovery.brief.mh"

partial workflow Discovery {
    var data = null

    entry step Dispatch {
        goto BriefGenerate
    }

    step Done {
        complete()
    }
}

// discovery.brief.mh
partial workflow Discovery {
    step BriefGenerate {
        self.data = {title: "brief"}

        goto Done
    }
}
```

Exactly one `entry step` (or `entry route`) across all fragments; `entry` on a step is rejected
outside `partial`. The
last step of a fragment must end in an unconditional exit, or lint reports a fallthrough
into another file. Factor what generate/commit pairs repeat into a shared `tool`, keeping
only the final `goto` in each step.

A fragment is a file; a stage is a phase. A large workflow is best split by phase, with the
stage on each fragment's header (see Stages).

## Stages

A stage names a phase of a workflow. The steps run exactly as without it: same names, same
checkpoints, `goto` still names a step. `goto Name` enters the stage at its first member,
which reads well for "start the phase over". Observers see the stage: `step.stage` in a step
hook, `stage` in step events and `run/status`, and a box in mhl studio.

A workflow that needs stages is usually big: split it by phase, one `partial` fragment per
file, and put the stage on each fragment's header. The file is the phase, with no extra
indentation:

```mh-fragment
// story/implementation.mh
partial workflow Story stage Implementation {
    step Implement {
        base.attempts = base.attempts + 1
    }

    step Review {
        if (!approved) {
            goto Implementation
        }

        goto Consolidation
    }
}
```

From another stage, go to the stage, not to one of its steps: `goto Specification`, not
`goto ApproveSpec`. The other fragment then depends only on the phase's name, and the
stage decides where it begins. `mhl lint` warns (without failing) about a `goto` into the
middle of another stage. When a caller must skip ahead, give the stage a first step that
dispatches:

```mh-fragment
partial workflow Story stage Specification {
    step Begin {
        if (specs_exist) {
            goto ApproveSpec
        }
    }

    step DraftSpec { ... }

    step ApproveSpec { ... }
}
```

Resume re-enters the step a run stopped at, never a stage's start. To redo a whole phase
of a stopped run, resume with `--from <stage>` (`mhl_run_resume {runId, from}`): it
re-enters the stage at its first step with the variables as of the stop, so the phase
must be safe to run again.

In a stage fragment, write the base's state as `base.name`: `base.attempts`,
`{base.story_id, base.project}`, `base.{plan, price} = offer`. The base is the fragment with the `entry step`. `base.x`
runs like `self.x` and says where the name lives, and `mhl lint` checks it: `base.x`
the base does not declare is an error, and a base name used bare in another fragment is a
warning. Keep a phase's own state in its own fragment (`var consolidated = {}`), where a
bare name is fine.

A stage can ask for its own inputs. An `input` in the stage's fragment belongs to that
stage. The run starts with the workflow's inputs only, and pauses on entering the stage until
its required inputs are given:

```mh-fragment
// onboarding/plan.mh
partial workflow Onboarding stage Plan {
    input plan: string

    step Choose {
        steps.append("plan ${plan}")
        goto Billing
    }
}
```

The paused result names them in `awaiting_inputs` (`{stage, step, inputs, schema}`). Resume
with only those: `--resume --input plan=pro`, or `mhl_run_resume {runId, arguments}`. A
resume still missing one is refused, and the run stays paused. Giving every input up front
runs straight through, and an input with a default never pauses. Put an input in a stage
when only that phase needs it and it is decided later, for example by a person once the
earlier phases are done. Keep the inputs every phase reads in the entry fragment. A stage
input is not allowed in a `loop` workflow.

A stage needs `partial`. Its name must not repeat a step, group or route name, and fragments
sharing a stage are imported one right after the other. A fragment without a stage is fine:
its steps are outside any. See `sample/syntax/stages/` for complete multi-file examples.

## Review checklist

Before finishing a workflow, confirm each item:

- Every step that mutates the outside world is safe to run twice.
- Every step blocking on an agent or the network has a `timeout`.
- Every backward `goto` and every `loop` has a bound.
- Every terminal convergence step calls `complete()`.
- No `var` inside a step shadows a pipeline var that step means to write.
- Every gate pauses before doing work; a gate a person may reach twice is typed
  (`pause(..., decision: T)`), so no old decision answers it.
- A long workflow is split by phase, each fragment naming its `stage`.
- `output:` exposes only what callers should see, and `description:` is set.
- Each branch and terminal state has a test (see `mhl-testing`).
- Steps use the shared code layout: logical phases and final exits are separated
  by blank lines, and sibling steps and parallel branches are visually distinct.
