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
        if (!self.approved) goto Reject
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
        if (self.attempts < attempts_allowed) goto FixBug
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
        if (status == "failed") break "build failed"
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
        step Left { left = Checker.run(prompt: "left") }
        step Right { right = Checker.run(prompt: "right") }
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
  assigning the same var is a run failure, so give each branch its own var. `goto` and
  `break` are not allowed inside a group, and a group re-runs entirely on resume.

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
        if (!approved) pause("approve: ${plan}")
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
    entry step Dispatch { goto BriefGenerate }
    step Done { complete() }
}

// discovery.brief.mh
partial workflow Discovery {
    step BriefGenerate {
        self.data = {title: "brief"}
        goto Done
    }
}
```

Exactly one `entry` step across all fragments; `entry` is rejected outside `partial`. The
last step of a fragment must end in an unconditional exit, or lint reports a fallthrough
into another file. Factor what generate/commit pairs repeat into a shared `tool`, keeping
only the final `goto` in each step.

## Review checklist

Before finishing a workflow, confirm each item:

- Every step that mutates the outside world is safe to run twice.
- Every step blocking on an agent or the network has a `timeout`.
- Every backward `goto` and every `loop` has a bound.
- Every terminal convergence step calls `complete()`.
- No `var` inside a step shadows a pipeline var that step means to write.
- Every gate pauses before doing work.
- `output:` exposes only what callers should see, and `description:` is set.
- Each branch and terminal state has a test (see `mhl-testing`).
