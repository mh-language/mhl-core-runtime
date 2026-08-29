# compound_assignment

The `+=` compound assignment operator — sugar for `x = x + expr` that reuses the binary
`+` operator's operand rules exactly (two numbers add, two strings concatenate, two arrays
combine into a fresh slice). The target is a declared `var` (or `mem`), optionally with
array-index trailers; `+=` never implicitly declares.

- [append_to_an_array.mh](append_to_an_array.mh) — `acc += ["msg"]` to accumulate into a
  list without the `acc = acc.append(...)` round-trip
- [add_to_a_number_or_string.mh](add_to_a_number_or_string.mh) — the same operator for
  `count += 1` and `msg += "suffix"`
- [index_target.mh](index_target.mh) — `grid[0] += [9]` reads the element, combines, and
  writes it back in place
- [mismatched_operands_are_rejected.mh](mismatched_operands_are_rejected.mh) — mixed-type
  operands raise the same catchable error as bare `+`
- [undeclared_target_is_rejected.mh](undeclared_target_is_rejected.mh) — `+=` on a name
  never declared with `var` is an error
