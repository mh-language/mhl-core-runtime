# string_methods

Built-in string methods, called with dot-method syntax.

- [split.mh](split.mh) — `.split(sep)`
- [split_on_missing_separator_returns_the_whole_string.mh](split_on_missing_separator_returns_the_whole_string.mh)
  — splitting on a separator that isn't present
- [replace.mh](replace.mh) — `.replace(old, new)`
- [contains_on_string.mh](contains_on_string.mh) — `.contains()` on a string
- [contains_on_array.mh](contains_on_array.mh) — `.contains()` on an array
- [starts_with_and_ends_with.mh](starts_with_and_ends_with.mh) — `.starts_with()` /
  `.ends_with()`
- [trim.mh](trim.mh) — `.trim()`
- [to_upper_and_to_lower.mh](to_upper_and_to_lower.mh) — `.to_upper()` / `.to_lower()`
- [substring.mh](substring.mh) — `.substring(start, end)`
- [substring_out_of_range_errors.mh](substring_out_of_range_errors.mh) — an out-of-range
  substring raises a catchable error
- [remove.mh](remove.mh) — `.remove(from, to)`, the index-based complement of `substring`
- [remove_out_of_range_errors.mh](remove_out_of_range_errors.mh) — an out-of-range `remove`
  raises a catchable error
- [remove_content.mh](remove_content.mh) — `.remove_content(from, to)`, removing everything
  from a start marker through the end of an end marker
- [remove_content_missing_marker_errors.mh](remove_content_missing_marker_errors.mh) — a
  marker that isn't found raises a catchable error
- [extract_content.mh](extract_content.mh) — `.extract_content(from, to)`, keeping only
  the span from a start marker through the end of an end marker (the complement of
  `remove_content`)
- [extract_content_missing_marker_errors.mh](extract_content_missing_marker_errors.mh) — a
  marker that isn't found raises a catchable error
- [chained_string_methods.mh](chained_string_methods.mh) — chaining
  `.trim().to_lower().replace()` in one expression
