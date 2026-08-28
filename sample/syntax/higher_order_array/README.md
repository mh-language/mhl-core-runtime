# higher_order_array

Array methods that take a lambda: `.filter()`, `.find()`, `.sort_by()`, `.map()`,
`.reduce()`, `.any()`, `.all()`.

- [filter.mh](filter.mh) — keep the elements a predicate accepts
- [filter_no_match_returns_empty_array.mh](filter_no_match_returns_empty_array.mh) — no
  matches yields an empty array
- [filter_does_not_mutate_the_source_array.mh](filter_does_not_mutate_the_source_array.mh) —
  the source array is left unchanged
- [filter_predicate_must_return_a_boolean.mh](filter_predicate_must_return_a_boolean.mh) — a
  predicate that returns a non-boolean raises a catchable error
- [find.mh](find.mh) — the first element a predicate accepts
- [find_no_match_returns_null.mh](find_no_match_returns_null.mh) — no match returns `null`
- [sort_by_number.mh](sort_by_number.mh) — sorting objects by a numeric key
- [sort_by_string.mh](sort_by_string.mh) — sorting by a string key
- [sort_by_does_not_mutate_the_source_array.mh](sort_by_does_not_mutate_the_source_array.mh)
  — `.sort_by()` returns a new array
- [sort_by_key_must_be_number_or_string.mh](sort_by_key_must_be_number_or_string.mh) — a key
  function returning any other type raises a catchable error
- [deterministic_feature_selection.mh](deterministic_feature_selection.mh) — chaining
  `.filter().filter().sort_by()` to pick the next ready item, deterministically
- [map.mh](map.mh) — transform every element into a new array
- [reduce.mh](reduce.mh) — fold an array left-to-right from an initial value
- [reduce_requires_an_initial_value.mh](reduce_requires_an_initial_value.mh) — `.reduce()`
  called with only the lambda raises a catchable error
- [any_and_all.mh](any_and_all.mh) — `.any()` / `.all()` existential and universal checks,
  including their empty-array edge cases
