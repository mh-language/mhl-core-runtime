# env

`.env` files: `KEY=VALUE` lines loaded into the environment before `mhl run` / `mhl test`
start, so `env("KEY")` — and every agent subprocess, extension and `cmd.exec` — sees API
keys and settings kept with the project instead of exported in a shell profile.

- **Default:** the working directory's `.env`, when it exists (mhl studio runs from the
  workspace root, so that is the workspace's `.env`).
- **`--env-file <path>`** (repeatable): load these files instead of `./.env`; each must
  exist, and a later file overrides an earlier one.
- **A variable already in the environment always wins** over a file.
- Format: `# comments`, an optional `export ` prefix, unquoted values (trimmed; ` #` starts
  a comment), `"double quotes"` with `\n \t \" \\` escapes, `'single quotes'` taken
  literally. No `${VAR}` expansion, no multi-line values. A malformed line fails the run
  with `file:line`.

Keep `.env` out of version control (`.gitignore` it); commit a `.env.example` instead.

- [env_file_values.mh](env_file_values.mh) — reads every value of
  [sample.env](sample.env) with `env()`: unquoted with a trailing comment, `export` +
  double quotes with an escape, single quotes kept literal, an empty value. Run with
  `mhl test sample/features/env --env-file sample/features/env/sample.env` (or
  `mhl run sample/features/env/env_file_values.mh --env-file sample/features/env/sample.env`
  for its `Greet` workflow).
