# schemas

`schema Name from "file.schema.json"` declares a JSON Schema file. The name
evaluates to `{content: string, path: string}` — the file's JSON text and its
absolute path — the two shapes agent CLIs take a schema in. The file must be a
JSON object of at most 1 MiB; a missing or invalid file is a `mhl lint` error
and fails before `mhl run` reaches any step. The checkpoint digest covers the
schema's content, not its path.

- [schema_declaration.mh](schema_declaration.mh) — `Brief.content`,
  `Brief.path`, passing the value to a `{content, path}`-typed parameter
