# fs

The `fs` / `dir` native namespaces — file and directory operations.

- [dir_clear_empties_a_directory.mh](dir_clear_empties_a_directory.mh) —
  `dir.clear(path)`: create-or-empty a directory, keeping it; its refusals
- [fs_encoding_transcodes_at_the_boundary.mh](fs_encoding_transcodes_at_the_boundary.mh) —
  the `encoding` argument of `fs.read`/`fs.write`/`fs.append`: Windows-1252 and
  UTF-16 round trips, `"auto"` detection, strict `"utf-8"`, and its errors
