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
- [chained_string_methods.mh](chained_string_methods.mh) — chaining
  `.trim().to_lower().replace()` in one expression
