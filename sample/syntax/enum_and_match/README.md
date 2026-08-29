# enum_and_match

`enum Name { A, B, C }` is a closed set of named constants. A value is produced
only by qualified access (`Name.A`) and is a distinct tagged value — not a string
(`Status.Draft == "Draft"` is `false`, `type_of` is `"enum"`), though it displays
as the bare variant in interpolation, `log`, and `json.stringify`.

`match subject { pattern -> body ... }` is an expression-position multi-way
branch. Arms are tried top to bottom; the first pattern deep-equal to the subject
wins, or `_`. A `match` over an `enum` or `bool` with no `_` must cover every
case (`mhl lint` flags a missing one); any other subject falls back to a runtime
error when nothing matches.

- [enum_value_access.mh](enum_value_access.mh) — `Status.Published`, `type_of` /
  `is_enum`, inequality with a string, display forms
- [match_over_an_enum.mh](match_over_an_enum.mh) — an exhaustive `match` returning
  a value per variant
- [match_literals_and_wildcard.mh](match_literals_and_wildcard.mh) — `match` on a
  number / string with literal arms and `_`
- [match_no_arm_matches_is_rejected.mh](match_no_arm_matches_is_rejected.mh) — the
  runtime error when nothing matches and there is no `_`
