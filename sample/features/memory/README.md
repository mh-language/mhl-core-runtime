# memory

Declaring and using `memory` stores: a `json`-backed key/value store (`SessionJsonMem`) and a
write-only `append_log` (`AuditLogMem`). Shared declarations live in
[fixtures/memory.mh](fixtures/memory.mh).

- [memory_session_mem.mh](memory_session_mem.mh) — `.set(key, value)` / `.get(key)`
- [memory_session_mem_with_non_existent_key.mh](memory_session_mem_with_non_existent_key.mh)
  — reading a missing key returns `null`
- [memory_session_mem_with_overwrite.mh](memory_session_mem_with_overwrite.mh) — `.set()`
  overwrites an existing key
- [memory_session_mem_add_object.mh](memory_session_mem_add_object.mh) — `.set()` with a
  whole object
- [memory_session_mem_add_object_with_overwrite.mh](memory_session_mem_add_object_with_overwrite.mh)
  — merging a partial object into existing keys
- [memory_session_mem_with_delete.mh](memory_session_mem_with_delete.mh) — `.remove(key)`
- [memory_session_mem_with_nested_object.mh](memory_session_mem_with_nested_object.mh) —
  storing and reading a nested object
- [memory_session_mem_with_nested_delete.mh](memory_session_mem_with_nested_delete.mh) — the
  `"parent::child"` path syntax for removing a nested key
- [memory_session_mem_with_array.mh](memory_session_mem_with_array.mh) — storing and indexing
  an array value
- [memory_AuditLogMem_append_and_read.mh](memory_AuditLogMem_append_and_read.mh) — an
  `append_log` memory only exposes `.append()`; reading it back is a plain `fs.read()`
- [memory_session_mem_stringify_roundtrips_through_a_file.mh](memory_session_mem_stringify_roundtrips_through_a_file.mh)
  — `json.stringify()` + `fs.write()` + `fs.read()` + `json.parse()` round-trip
- [memory_session_mem_value_survives_cmd_exec_argv.mh](memory_session_mem_value_survives_cmd_exec_argv.mh)
  — a memory value with spaces passed safely through `cmd.exec()`'s argv array
- [memory_session_mem_value_appended_as_a_progress_line.mh](memory_session_mem_value_appended_as_a_progress_line.mh)
  — `fs.append()` building a progress log line by line
