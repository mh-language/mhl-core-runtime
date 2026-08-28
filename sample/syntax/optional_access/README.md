# optional_access

The `?.` optional member operator and the `??` null-coalescing operator — together they
replace most of the defensive `has(...)` / `try`-`catch` scaffolding around dynamic data.

- [optional_member.mh](optional_member.mh) — `x?.field` reads like `x.field` when the field
  is there
- [optional_member_short_circuits_on_missing_field.mh](optional_member_short_circuits_on_missing_field.mh)
  — a field the object lacks yields `null` instead of raising
- [optional_member_short_circuits_on_null.mh](optional_member_short_circuits_on_null.mh) — a
  present-but-null hop collapses the rest of the chain to `null`
- [optional_chain_before_method_call.mh](optional_chain_before_method_call.mh) — `x?.field?.trim()`
  runs the method only when the receiver survived the chain
- [coalesce_supplies_a_default.mh](coalesce_supplies_a_default.mh) — `value ?? fallback` keeps
  `value` unless it is `null` (so `0` and `""` are kept)
- [coalesce_chains_left_to_right.mh](coalesce_chains_left_to_right.mh) — `a ?? b ?? c`, and
  `??` binding looser than every other operator
- [optional_member_on_non_object_raises.mh](optional_member_on_non_object_raises.mh) — `?.`
  guards null / missing field only; reaching into a non-object is still a catchable error
