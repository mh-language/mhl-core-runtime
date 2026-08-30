# extension_declaration

`extension <kind> <Name> { ...props }` declares a use of a runtime extension.
The core knows only the shape — kind, name, property bag; whichever extension is
registered for that `kind` validates the properties, declares the callable
methods, and executes them. `mcp` and `a2a` are built in; other kinds come from
an installed external extension (see [../../extensions/](../../extensions/README.md)).

- [extension_declares_a_kind_and_name.mh](extension_declares_a_kind_and_name.mh)
  — the declaration parses and lints with any kind, one or more per file.
  Executable behaviour of a resolved extension is covered by
  [../../features/mcp](../../features/mcp) and
  [../../features/a2a](../../features/a2a).
