# time

Datetime values as plain RFC3339 UTC strings — mhl has no dedicated datetime type, so
`time.*` reads and produces the same `string` shape `internal/features/memory/jsonlog.go`
already uses for its `"ts"` field.

- [time_now_returns_utc_rfc3339.mh](time_now_returns_utc_rfc3339.mh) — `time.now()`
- [time_parse_normalizes_custom_layout.mh](time_parse_normalizes_custom_layout.mh) —
  `time.parse(text, layout)` with a non-RFC3339 input layout
- [time_parse_invalid_text_errors.mh](time_parse_invalid_text_errors.mh) — unparseable
  input raises an error
- [time_format_with_custom_layout.mh](time_format_with_custom_layout.mh) —
  `time.format(value, layout)` with a raw Go reference-time layout
- [time_format_with_friendly_layout_tokens.mh](time_format_with_friendly_layout_tokens.mh)
  — the moment.js/ICU-style friendly tokens (`dd/MM/yyyy`, `HH:mm:ss`) as an alternative
  to Go's layout syntax
- [time_add_duration_literal.mh](time_add_duration_literal.mh) — `time.add(value,
  duration)` accepts a duration literal (`7d`) the same way `cmd.exec`'s `timeout:` does
- [time_diff_seconds_between_two_timestamps.mh](time_diff_seconds_between_two_timestamps.mh)
  — `time.diff(a, b)` returns the difference in seconds
- [time_compare_orders_datetime_strings.mh](time_compare_orders_datetime_strings.mh) —
  `time.compare(a, b)` returns -1/0/1, since mhl's `<`/`>` operators don't support strings
- [time_checkpoint_expiry_uses_add_and_compare.mh](time_checkpoint_expiry_uses_add_and_compare.mh)
  — composing `time.add` + `time.compare` to reason about staleness, illustrating the
  same idea `checkpoint { ttl: 7d }` applies internally
