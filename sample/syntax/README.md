# syntax

Examples of mhl's expression and statement syntax — the parts of the language you'd use
inside any `test`, `tool` method body, or pipeline `step`, independent of any specific
feature (agents, memory, prompts, ...). Every example is a self-verifying `mhl test` file:
run it with `mhl test sample/syntax/<topic>/<file>.mh`, or a whole topic with
`mhl test sample/syntax/<topic>`.

- [array_statements/](array_statements/README.md) — array literals, indexing, mutation,
  concatenation, slicing
- [concatenation/](concatenation/README.md) — string `+` and `${...}` interpolation
- [conditional_statements/](conditional_statements/README.md) — `if`/`else` as expression and
  as statement
- [conditional_symbols/](conditional_symbols/README.md) — comparison operators
- [higher_order_array/](higher_order_array/README.md) — `.filter()`, `.find()`, `.sort_by()`
- [logical_operations/](logical_operations/README.md) — `&&` and `||`
- [looping_constructs/](looping_constructs/README.md) — `for (var x in items) { ... }`
- [math_operations/](math_operations/README.md) — arithmetic on the `number` type
- [object_property_access/](object_property_access/README.md) — object literals, `.key` and
  dynamic `[key]` access
- [string_methods/](string_methods/README.md) — built-in string methods
