# prompts

Declaring a `prompt` template, rendering it by calling it like a function, nesting one prompt
inside another, and plain inline `${...}` interpolation as an alternative to a declared
template. Shared declarations live in [fixtures/prompts.mh](fixtures/prompts.mh) and
[fixtures/agents.mh](fixtures/agents.mh) (the `Echo` agent, used here only to make the
rendered prompt text observable via its response).

- [prompt_renders_declared_template.mh](prompt_renders_declared_template.mh) — calling a
  declared `prompt(...)` template with named arguments
- [prompt_inline_interpolated_string.mh](prompt_inline_interpolated_string.mh) — a plain
  `"${...}"` string used directly as a prompt, no declared template needed
- [prompt_nested_inside_another_prompt.mh](prompt_nested_inside_another_prompt.mh) — passing
  the result of one prompt as an argument to another
- [prompt_loaded_from_markdown_file.mh](prompt_loaded_from_markdown_file.mh) — `prompt
  X(...) from "path.md"`, loading the template body from
  [fixtures/greeting.prompt.md](fixtures/greeting.prompt.md) instead of an inline block, plus
  the `\${...}` escape for literal placeholders in file-sourced Markdown
- [prompt_default_parameter.mh](prompt_default_parameter.mh) — a `prompt` parameter with a
  default value (`lang: string = "en"`), filled in when the caller omits that named argument
