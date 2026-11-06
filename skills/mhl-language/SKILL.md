---
name: mhl-language
description: Core syntax, types and semantics of mhl (Meta-Harness Language, .mh files), a language most models have not seen. Load before writing, editing, reviewing or debugging any .mh file, or when mhl lint reports a parse, scope or type error.
---

# mhl language

mhl is a declarative language for AI agent pipelines: agents, tools, memory, MCP
servers, prompts, and the pipelines that wire them together. It reads like TypeScript,
and that resemblance is where most bugs come from. Trust this file and the compiler
over your priors.

The other `mhl-*` skills go deeper: `mhl-workflows` (steps, branching, resume),
`mhl-agents` (agents, prompts, routers), `mhl-tools` (tools, native ops, memory,
extensions), `mhl-testing`, `mhl-serving`. Every native op and value method is in
[references/stdlib.md](references/stdlib.md).

## The verification loop

`mhl` is the source of truth. Run this loop on every change:

1. `mhl lint <file-or-dir>` until it prints `No problems found.`
2. `mhl test <file-or-dir>` until it exits 0.
3. `mhl run <file> --dry-run --input key=value` to validate the input contract and see
   the step plan. It executes nothing.

A plain `mhl run` launches real agents and real side effects, so treat it as a deliberate
action, not a syntax check.

When `mhl` is missing, ask before installing it (`install.sh` from
`github.com/mh-language/mhl-core-runtime`), and state plainly that any `.mh` you hand
over without it is unverified.

Lint is not complete. It misses the traps marked *(lint silent)* below, so a clean lint
does not prove those. Cover them with a test.

## File shape

A file is a flat list of declarations: `import`, `agent`, `router`, `prompt`, `skill`,
`schema`, `memory`, `tool`, `extension`, `pipeline`, `workflow`, `type`, `enum`, `test`.
Any of them can carry `export`. Statements live only inside a step, a tool method, a
lambda body, or a `describe` block, so reference snippets showing a bare `var x = 1` are
excerpts of one of those.

```mh
// Comments are // only.
agent Writer {
    command: "echo"            // swap for a real CLI such as "claude"
    args: ["${prompt}"]
}

prompt Greeting(name: string) {
    "Write a greeting for ${name}."
}

tool Text {
    slug(value: string): string -> value.trim().to_lower().replace(" ", "-")
}

pipeline Main {
    description: "Writes a greeting and logs its slug."
    input name: string
    var reply = ""

    step Greet {
        reply = Writer.run(prompt: Greeting(name: name))
    }

    step Report {
        log(Text.slug(reply))
    }
}

test main {
    describe greets_by_name {
        var result = Main.run(inputs: {name: "Ada"})
        are_equal(result.state, "completed")
        are_equal(result.executed, ["Greet", "Report"])
        is_true(result.vars.reply.contains("Ada"))
    }
}
```

`mhl run file.mh` runs the first non-`internal` pipeline in the file; there is no flag
to pick another. Keep one entry point per file.

Naming convention: `PascalCase` for declarations, enum variants and steps; `snake_case`
for inputs, vars, tool methods, tests and describes.

## Scope and state

| Declaration | Where | Lifetime |
|---|---|---|
| `input x: T = default` | pipeline body | whole run, read-only |
| `var x = e` | pipeline body | shared by every step, survives `goto`, `pause` and `--resume`; reset on each `loop` iteration |
| `const x = e` | pipeline, step, tool | single assignment |
| `mem x = e` | pipeline body only | persisted to disk: survives `loop` iterations, `--resume`, and separate `mhl run`s until `x.reset()` |
| `var x = e` | step, tool method, lambda | that block only |

Write pipeline state with a bare `x = value` or, better, `self.x = value`, which always
means the pipeline's binding. `var x` inside a step declares a new local that hides the
pipeline's `x`, so the pipeline value stays unchanged *(lint silent)*. Reserve `var` in a
step for values that die with the step.

To change an input, copy it into a pipeline `var` first.

## Priors that break

| Habit from other languages | What mhl needs |
|---|---|
| `obj.field = v` | `obj["field"] = v`, including nested `o["a"]["b"] = v`, or `obj = obj with { field: v }`. `.field` is read-only outside `self.name` |
| `list.append(x)` mutates | `list = list.append(x)` or `list += [x]`. Array methods return new arrays *(lint silent)* |
| `b = a` aliases an object | Objects and arrays copy on every read. Use `ref { ... }` when sharing is the point |
| `break` leaves a loop | `break` ends the **whole run**, even inside `for`/`while` *(lint silent)*. To stop early, use `.find()`/`.filter()`, or a `while` with a flag |
| `continue` | Does not exist, and fails only when reached *(lint silent)*. Put `.filter()` first, or an `if` around the body |
| Closures write outer vars | A lambda sees a snapshot, and its assignments never reach the caller *(lint silent)*. Return the value, or use `.reduce()` |
| Named args bind by name | Only `prompt`, `Agent.run` and native options do. **Tool methods and lambdas bind by position**, and names are ignored: `T.f(b: 1, a: 2)` puts `1` in `a` *(lint silent)* |
| `Prompt("x")` | Prompts require named args: `Prompt(name: "x")` |
| `var t = 30s` | Durations are not values *(lint silent)*. Write the literal at its use site: `timeout: 30s` |
| Newline ends a statement | Newlines are whitespace. A line starting with `-`, `(` or `[` continues the previous expression, so `var b = a` then `-1` sets `b` to `a - 1` *(lint silent)* |
| `if (x) return` then more code | Bare `return`/`break` read the next statement as their value. Write `if (x) { return }` |
| `;` and `/* */` | Both are lexer errors |
| `a ? b : c` | `if (a) b else c`. Both branches are bare expressions; `else` is required |
| Block bodies in `match` arms | Arms hold one expression. Call a tool method for anything longer |
| Trailing comma in a call | Parse error in calls. Fine in arrays and objects |
| `len(x)`, `.length`, `print` | `x.size()`, `log(...)` |
| `"x" + null` | Raises. Use `"x ${value}"`, which renders `null` |
| `Status.Draft == "Draft"` | Always false: enums are tagged values. Compare to `Status.Draft` |
| `str`, `Array<T>`, unions | Types are `string`, `number`, `bool`, `any`, `T[]`, `{a: T, b?: T}`, aliases, enums. No unions |

## Expressions

Each assertion below runs, so read them as the spec.

```mh
enum Status { Draft, Published }

test expressions {
    describe optional_access_and_defaults {
        var config = {retries: 0}
        are_equal(config?.retries ?? 3, 0)              // ?? replaces only null
        are_equal(config?.missing?.deep ?? "none", "none")
        are_equal(config?.["dyn" + "amic"] ?? 1, 1)     // dynamic key
    }

    describe interpolation_takes_any_expression {
        var items = ["a", "b"]
        are_equal("${items.size()} of ${items.join(", ")}", "2 of a, b")
    }

    describe values_are_copied {
        var a = {x: 1}
        var b = a
        b["x"] = 2
        are_equal(a.x, 1)
    }

    describe with_copies_and_overrides {
        var draft = {title: "t", notes: []}
        var next = draft with { notes: ["n"] }
        are_equal(draft.notes, [])
        are_equal(next.title, "t")
    }

    describe ref_objects_are_shared {
        var counter = ref { n: 0 }
        var alias = counter
        alias["n"] = 1
        are_equal(counter.n, 1)
    }

    describe match_and_if_are_expressions {
        var label = match Status.Draft {
            Status.Draft     -> "hold"
            Status.Published -> "ship"
        }
        are_equal(label, "hold")
        are_equal(if (label == "hold") 1 else 2, 1)
    }

    describe array_pipelines {
        var ready = [{id: 2, ok: true}, {id: 1, ok: true}, {id: 3, ok: false}]
            .filter((item) -> item.ok)
            .sort_by((item) -> item.id)
            .map((item) -> item.id)
        are_equal(ready, [1, 2])
        are_equal([10, 20, 30, 40][1..^1], [20, 30])
    }

    describe destructuring_and_shorthand {
        var review = {data: 1, note: "n"}
        var {data, note: comment} = review
        are_equal({data, comment}, {data: 1, comment: "n"})
    }

    describe iterate_with_index_or_keys {
        var seen = []
        for (var pair in ["a", "b"].enumerate()) {
            seen += ["${pair.index}=${pair.value}"]
        }
        for (var key in {z: 1}.keys()) { seen += [key] }
        are_equal(seen, ["0=a", "1=b", "z"])
    }

    describe enums_round_trip_through_text {
        are_equal(Status.parse("Draft"), Status.Draft)
        are_equal("${Status.Published}", "Published")
    }
}
```

Multi-line strings use `"""`; they are trimmed and keep their inner indentation.
Lambdas are `(x) -> expr` or `(x) -> { ...; return v }`. Strings interpolate `${...}`
with unescaped nested quotes.

## Types

```mh
type Finding = { file: string, message: string, line?: number }
type Findings = Finding[]

tool Report {
    count(items: Findings): number -> items.size()
}

pipeline Audit {
    input findings: Findings = []
    step Show { log(Report.count(findings)) }
}
```

An optional field (`line?`) reads as `null` when absent, so its default lives at the use
site: `item.line ?? 0`. `prompt` is also a parameter type for a prompt that takes another
prompt's rendered text.

Enums, type aliases and typed inputs are checked at the boundary, and a typed input with
a declared enum advertises its variants to MCP callers. Prefer an enum over a free-form
`string` whenever the value set is closed.

## Control flow

`if`/`else`, `while (cond)`, `for (var x in array)` and `try`/`catch (e)`/`finally` take a
braced block or one bare statement. `for` walks arrays only. `while` stops the run with an
error after 10000 iterations.

| Exit | Effect |
|---|---|
| `return value` | Leaves the method, or the step (the run continues at the next step) |
| `fail("msg")` | Raises. Catchable; uncaught it fails the run with a non-zero exit |
| `break "reason"` | Clean stop of the whole run, exit 0, state kept |
| `complete()` | Finishes the run as completed, here |
| `pause("reason")` | Suspends for a human; resume re-enters this step |
| `goto Step` | `workflow` only. Jumps to another step |

`catch` handles failures only. `break`, `pause`, `complete` and `goto` pass straight
through it, and `finally` still runs. Keep the last four in step bodies: they belong to
the run, not to a tool method.

`catch (e)` needs the binding to use `e`; render it with `"${e}"`.

## Modules

```mh-fragment
// shared.mh
export agent Echo { command: "echo" }
export prompt Hello(name: string) { "Hello ${name}" }

// main.mh
import {Echo, Hello as Hi} from "shared.mh"
```

Named imports need `export` and resolve relative to the importing file. Imports merge
into one flat namespace, transitively, so keep declaration names unique across the
program. The bare `import "file.mh"` form merges everything, exported or not; it exists
for `partial` workflow fragments.
