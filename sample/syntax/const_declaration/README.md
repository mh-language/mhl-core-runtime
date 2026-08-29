# const_declaration

`const x = expression` is a single-assignment binding. It reads exactly like a
`var`, but reassigning it — `x = …` or `x += …` — is an error at both `mhl run`
and `mhl lint`. Re-running the `const` *declaration* (for example on a later
iteration of a loop body) simply rebinds; only assignment to the name is
forbidden. `const` is also valid at pipeline top level, where its value is shared
read-only across every step.

- [const_holds_a_single_value.mh](const_holds_a_single_value.mh) — declare, read,
  use in expressions; a `const` inside a `while` body rebinds each iteration
- [reassigning_a_const_is_rejected.mh](reassigning_a_const_is_rejected.mh) —
  `K = 2` on a const raises `cannot assign to constant`
- [const_plus_equals_is_rejected.mh](const_plus_equals_is_rejected.mh) — the same
  for the compound `+=` form
