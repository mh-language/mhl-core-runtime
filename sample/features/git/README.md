# git

The `git` native namespace runs `git` in the interpreter's working directory — or in a
`dir:` argument when given (every `git.*` op accepts one; it maps to `git -C <dir>`). The
read ops (`status`, `diff`, `log`, `rev_parse`) are what a pipeline uses to inspect repo
state; `status` returns the same inspectable `{stdout, stderr, exit_code}` shape as
`cmd.exec`, while `diff`/`log`/`rev_parse` return plain text and fail closed on a bad ref.
The write ops (`add`, `commit`) mutate the tree, so the examples here exercise only their
argument-validation paths — no sample run stages or commits anything.

- [git_read_ops_report_repo_state.mh](git_read_ops_report_repo_state.mh) — `git.status()`
  returns the `cmd.exec` result shape; `git.rev_parse("HEAD")` resolves to a short SHA;
  `git.log(3)` lists recent commits as `--oneline` text
- [git_diff_text_round_trips_through_memory.mh](git_diff_text_round_trips_through_memory.mh)
  — `git.diff("HEAD")` captured once and stashed in a `memory` store, the pattern
  language-design.md §8 uses for `session_mem.set("current_diff", diff)`
- [git_write_ops_validate_their_arguments.mh](git_write_ops_validate_their_arguments.mh) —
  `git.add([])`, `git.commit("   ")`, `git.rev_parse("")` and `git.log(0)` each raise
  before any `git` subprocess runs
- [git_ops_accept_a_dir_argument.mh](git_ops_accept_a_dir_argument.mh) — the optional named
  `dir:` argument (`git -C <dir>`): targeting a directory inside the repo, and a non-repo
  directory surfacing as a non-zero `exit_code`
