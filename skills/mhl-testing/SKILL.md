---
name: mhl-testing
description: Test and validate mhl programs. Use for test and describe blocks, mocking agents without calling an LLM, asserting workflow outcomes with Name.run, and reading mhl lint, mhl test or mhl run --dry-run results.
---

# mhl testing

Assumes the `mhl-language` skill. mhl tests are `.mh` code: a `test` holds `describe`
blocks, each a fresh scope running ordinary statements plus assertions. Tests must be
**hermetic**: no real model, no network, no paid call, so they can run on every change.

## Validation layers

| Command | Proves | Cost |
|---|---|---|
| `mhl lint <path>` | syntax, types, names, imports, agent config | instant |
| `mhl test <path>` | behavior of tools, prompts, workflow control flow | fast, hermetic if you keep it so |
| `mhl run x.mh --dry-run --input k=v` | input contract and static step plan | instant, runs nothing |
| `mhl run x.mh` | the real thing | real agents, real effects |

`mhl test` recurses a directory and exits 1 on any failure. Naming a single file with no
`test` block is also an error. `incomplete("reason")` records a skip and never fails.

Treat a task as done when lint prints `No problems found.` and `mhl test` exits 0 with the
new behavior covered by a test that failed before the change.

## Assertions

`are_equal(actual, expected)`, `are_not_equal(a, b)`, `is_true(v)`, `is_false(v)`,
`is_null(v)`, `not_null(v)`, `greater_than(a, b)`, `less_than(a, b)`,
`greater_than_or_equal(a, b)`, `less_than_or_equal(a, b)`, `includes(array, value)`,
`incomplete(reason)`.

`are_equal` is deep and order-sensitive. `is_true` errors on a non-bool, so compare first:
`is_true(x == 1)`. There is no `raises` assertion; use `try`/`catch` and assert the
message.

## Mocking agents

An agent is a subprocess, so a mock is a cheap command. Three shapes cover almost
everything:

```mh
// 1. Echo: returns the built prompt, so a test can assert on its content.
agent Reviewer {
    command: "echo"
    args: ["${prompt}"]
}

// 2. Canned answer: a fixed response, for parsing and branching logic.
agent Classifier {
    command: "sh"
    args: ["-c", "printf '{\"label\":\"bug\"}'"]
}

// 3. Failure: exercises fallback and error paths.
agent Broken {
    command: "sh"
    args: ["-c", "echo overloaded >&2; exit 1"]
}

agent Resilient {
    command: "sh"
    args: ["-c", "exit 1"]
    fallback: [Reviewer]
}

workflow Triage {
    input title: string
    var label = ""

    step Classify {
        self.label = json.parse(Classifier.run(prompt: title)).label
    }

    step Review {
        log(Broken.run(prompt: title))
    }
}

test mocks {
    describe echo_exposes_the_prompt {
        is_true(Reviewer.run(prompt: "look at ${1 + 1}").contains("look at 2"))
    }

    describe failure_raises_when_called_directly {
        var message = ""
        try {
            Broken.run(prompt: "x")
        } catch (e) {
            message = "${e}"
        }
        is_true(message.contains("overloaded"))
    }

    describe fallback_takes_over {
        are_equal(Resilient.run(prompt: "x"), "x")
    }

    describe a_failed_step_is_reported_not_raised {
        var result = Triage.run(inputs: {title: "t"})
        are_equal(result.state, "failed")
        are_equal(result.step, "Review")
        is_true(result.error.contains("overloaded"))
    }
}
```

To test a production agent, make its command swappable and override it from the shell:

```mh-fragment
agent Reviewer {
    command: env("REVIEWER_CMD")      // fails closed when unset
    args: ["-p", "${prompt}"]
}
```

`REVIEWER_CMD=echo mhl test .` then runs it hermetically, while production sets
`REVIEWER_CMD=claude`.

## Testing workflows

Inside a `describe`, `Name.run(inputs: {...})` runs a declared pipeline or workflow and
**returns** its outcome instead of raising:

| Field | Meaning |
|---|---|
| `ok` | false only when `state` is `"failed"` |
| `state` | `"completed"`, `"paused"`, `"broke"`, `"failed"` |
| `executed` | step names in order, the best view of the path taken |
| `vars` | final state; `{}` on failure |
| `output` | what a real caller receives; null unless completed or broke |
| `error`, `step` | set when failed |
| `break_reason`, `pause_reason` | set when broke or paused |

Assert on `state` and `executed` first: together they pin the control flow. A workflow's
own `fail()`, `break` and `pause` are data here. Only a problem with the call raises, such
as a missing or mistyped required input:

```mh
enum Level { low, high }

workflow Gate {
    input level: Level
    var note = ""

    output: { note }

    step Check {
        if (level == Level.high) break "needs a human"
        self.note = "auto"
    }
}

test gate {
    describe low_completes {
        var result = Gate.run(inputs: {level: "low"})
        are_equal(result.state, "completed")
        are_equal(result.output, {note: "auto"})
    }

    describe high_stops_cleanly {
        var result = Gate.run(inputs: {level: "high"})
        is_true(result.ok)
        are_equal(result.state, "broke")
        are_equal(result.break_reason, "needs a human")
    }

    describe a_bad_input_raises {
        var message = ""
        try {
            Gate.run(inputs: {level: "medium"})
        } catch (e) {
            message = "${e}"
        }
        is_true(message.contains("low"))
    }
}
```

### Limits of `Name.run`

- It works only inside `describe`, never from a step.
- `loop pipeline` and `loop workflow` targets are refused. Extract the body into tool
  methods and test those, or run the loop end to end with `mhl run`.
- It covers one run, so `--resume` itself is out of reach. Simulate a resume by calling
  again with the decision input, and check the real path once with `mhl run ... --resume`.
- Lifecycle hooks do not fire.
- Only `mem` variables and checkpoints are sandboxed. `fs`, `cmd`, `git`, agents and
  `memory` files are real and run in the working directory, so write under a throwaway
  path and clean it up, for example with `dir.clear(".mhl/test-tmp")`.

## What to cover

For each workflow, one `describe` per:

- terminal state it can reach: `completed`, `broke`, `paused`, `failed`;
- `goto` target, `route` arm and `match` branch;
- bound: the retry counter hitting its limit;
- `output:` shape, and the output-contract failure when the type is declared;
- trap lint misses: shadowed pipeline vars, unchecked `exit_code`, values lost in a lambda.

Name each `describe` as a behavior (`unapproved_run_parks_at_the_gate`), so the report
reads as a specification.

Keep tests beside the declarations they cover, or in a sibling file using
`import {Name} from "x.mh"` with `export` on the declaration. `internal` tool methods are
testable only from the declaring file. Add `.mhl/` to `.gitignore`.

## Reading failures

| Output | Meaning |
|---|---|
| `undefined variable "y"` in a step | Assigned without declaring it in the pipeline |
| `assignment target must be a plain variable or an array index` | `o.a = v`; use `o["a"] = v` |
| `stop_when references var` | State for `stop_when` must be `mem` |
| `args contains ${prompt}, which is never substituted` | Placeholder must be a whole `args:` element |
| `duration values are not supported in expressions` | Move the literal to its use site |
| `pipeline variable is not checkpointable` | A closure or handle is in a pipeline `var` |
| `checkpoint was written for a different pipeline definition` | The workflow changed since the checkpoint; rerun, or `--resume --force` if safe |
| `step "A" revisited more than 10000 times` | A `goto` cycle with no counter |
