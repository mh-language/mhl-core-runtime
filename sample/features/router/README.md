# router

Declaring a `router` and calling `.delegate(prompt: ...)`: a hybrid decision between a
deterministic `select` hook and an LLM decision call made through a `decider` — an ordinary,
separately declared `agent`, not properties duplicated onto the router — the `nameof(...)`
builtin that makes a `select` return typo-safe and IDE-navigable, and the errors raised for an
undeclared `agents:`/`decider:` entry or a `select` return matching no declared agent. Shared
declarations live in [fixtures/router.mh](fixtures/router.mh).

- [router_select_hook_resolves_deterministically.mh](router_select_hook_resolves_deterministically.mh)
  — `select: (prompt) -> {...}` runs first; a matching return picks the agent directly, with no
  LLM decision call
- [router_cascades_to_llm_decision_when_select_is_inconclusive.mh](router_cascades_to_llm_decision_when_select_is_inconclusive.mh)
  — an absent (`null`) `select` result falls back to a decision call run through `decider: ...`
  (an ordinary declared agent), which then runs the agent it chose
- [router_delegate_errors_when_agent_is_not_declared.mh](router_delegate_errors_when_agent_is_not_declared.mh)
  — an `agents: [...]` entry naming an undeclared agent fails `.delegate(...)` with a clear error
- [router_delegates_without_any_llm_decision_engine.mh](router_delegates_without_any_llm_decision_engine.mh)
  — the LLM cascade is entirely optional: a router with only `select` (no `decider` at all) is a
  purely deterministic router, as long as `select` is exhaustive
- [router_without_select_or_decision_engine_errors.mh](router_without_select_or_decision_engine_errors.mh)
  — a router declaring neither `select` nor a `decider` is rejected by `mhl lint`, and
  `.delegate()` also fails at runtime with a clear "no decider is configured" error
- [router_select_typo_errors_immediately.mh](router_select_typo_errors_immediately.mh)
  — a `select` return that is non-null but matches no declared agent is always a specific,
  immediate error (naming the bad value and the real agents) — never a silent cascade
- [router_select_uses_nameof_for_ide_navigation.mh](router_select_uses_nameof_for_ide_navigation.mh)
  — `return nameof(Billing)` instead of `return "Billing"`: the same result, but `mhl lint`
  catches a typo in the name statically, and an editor's "go to definition" follows it
