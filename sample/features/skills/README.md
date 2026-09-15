# skills

Declaring a `skill` from a Markdown file with frontmatter, permitting a subset of an agent's
declared skills at the call site (`run(skills: [...])`), and composing them into the outgoing
request through an explicit `system_prompt: (skills, prompt) -> ...` hook. Shared declarations
live in [fixtures/agents.mh](fixtures/agents.mh) (the `CodeReview`/`SecurityReview` skills and
the `Reviewer` agent), backed by
[fixtures/code-review.md](fixtures/code-review.md)/[fixtures/security-review.md](fixtures/security-review.md).

- [skills_compose_system_prompt_from_selected_skills.mh](skills_compose_system_prompt_from_selected_skills.mh)
  — two skills selected, in order, reach `system_prompt` as a list of
  `{name, frontmatter, content}` objects; `name` is the MHL declaration identifier (what
  `nameof(CodeReview)` resolves to), distinct from `frontmatter.name`
- [skills_call_without_selection_leaves_prompt_unchanged.mh](skills_call_without_selection_leaves_prompt_unchanged.mh)
  — a call that never mentions `skills:` never invokes `system_prompt` at all, even on an
  agent that declares both `skills:` and `system_prompt`

`system_prompt` is required by `mhl lint` on any agent whose `skills:` allow-list is non-empty
— it is the only place composed skill instructions reach the outgoing request, so an agent that
declares `skills:` with no way to receive them is a lint error, not a silent no-op. Selecting a
skill the agent didn't permit, repeating one, or naming one that isn't declared all fail before
any model call, in both `mhl lint` and `mhl run`.
