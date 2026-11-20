# cmd

The `cmd` native namespace runs subprocesses: `cmd.exec(command)` and, concurrently,
`cmd.exec_all(commands)`. Each returns `{stdout, stderr, exit_code}` — a non-zero exit is a
value to check, not a raised error. Optional named arguments: `timeout:`, `encoding:` and
`cwd:`, the directory the command starts in (`cd cwd && command`; a missing or
non-directory `cwd` raises before anything runs).

- [cmd_exec_cwd_runs_in_another_directory.mh](cmd_exec_cwd_runs_in_another_directory.mh) —
  `cwd:` on `cmd.exec` (argv and string forms) and `cmd.exec_all`: the command sees that
  directory's files, relative argv paths resolve there, `""` inherits, a failing test
  command stays a value, and a bad `cwd` raises
