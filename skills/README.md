# mhl skills

Agent skills that teach AI models to write correct, idiomatic
[mhl](https://mh-language.github.io/mhl-core-runtime/Docs-Reference.dc.html). mhl is too new
for models to know it from training, and they reach for TypeScript habits that fail here,
often silently. Each skill is a folder with a `SKILL.md`, loaded when its description
matches the task.

| Skill | Teaches |
|---|---|
| [mhl-language](mhl-language/SKILL.md) | Syntax, types, scope, the verification loop, and the habits that break. Start here |
| [mhl-workflows](mhl-workflows/SKILL.md) | Pipelines and workflows: steps, branching, loops, concurrency, pause, resume |
| [mhl-agents](mhl-agents/SKILL.md) | Agents, prompts, routers, skills, structured output, resilience |
| [mhl-tools](mhl-tools/SKILL.md) | Tools, native ops, memory, MCP and A2A clients, credentials |
| [mhl-testing](mhl-testing/SKILL.md) | Hermetic tests, agent mocks, workflow assertions, reading failures |
| [mhl-serving](mhl-serving/SKILL.md) | `mhl serve`: workflows as MCP tools and A2A skills, hardening, `mhl pack` |

## Install

Copy the folders you want into your agent's skills directory. For Claude Code:

```sh
cp -R skills/mhl-* ~/.claude/skills/            # every project
cp -R skills/mhl-* /path/to/project/.claude/skills/   # one project
```

Install them together: they refer to each other by name. For an agent without skill
support, point its instructions file (such as `AGENTS.md`) at
`mhl-language/SKILL.md` and tell it to read the sibling skills as needed.

An mhl program can load them too, since each `SKILL.md` is valid input for
`skill MhlLanguage from "skills/mhl-language/SKILL.md"`.

## Keeping them true

Every ```` ```mh ```` block in these files is a complete program. Excerpts that cannot
stand alone are fenced as ```` ```mh-fragment ````.

```sh
cd src/mhl-runtime && make build && cd ../..
MHL=src/mhl-runtime/dist/mhl skills/check.sh
```

`check.sh` lints every block, runs the ones declaring a `test`, and loads every `SKILL.md`
through mhl's own `skill` declaration. Run it when the language changes, and keep
frontmatter to flat single-line keys so both agents and mhl can read it.
