# array_collection_ops

Collection operations that don't take a lambda: building arrays up immutably, rendering
them, de-duplicating, and comparing by value. (Lambda-taking transforms — `.map()`,
`.reduce()`, `.any()`, `.all()` — live in [../higher_order_array/](../higher_order_array/README.md).)

- [append_returns_a_new_array.mh](append_returns_a_new_array.mh) — `.append(x)` yields a new
  array; the receiver is untouched
- [append_statement_grows_the_variable.mh](append_statement_grows_the_variable.mh) — a bare
  `xs.append(x)` statement means `xs = xs.append(x)` (a step var, `self.xs`, `grid[i]`);
  used as a value, `.append` still leaves its receiver alone
- [join_formats_each_element.mh](join_formats_each_element.mh) — `.join(sep)` to a string,
  non-string elements formatted the way `log` prints them
- [join_separator_can_be_an_operator_token.mh](join_separator_can_be_an_operator_token.mh) —
  a string literal that is exactly an operator (`"-"`, `"!"`, `"=="`) stays a string, never
  parsed as the operator
- [unique_keeps_first_occurrence.mh](unique_keeps_first_occurrence.mh) — `.unique()` removes
  deep-equal duplicates, order preserved
- [equals_is_a_deep_comparison.mh](equals_is_a_deep_comparison.mh) — `.equals()` /
  `.deep_equal()` for a type- and order-sensitive value comparison of any two values
- [enumerate_pairs_index_with_value.mh](enumerate_pairs_index_with_value.mh) — `.enumerate()`
  pairs each element with its zero-based index, composing with a plain `for`-in loop
