package lsp

import "strings"

// This file is the LSP's hand-maintained catalogue of every built-in
// callable's signature — the data behind a completion item's `detail`/
// `documentation` and the whole `textDocument/signatureHelp` response.
//
// There is no single source these are generated from: they mirror, by hand,
//   - native ops   -> internal/engine/interpreter/tool.go   (nativeOpCall)
//   - collection   -> internal/engine/interpreter/eval.go   (callValueMethod)
//   - memory       -> internal/engine/interpreter/memory_ops.go
//   - mcp_server   -> internal/engine/interpreter/mcp_ops.go
//   - agent.run    -> internal/engine/interpreter/agent.go
//   - globals      -> internal/engine/interpreter/eval.go   (log/fail/env)
//   - assertions   -> internal/engine/interpreter/test.go   (runAssertion)
// and the same surface documented in docs/site/stdlib.html. signatures_test.go
// fails if the method *sets* here drift from the LSP's own symbol tables
// (nativeSymbols, stringMethods, ...), which in turn mirror the interpreter.

// sig is one callable's human-facing signature. Label is the full
// "name(params) -> ret" line shown in the UI; Params is the ordered
// parameter list, used by signature help to point at the active argument;
// Doc is a one-line Markdown explanation.
type sig struct {
	Label  string
	Params []string
	Doc    string
}

// --- native operations, keyed "namespace.method" -------------------------

var nativeSigs = map[string]sig{
	"cmd.exec": {
		Label:  "cmd.exec(command: string | string[], timeout?: duration) -> {stdout: string, stderr: string, exit_code: number}",
		Params: []string{"command", "timeout"},
		Doc:    "Runs a subprocess. A non-zero `exit_code` is returned, not raised. Prefer the argv-array form.",
	},
	"cmd.exec_all": {
		Label:  "cmd.exec_all(commands: (string | string[])[], timeout?: duration) -> {stdout, stderr, exit_code}[]",
		Params: []string{"commands", "timeout"},
		Doc:    "Runs each command concurrently; results come back in input order. `timeout` applies per command.",
	},
	"git.diff": {
		Label:  "git.diff(target?: string, dir?: string) -> string",
		Params: []string{"target", "dir"},
		Doc:    "`git [-C dir] diff [target]`. Plain text; a non-zero exit raises.",
	},
	"git.add": {
		Label:  "git.add(paths: string[], dir?: string) -> {stdout, stderr, exit_code}",
		Params: []string{"paths", "dir"},
		Doc:    "`git [-C dir] add -A -- <paths>`. At least one path is required.",
	},
	"git.commit": {
		Label:  "git.commit(message: string, paths?: string[], dir?: string) -> {stdout, stderr, exit_code}",
		Params: []string{"message", "paths", "dir"},
		Doc:    "`git [-C dir] commit -m message [-- paths]`. Message must be non-empty; it is passed as one argument, never shell-quoted.",
	},
	"git.status": {
		Label:  "git.status(paths?: string[], dir?: string) -> {stdout, stderr, exit_code}",
		Params: []string{"paths", "dir"},
		Doc:    "`git [-C dir] status --short`. A non-zero exit (e.g. `dir` is not a repo) is a value to inspect, not a raise.",
	},
	"git.rev_parse": {
		Label:  "git.rev_parse(ref: string, dir?: string) -> string",
		Params: []string{"ref", "dir"},
		Doc:    "`git [-C dir] rev-parse --short <ref>`, trimmed. A bad ref raises.",
	},
	"git.log": {
		Label:  "git.log(n: number, dir?: string) -> string",
		Params: []string{"n", "dir"},
		Doc:    "`git [-C dir] log -n <n> --oneline`. `n` must be positive.",
	},
	"fs.read":   {Label: "fs.read(path: string) -> string", Params: []string{"path"}, Doc: "Full file contents. Raises if unreadable."},
	"fs.exists": {Label: "fs.exists(path: string) -> bool", Params: []string{"path"}, Doc: "A stat error other than \"not found\" raises."},
	"fs.write":  {Label: "fs.write(path: string, content: string) -> bool", Params: []string{"path", "content"}, Doc: "Truncates and writes, creating parent directories. Returns `true`."},
	"fs.append": {Label: "fs.append(path: string, content: string) -> bool", Params: []string{"path", "content"}, Doc: "Appends, creating parent directories. Returns `true`."},
	"fs.delete": {Label: "fs.delete(path: string) -> bool", Params: []string{"path"}, Doc: "Removes a file or empty directory. Raises if it can't."},
	"fs.join":   {Label: "fs.join(...segments: string) -> string", Params: []string{"segments"}, Doc: "OS-appropriate path join of one or more segments."},
	"fs.list":   {Label: "fs.list(dir: string) -> string[]", Params: []string{"dir"}, Doc: "Entries in `dir`."},
	"http.get":     httpSig("get"),
	"http.post":    httpSig("post"),
	"http.put":     httpSig("put"),
	"http.patch":   httpSig("patch"),
	"http.delete":  httpSig("delete"),
	"http.head":    httpSig("head"),
	"http.options": httpSig("options"),
	"http.download": {
		Label: "http.download(url: string, path: string, headers?, query?, timeout?: duration, " +
			"raise_for_status?: bool, auth?, tls?, proxy?: string) -> {status: number, path: string, bytes: number, ok: bool, headers: {string: string}}",
		Params: []string{"url", "path", "headers", "query", "timeout", "raise_for_status", "auth", "tls", "proxy"},
		Doc: "Streams a GET response straight to `path` — atomic write, parent directories created — instead of returning the body. " +
			"On a non-2xx response no file is written and `ok` is false (unless `raise_for_status` is set). Shares the http option surface.",
	},
	"json.parse":       {Label: "json.parse(text: string) -> any", Params: []string{"text"}, Doc: "Parses a JSON document. Invalid JSON raises."},
	"json.parse_lines": {Label: "json.parse_lines(text: string) -> any[]", Params: []string{"text"}, Doc: "Parses JSON-lines (one value per line)."},
	"json.stringify":   {Label: "json.stringify(value: any) -> string", Params: []string{"value"}, Doc: "Serializes to JSON; object keys are emitted in sorted order."},
	"log.info":         {Label: "log.info(...values: any) -> null", Params: []string{"values"}, Doc: "Writes `[INFO] ` + the space-joined values."},
	"log.warn":         {Label: "log.warn(...values: any) -> null", Params: []string{"values"}, Doc: "Writes `[WARN] ` + the space-joined values."},
	"log.error":        {Label: "log.error(...values: any) -> null", Params: []string{"values"}, Doc: "Writes `[ERROR] ` + the space-joined values. Does not raise or change the exit code."},
	"time.now":         {Label: "time.now() -> string", Params: nil, Doc: "Current instant as an RFC3339 UTC string."},
	"time.parse":       {Label: "time.parse(text: string, layout?: string) -> string", Params: []string{"text", "layout"}, Doc: "Normalizes `text` to RFC3339 UTC. `layout` is a Go reference layout or friendly tokens (`dd/MM/yyyy`). Unparseable input raises."},
	"time.format":      {Label: "time.format(value: string, layout?: string) -> string", Params: []string{"value", "layout"}, Doc: "Formats an RFC3339 string with `layout`."},
	"time.add":         {Label: "time.add(value: string, duration: duration) -> string", Params: []string{"value", "duration"}, Doc: "Returns `value + duration` as an RFC3339 UTC string."},
	"time.diff":        {Label: "time.diff(a: string, b: string) -> number", Params: []string{"a", "b"}, Doc: "Seconds in `a - b` (negative when `a` is earlier)."},
	"time.compare":     {Label: "time.compare(a: string, b: string) -> number", Params: []string{"a", "b"}, Doc: "`-1` if `a < b`, `0` if equal, `1` if `a > b`."},
}

// httpSig builds the signature entry for one http.<verb> native op — they
// all share the same parameter surface (internal/engine/interpreter/tool.go
// httpOptions), so only the verb in the label differs.
func httpSig(verb string) sig {
	return sig{
		Label: "http." + verb + "(url: string, headers?, query?, body?, text?, form?, timeout?: duration, " +
			"follow_redirects?: bool, raise_for_status?: bool, auth?: {bearer, basic}, tls?: {cert, key, ca, insecure}, proxy?: string) " +
			"-> {status: number, body: string, headers: {string: string}, ok: bool, json: any}",
		Params: []string{"url", "headers", "query", "body", "text", "form", "timeout", "follow_redirects", "raise_for_status", "auth", "tls", "proxy"},
		Doc: "Issues an HTTP " + strings.ToUpper(verb) + ". `body` is JSON-encoded (sets `Content-Type: application/json`); " +
			"`text`/`form` are the raw and form-encoded alternatives (pick one). `tls.cert`/`tls.key` are PEM client-certificate " +
			"paths. `proxy` overrides `HTTP(S)_PROXY` for this call. A transport failure raises; a non-2xx status is returned in " +
			"`status` unless `raise_for_status` is set.",
	}
}

// --- collection / value methods, keyed by method name -------------------

var commonMethodSigs = map[string]sig{
	"size":     {Label: "size() -> number", Params: nil, Doc: "Element count (array), entry count (object), or byte length (string)."},
	"is_empty": {Label: "is_empty() -> bool", Params: nil, Doc: "Equivalent to `size() == 0`."},
}

var stringMethodSigs = map[string]sig{
	"split":       {Label: "split(separator: string) -> string[]", Params: []string{"separator"}, Doc: "Splits on every occurrence of `separator`."},
	"replace":     {Label: "replace(old: string, new: string) -> string", Params: []string{"old", "new"}, Doc: "Replaces all occurrences of `old`."},
	"contains":    {Label: "contains(sub: string) -> bool", Params: []string{"sub"}, Doc: "Whether the string contains `sub`."},
	"starts_with": {Label: "starts_with(prefix: string) -> bool", Params: []string{"prefix"}, Doc: ""},
	"ends_with":   {Label: "ends_with(suffix: string) -> bool", Params: []string{"suffix"}, Doc: ""},
	"trim":        {Label: "trim() -> string", Params: nil, Doc: "Strips leading and trailing whitespace."},
	"to_upper":    {Label: "to_upper() -> string", Params: nil, Doc: ""},
	"to_lower":    {Label: "to_lower() -> string", Params: nil, Doc: ""},
	"substring":   {Label: "substring(start: number, end: number) -> string", Params: []string{"start", "end"}, Doc: "Byte range `[start, end)`; both bounds must be integers within `[0, size()]`."},
}

var arrayMethodSigs = map[string]sig{
	"contains":  {Label: "contains(value: any) -> bool", Params: []string{"value"}, Doc: "Deep equality against each element."},
	"get_index": {Label: "get_index(index: number) -> any", Params: []string{"index"}, Doc: "Element at `index` (0-based integer). Out of range raises."},
	"index_of":  {Label: "index_of(value: any) -> number", Params: []string{"value"}, Doc: "First deep-equal position, or `-1`."},
	"filter":    {Label: "filter(predicate: (item: any) -> bool) -> any[]", Params: []string{"predicate"}, Doc: "New array of the elements for which `predicate` is true."},
	"find":      {Label: "find(predicate: (item: any) -> bool) -> any", Params: []string{"predicate"}, Doc: "First matching element, or `null`."},
	"sort_by":   {Label: "sort_by(key: (item: any) -> any) -> any[]", Params: []string{"key"}, Doc: "New array, ascending by the key each element maps to."},
}

var objectMethodSigs = map[string]sig{
	"keys":   {Label: "keys() -> string[]", Params: nil, Doc: "Keys in stable (sorted) order."},
	"values": {Label: "values() -> any[]", Params: nil, Doc: "Values in the same order as `keys()`."},
}

// --- declared-construct methods ----------------------------------------

var memoryMethodSigs = map[string]sig{
	"set":    {Label: "set(key: string, value: any) -> any    |    set(values: object) -> object", Params: []string{"key", "value"}, Doc: "kv/json: writes an entry and returns the value. json also accepts a single object (bulk write)."},
	"get":    {Label: "get(key: string, default?: any) -> any", Params: []string{"key", "default"}, Doc: "kv/json: reads an entry, or `default` (or `null`). json keys may navigate nested values with `::` segments."},
	"append": {Label: "append(value: any) -> any", Params: []string{"value"}, Doc: "append_log: one text line (a string). jsonl: one JSON line (any value). Returns what was written."},
	"reset":  {Label: "reset() -> null", Params: nil, Doc: "Ephemeral `mem` only: clears the store."},
}

var mcpServerMethodSigs = map[string]sig{
	"call":       {Label: "call(tool: string, arguments?: object) -> any", Params: []string{"tool", "arguments"}, Doc: "Issues a stateless JSON-RPC `tools/call`; returns the decoded result."},
	"list_tools": {Label: "list_tools() -> any", Params: nil, Doc: "Issues `tools/list`. One page only."},
	"discover":   {Label: "discover() -> any", Params: nil, Doc: "Issues `server/discover` — supported versions, capabilities, identity."},
}

var agentMethodSigs = map[string]sig{
	"run": {
		Label:  "run(prompt: string | Prompt(...), schema?: string) -> string",
		Params: []string{"prompt", "schema"},
		Doc:    "Runs the agent. `prompt:` (required) is a string literal or a declared `prompt` template call. `schema:` (optional) is a string, usually `json.stringify({...})`. Returns the model's response text.",
	},
}

// --- bare-name callables (no receiver) --------------------------------

var globalSigs = map[string]sig{
	"log":  {Label: "log(...values: any) -> null", Params: []string{"values"}, Doc: "Writes one space-joined line to stdout."},
	"fail": {Label: "fail(...values: any) -> never", Params: []string{"values"}, Doc: "Raises an error whose message is the joined values. Catchable with `try/catch`; uncaught, it makes `mhl run` exit non-zero."},
	"env":  {Label: "env(name: string) -> string", Params: []string{"name"}, Doc: "Reads an OS environment variable. Returns `\"\"` when unset."},
}

var assertionSigs = map[string]sig{
	"are_equal":             {Label: "are_equal(actual: any, expected: any)", Params: []string{"actual", "expected"}, Doc: "Passes when `actual` deep-equals `expected`."},
	"are_not_equal":         {Label: "are_not_equal(a: any, b: any)", Params: []string{"a", "b"}, Doc: "Passes when `a` does not deep-equal `b`. Alias: `not_equal`."},
	"not_equal":             {Label: "not_equal(a: any, b: any)", Params: []string{"a", "b"}, Doc: "Alias of `are_not_equal`."},
	"is_true":               {Label: "is_true(value: bool)", Params: []string{"value"}, Doc: "Passes when `value` is `true`. A non-bool argument errors."},
	"is_false":              {Label: "is_false(value: bool)", Params: []string{"value"}, Doc: "Passes when `value` is `false`. A non-bool argument errors."},
	"is_null":               {Label: "is_null(value: any)", Params: []string{"value"}, Doc: "Passes when `value` is `null`."},
	"not_null":              {Label: "not_null(value: any)", Params: []string{"value"}, Doc: "Passes when `value` is not `null`."},
	"greater_than":          {Label: "greater_than(a: number, b: number)", Params: []string{"a", "b"}, Doc: "Passes when `a > b`."},
	"less_than":             {Label: "less_than(a: number, b: number)", Params: []string{"a", "b"}, Doc: "Passes when `a < b`."},
	"greater_than_or_equal": {Label: "greater_than_or_equal(a: number, b: number)", Params: []string{"a", "b"}, Doc: "Passes when `a >= b`."},
	"less_than_or_equal":    {Label: "less_than_or_equal(a: number, b: number)", Params: []string{"a", "b"}, Doc: "Passes when `a <= b`."},
	"includes":              {Label: "includes(array: any[], value: any)", Params: []string{"array", "value"}, Doc: "Passes when `array` contains a deep-equal `value`."},
	"incomplete":            {Label: "incomplete(reason?: any)", Params: []string{"reason"}, Doc: "Marks the enclosing `describe` as skipped/pending. Never fails."},
}

// signatureForMethod resolves a `receiver.method` call's signature from the
// receiver's symbol kind. receiver is the symbol name (a native namespace, a
// declared agent/memory/mcp_server, or a typed variable); ok is false when
// nothing static is known (a user-declared `tool` method, say).
func signatureForMethod(kind symbolKind, receiver, method string) (sig, bool) {
	switch kind {
	case symNative:
		s, ok := nativeSigs[receiver+"."+method]
		return s, ok
	case symString:
		return lookupValueMethod(stringMethodSigs, method)
	case symArray:
		return lookupValueMethod(arrayMethodSigs, method)
	case symObject:
		return lookupValueMethod(objectMethodSigs, method)
	case symMemory:
		s, ok := memoryMethodSigs[method]
		return s, ok
	case symMCPServer:
		s, ok := mcpServerMethodSigs[method]
		return s, ok
	case symAgent:
		s, ok := agentMethodSigs[method]
		return s, ok
	default:
		return sig{}, false
	}
}

// lookupValueMethod checks a value-kind's own method table first, then the
// size()/is_empty() common set every value kind shares.
func lookupValueMethod(own map[string]sig, method string) (sig, bool) {
	if s, ok := own[method]; ok {
		return s, true
	}
	s, ok := commonMethodSigs[method]
	return s, ok
}

// signatureForBareCall resolves a call with no receiver — a global builtin
// (log/fail/env) or a test assertion.
func signatureForBareCall(name string) (sig, bool) {
	if s, ok := globalSigs[name]; ok {
		return s, true
	}
	s, ok := assertionSigs[name]
	return s, ok
}
