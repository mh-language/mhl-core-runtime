---
name: mhl-agents
description: Declare and call LLM backends in mhl. Use for agent, router, prompt, skill and schema declarations, before and after hooks, structured output, and retry, cache, rate_limit or fallback configuration.
---

# mhl agents

Assumes the `mhl-language` skill. An `agent` is a **one-shot call**: a CLI subprocess
(`cli/*`) or an Ollama request (`ollama/*`) that takes a prompt and returns a string. It
cannot call a tool mid-generation on mhl's behalf. So the program does the deterministic
work around the model: it fetches the data, builds the prompt, and validates the answer.

## Declaring an agent

The runtime reads exactly: `engine`, `command`, `args`, `stdin`, `endpoint`, `temperature`,
`log`, `trace`, `retry`, `cache`, `rate_limit`, `fallback`, `skills`, `system_prompt`,
`before`, `after`, `description`. Anything else is a lint error, including `timeout` (use
a step's `timeout` clause) and `api_key` (let the CLI read its own environment).

```mh
agent Claude {
    engine: "cli/claude-code"
    command: env("MODEL_CMD", "claude")     // swappable per environment
    args: ["-p", "${prompt}", "--output-format", "text"]

    retry: {
        max_attempts: 3
        delay: 2s
        retry_on: [500, 503, "timeout", "rate_limit"]
    }
    cache: { ttl: 10m, storage: "disk" }
    rate_limit: { requests_per_minute: 20, concurrency: 2 }
}

agent Local {
    engine: "ollama/qwen2.5-coder"
    endpoint: "http://localhost:11434"
    temperature: 0.1
    fallback: [Claude]
}
```

**Placeholders.** `"${prompt}"` and `"${schema}"` are recognized only as a **whole**
`args:` element. `"pre ${prompt}"` is a lint error. Without either placeholder, the prompt
is appended as the last argument. An `args:` element may also be an expression
(`env("FLAG", "x")`).

**Large prompts** go through `stdin:` instead of `args:`, where placeholders are
substituted anywhere in the string. Whole documents in argv hit the ~32 KB command-line
limit on Windows. Some CLIs treat piped text that reads like an instruction as prompt
injection, so keep stdin content phrased as data and the instruction in `args:`.

```mh
agent Summarizer {
    command: "sh"
    args: ["-c", "cat"]              // stands in for: claude -p "Summarize stdin"
    stdin: "${prompt}"
}

test summarizer {
    describe prompt_arrives_on_stdin {
        are_equal(Summarizer.run(prompt: "a long document"), "a long document")
    }
}
```

A failed call retries, then tries each `fallback` in order, then raises a catchable error.
`retry.backoff`, `cache.strategy` and `rate_limit.on_exceeded` accept one value each
(`"exponential"`, `"exact"`, `"queue"`), so leave them out.

Give any agent call that a resume may repeat a `cache`.

## Feeding context: before and after

Fetch data in a `before` hook rather than asking the model to go get it. The hook runs
real calls through mhl, and its returned fields become `${...}` bindings in the prompt.

```mh
tool Repo {
    diff(): string -> "- old\n+ new"
}

agent Reviewer {
    command: "echo"
    args: ["${prompt}"]

    before: () -> {
        return { diff: Repo.diff() }
    }

    // Runs once on the final response, bound as `result`.
    // Return a string to replace it, or nothing to pass it through.
    after: () -> {
        return result.trim()
    }
}

test reviewer {
    describe before_fields_reach_the_prompt {
        is_true(Reviewer.run(prompt: "Review: ${diff}").contains("+ new"))
    }
}
```

Both hooks take zero parameters. Assignments inside them never reach the caller, so the
return value (or a tool, extension or `mem` effect) is the only way out. `after` must
return a string or nothing.

## Prompts

Declare a `prompt` for any text you reuse or test. It is a typed template, so a missing
or unknown argument fails instead of rendering a broken prompt.

```mh
prompt Role() {
    "You are a security reviewer."
}

prompt Review(role: prompt, file: string, lang: string = "en") {
    """
    ${role}
    Review ${file}. Answer in ${lang}.
    """
}

test prompts {
    describe composition {
        var text = Review(role: Role(), file: "auth.go")
        is_true(text.starts_with("You are a security reviewer."))
        is_true(text.contains("Answer in en."))
    }
}
```

- Call with named arguments. Parameter defaults must be strings. Lint rejects an omitted
  default inside `x = Agent.run(prompt: P(...))` even though the run works, so pass every
  argument explicitly at that call site.
- Move long prompts to Markdown: `prompt Review(file: string) from "review.prompt.md"`,
  resolved relative to the declaring file. Write `\${NAME}` for a literal placeholder.
- Keep caller-supplied text clearly delimited as data inside the prompt, separate from the
  instruction.

## Structured output

`Agent.run` always returns a string. Pair a schema with `json.parse`, then check the
result before trusting it.

```mh
agent Extractor {
    engine: "cli/claude-code"
    command: "claude"
    args: ["-p", "${prompt}", "--output-format", "json", "--json-schema", "${schema}"]
}

tool Parse {
    // Each CLI wraps its answer differently; test this against a captured sample.
    finding(raw: string): any {
        var envelope = {}
        try {
            envelope = json.parse(raw)
        } catch (e) {
            fail("model returned invalid JSON: ${e}")
        }
        var finding = envelope?.structured_output
        if (finding == null) fail("no structured_output in: ${raw}")
        return finding
    }
}

pipeline Extract {
    input text: string
    var finding = null

    step Ask timeout 2m {
        var shape = json.stringify({type: "object", properties: {severity: {type: "string"}}})
        var raw = Extractor.run(prompt: "Extract the finding: ${text}", schema: shape)
        self.finding = Parse.finding(raw)
    }
}

test parse {
    describe unwraps_the_envelope {
        var raw = "{\"structured_output\":{\"severity\":\"high\"}}"
        are_equal(Parse.finding(raw).severity, "high")
    }

    describe rejects_prose {
        var message = ""
        try {
            Parse.finding("Sure! Here is the JSON")
        } catch (e) {
            message = "${e}"
        }
        is_true(message.contains("invalid JSON"))
    }
}
```

For a schema that lives in a file, `schema Finding from "finding.schema.json"` gives
`{content, path}`: pass `Finding.content` to CLIs that take inline JSON
(`claude --json-schema`) and `Finding.path` to those that take a file
(`codex --output-schema`). Lint validates the file, and editing it invalidates checkpoints.

## Routers

A router picks one of several agents. Make the deterministic path do as much as it can,
and add an LLM `decider` only for what is left.

```mh
agent Billing {
    command: "echo"
    args: ["billing:", "${prompt}"]
    description: "Invoices, charges and refunds"
}

agent Support {
    command: "echo"
    args: ["support:", "${prompt}"]
    description: "Account access and technical issues"
}

agent Judge {
    command: "sh"
    args: ["-c", "printf Support"]       // a real LLM in production
}

router Frontdesk {
    agents: [Billing, Support]
    select: (prompt) -> {
        if (prompt.contains("invoice")) { return nameof(Billing) }
        return null          // null cascades to the decider
    }
    decider: Judge
}

test frontdesk {
    describe keyword_skips_the_llm {
        are_equal(Frontdesk.select(prompt: "my invoice"), "Billing")
        are_equal(Frontdesk.delegate(prompt: "my invoice"), "billing: my invoice")
    }

    describe unknown_prompt_reaches_the_decider {
        are_equal(Frontdesk.delegate(prompt: "cannot log in"), "support: cannot log in")
    }
}
```

Return `nameof(Billing)`, not `"Billing"`, so lint catches a typo. `.select()` runs only
the hook and never the agent. An agent's `description` feeds the decider, so write it for
that reader. A router without `decider` needs an exhaustive `select`. Use `spawn` on agents
only; a router call cannot be spawned.

## Skills

A `skill` is a Markdown instruction package with `name` and `description` frontmatter
(flat keys only). It is never executed.

```mh-fragment
skill CodeReview from "./skills/code-review/SKILL.md"

agent Reviewer {
    command: "claude"
    args: ["-p", "${prompt}"]
    skills: [CodeReview]                 // allow-list
    system_prompt: (skills, prompt) -> {
        var text = ""
        for (var s in skills) { text += "## ${s.frontmatter.name}\n${s.content}\n\n" }
        return text + prompt
    }
}

// In a step:
Reviewer.run(skills: [CodeReview], prompt: "Review this diff.")
```

`system_prompt` is the only path from a selected skill to the request, and it runs only
when the call passes `skills:`. There is no `${skills}` placeholder.

## Security

Keep secrets out of `args`, `stdin` and prompts; let the agent CLI read its own
environment. `env("KEY")` inside `command`/`args` fails closed when unset and is redacted;
`env("KEY", "default")` opts out of both, so reserve it for non-secrets.
