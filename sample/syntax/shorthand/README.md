# shorthand

Two abbreviations for the common case where a field or argument has the same
name as the variable holding its value. Both are expanded by the parser, so
they evaluate, lint and hash (the checkpoint digest) exactly like the long form.
Only an identifier abbreviates — a keyword (`{return}`) or a string key
(`{"a"}`) does not.

- [object_field_shorthand.mh](object_field_shorthand.mh) — `{artifact, path}`
  is `{artifact: artifact, path: path}`
- [named_argument_shorthand.mh](named_argument_shorthand.mh) — `f(name:)` is
  `f(name: name)`
