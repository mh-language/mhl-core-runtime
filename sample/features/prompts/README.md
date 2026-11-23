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

### Variants per harness/model

A `prompt` may declare one `default` body plus `for "<target>"` arms; the agent that receives
the prompt picks one with `prompt.target(...)` in its own declaration, so call sites never
change. Shared declarations live in [fixtures/variants.mh](fixtures/variants.mh).

- [prompt_variant_selected_by_agent_target.mh](prompt_variant_selected_by_agent_target.mh) —
  one call site, three agents: `stdin: prompt.target("claude/" + model)`,
  `stdin: "${prompt.target(model)}"`, and the classic `"${prompt}"` (always the default)
- [prompt_variant_target_from_input.mh](prompt_variant_target_from_input.mh) — the target
  chosen per call through an agent input (a model picked at run time); an unmatched target
  falls back to `default`
- [prompt_variant_survives_tool_string_param.mh](prompt_variant_survives_tool_string_param.mh)
  — a rendered prompt is still a string, so it crosses a tool parameter typed `string` and the
  agent at the end still picks its variant
- [prompt_variant_string_ops_use_default.mh](prompt_variant_string_ops_use_default.mh) —
  concatenation and other string operations work on the default text; `.target()` on any
  other string returns it unchanged
- [prompt_variant_nested_follows_target.mh](prompt_variant_nested_follows_target.mh) — a
  variant prompt nested in another prompt follows the same target
- [prompt_variant_fallback_picks_own_variant.mh](prompt_variant_fallback_picks_own_variant.mh)
  — each fallback leg picks the variant for its own harness
- [prompt_variant_loaded_from_markdown.mh](prompt_variant_loaded_from_markdown.mh) — arms
  loaded `from` Markdown files ([fixtures/review.prompt.md](fixtures/review.prompt.md),
  [fixtures/review.claude.prompt.md](fixtures/review.claude.prompt.md)), frontmatter stripped
