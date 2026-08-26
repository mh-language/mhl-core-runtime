# array_statements

Array literals, indexing, mutation, concatenation and slicing.

- [array_statements.mh](array_statements.mh) — literal arrays of numbers, strings and
  booleans; `.size()`, indexing and `.index_of()`
- [array_index_out_of_bounds.mh](array_index_out_of_bounds.mh) — reading past the end of an
  array raises a catchable error
- [array_index_of_not_found.mh](array_index_of_not_found.mh) — `.index_of()` returns `-1`
  when the value isn't present
- [array_concatenation.mh](array_concatenation.mh) — `+` concatenates two arrays
- [array_of_arrays.mh](array_of_arrays.mh) — nesting arrays inside arrays
- [array_with_mixed_types.mh](array_with_mixed_types.mh) — a single array holding numbers,
  strings and booleans together
- [set_array_element.mh](set_array_element.mh) — assigning to an existing index
- [set_array_element_out_of_bounds.mh](set_array_element_out_of_bounds.mh) — assigning past
  the end of an array raises a catchable error
- [slice_array.mh](slice_array.mh) — the `[a..b]` slice syntax, open-ended bounds, the `^`
  from-the-end operator, out-of-range clamping and slicing without mutation
- [array_slice_assignment_is_rejected.mh](array_slice_assignment_is_rejected.mh) — a slice
  expression cannot be used as an assignment target
