# extension_declaration

`extension <kind> <Name> { ...props }` declares a use of a runtime extension.
The core knows only the shape — kind, name, property bag; whichever extension is
registered for that `kind` validates the properties, declares the callable
methods, and executes them. `mcp` and `a2a` are built in; other kinds come from
an installed external extension (see `internal/extension` and
`Docs-Extensions.dc.html`). An optional `from "<id>"` names which installed
extension serves the declaration, so several of one kind can be used side by
side.

- [extension_declares_a_kind_and_name.mh](extension_declares_a_kind_and_name.mh)
  — the declaration parses and lints with any kind, one or more per file.
  Executable behaviour of a resolved extension is covered by
  [../../features/mcp](../../features/mcp) and
  [../../features/a2a](../../features/a2a).
- [extension_from_names_the_provider.mh](extension_from_names_the_provider.mh)
  — `extension <kind> <Name> from "<id>"`: selects the extension by its
  manifest id (the built-in `mcp` adapter is `"mhl.mcp"`), and an id that is
  not installed, or that serves another kind, is a runtime error.
