# type_introspection

Runtime type checks for dynamic data: the bare `type_of(x)` builtin and its per-kind
`is_string` / `is_number` / `is_bool` / `is_array` / `is_object` / `is_null` predicates.

- [type_of.mh](type_of.mh) — `type_of(x)` returns the kind as a string
- [is_checks.mh](is_checks.mh) — each `is_<kind>(x)` predicate, equivalent to
  `type_of(x) == "<kind>"`
- [guarding_dynamic_data.mh](guarding_dynamic_data.mh) — combining `?.` and `is_array` to
  iterate a field only when it really is an array
