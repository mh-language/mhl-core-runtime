# looping_constructs

The `for (var x in items) { ... }` loop.

> **Note:** the original test suite's `test_looping_constructs.mh` was empty (a pre-existing
> gap — see `internal/lang/parser` `TestFixturesParse` for another example of this repo's known
> gaps). [for_in_loop.mh](for_in_loop.mh) below is authored fresh from the syntax verified in
> `internal/lang/parser/parser_test.go`, not migrated from an existing example.

- [for_in_loop.mh](for_in_loop.mh) — iterating an array with `for (var n in numbers) { ... }`
