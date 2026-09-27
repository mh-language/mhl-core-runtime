# value_vs_ref

mhl has two kinds of structure:

- **Values** — every `{ ... }` object and `[ ... ]` array. Assigning one, passing
  it to a lambda/tool, or nesting it inside another object hands over an
  independent copy, so two names never share a structure that one of them
  changes.
- **Reference objects** — `ref { ... }` (or `ref (expr)`, from a copy of any
  object). Every name holding one sees every change; `==` compares identity. Its
  identity survives a checkpoint: after `--resume` / `run/resume`, names that
  shared a reference object before the pause share it again, so a resumed run
  behaves exactly like one that never stopped.

A reference object never leaves the interpreter as one: JSON, native ops, `mem`,
`memory` and a run's result all see a plain object.

`obj with { field: value, ... }` copies obj and overrides the listed fields on the
copy, leaving obj itself untouched — copies the same way an ordinary object literal
does, so a ref-typed field keeps its shared identity while every other field is
deep-copied.

- [objects_are_values.mh](objects_are_values.mh) — assignment, nested parts and
  lambda arguments copy
- [ref_objects_are_shared.mh](ref_objects_are_shared.mh) — sharing, identity
  equality, `ref (expr)`, plain JSON on the way out
- [with_copies_and_overrides_fields.mh](with_copies_and_overrides_fields.mh) —
  copy-with-override, chained `with`, a ref-typed field's identity surviving the
  copy, the non-object error, `with` staying usable as a plain identifier
