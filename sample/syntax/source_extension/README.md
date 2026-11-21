# source_extension

An mhl source file ends in `.mh` — the canonical extension, the one `mhl init` and the
studio write — or in `.mhl`, an accepted alias. The two are interchangeable everywhere:
`mhl run`/`test`/`lint`, a directory scan (`mhl test <dir>`, `mhl serve <dir>`), `import`,
the LSP and the editors. An `import` names the file with its extension, so `"x.mh"` and
`"x.mhl"` are two different files.

- [mh_imports_a_mhl_file.mh](mh_imports_a_mhl_file.mh) — a `.mh` file imports
  [lib/greetings.mhl](lib/greetings.mhl)
- [written_as_mhl.mhl](written_as_mhl.mhl) — a `.mhl` test file, found by a directory scan,
  importing a `.mh` and a `.mhl` library
