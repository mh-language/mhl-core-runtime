# type_alias

`type Name = <TypeExpr>` binds a name to a type expression so a repeated or
verbose shape can be written once. An alias is purely a spelling — it resolves to
the same type its target would produce inline, adds no distinct type, and works
anywhere a `: Type` annotation is accepted (pipeline inputs, tool-method params
and returns). Aliases may reference other aliases; a cycle, a duplicate name, an
unknown target, or shadowing a builtin keyword is a static error reported by
`mhl lint`.

- [alias_resolves_to_its_target.mh](alias_resolves_to_its_target.mh) — `type Slug
  = string` on a tool-method param; the wrong type is rejected with the resolved
  target (`string`) in the message
- [alias_to_an_object_shape.mh](alias_to_an_object_shape.mh) — `type Point = { x:
  number, y: number }` as a return type; a value missing a field fails the check
