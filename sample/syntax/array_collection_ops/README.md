# array_collection_ops

Collection operations that don't take a lambda: building arrays up immutably, rendering
them, de-duplicating, and comparing by value. (Lambda-taking transforms — `.map()`,
`.reduce()`, `.any()`, `.all()` — live in [../higher_order_array/](../higher_order_array/README.md).)

- [append_returns_a_new_array.mh](append_returns_a_new_array.mh) — `.append(x)` yields a new
  array; the receiver is untouched
- [join_formats_each_element.mh](join_formats_each_element.mh) — `.join(sep)` to a string,
  non-string elements formatted the way `log` prints them
- [unique_keeps_first_occurrence.mh](unique_keeps_first_occurrence.mh) — `.unique()` removes
  deep-equal duplicates, order preserved
- [equals_is_a_deep_comparison.mh](equals_is_a_deep_comparison.mh) — `.equals()` /
  `.deep_equal()` for a type- and order-sensitive value comparison of any two values
