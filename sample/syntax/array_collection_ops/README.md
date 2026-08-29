# array_collection_ops

Collection operations that don't take a lambda: building arrays up immutably, rendering
them, de-duplicating, and comparing by value. (Lambda-taking transforms — `.map()`,
`.reduce()`, `.any()`, `.all()` — live in [../higher_order_array/](../higher_order_array/README.md).)

- [append_returns_a_new_array.mh](append_returns_a_new_array.mh) — `.append(x)` yields a new
  array; the receiver is untouched
- [join_formats_each_element.mh](join_formats_each_element.mh) — `.join(sep)` to a string,
  non-string elements formatted the way `log` prints them
- [join_separator_can_be_an_operator_token.mh](join_separator_can_be_an_operator_token.mh) —
  a string literal that is exactly an operator (`"-"`, `"!"`, `"=="`) stays a string, never
  parsed as the operator
- [unique_keeps_first_occurrence.mh](unique_keeps_first_occurrence.mh) — `.unique()` removes
  deep-equal duplicates, order preserved
- [equals_is_a_deep_comparison.mh](equals_is_a_deep_comparison.mh) — `.equals()` /
  `.deep_equal()` for a type- and order-sensitive value comparison of any two values
