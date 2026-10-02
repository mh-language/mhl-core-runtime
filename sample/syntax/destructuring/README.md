# destructuring

Binding several fields of one object in a single statement. The right-hand side
is evaluated once; each entry `field` binds the field of the same name, and
`field: name` binds it to another name. Each form is exactly equivalent to one
`var` / assignment per field, so a `const` or an input target is rejected the
same way.

- [var_destructuring.mh](var_destructuring.mh) — `var {data, revisao: note} = r`
  declares locals; a missing field is an error
- [assign_destructuring.mh](assign_destructuring.mh) — `{a, b} = r` assigns
  existing names, `self.{tokens_in, tokens_out} = r` assigns pipeline vars

Inside a brace-less `if`, write `if (c) { {a} = r }` — a bare `{a}` right after
`if (c)` is read as the `if`'s block.
