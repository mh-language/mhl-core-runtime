# mhl standard library

Every built-in callable, grouped by receiver. `?` marks an optional argument. Native ops
take their optional arguments by name (`cmd.exec(argv, timeout: 30s)`). The canonical
table is `src/mhl-runtime/internal/lsp/signatures.go` in the mhl repository.

## Globals

| Call | Returns | Notes |
|---|---|---|
| `log(...values)` | null | One space-joined line on stdout |
| `fail(...values)` | never | Raises a catchable error |
| `pause(reason?)` | never | Suspends the run at this step |
| `complete()` | never | Ends the run as completed |
| `env(name, default?)` | string | `""` when unset and no default; see `mhl-tools` for fail-closed positions |
| `nameof(Name)` | string | A declared top-level name, validated by lint |
| `type_of(value)` | string | `string`, `number`, `bool`, `array`, `object`, `null`, `enum` |
| `is_string`, `is_number`, `is_bool`, `is_array`, `is_object`, `is_null`, `is_enum` | bool | `is_null` is also a test assertion inside `describe` |

## Value methods

Every string, array and object: `size()`, `is_empty()`, `equals(other)`, `deep_equal(other)`.

**String:** `split(sep)`, `replace(old, new)` (all occurrences), `contains(sub)`,
`starts_with(prefix)`, `ends_with(suffix)`, `matches(pattern)` (RE2, must match the whole
string), `trim()`, `to_upper()`, `to_lower()`, `substring(start, end)` and
`remove(from, to)` (byte offsets), `remove_content(from, to)` and
`extract_content(from, to)` (by marker text).

**Array:** `get_index(i)` (raises out of range), `index_of(value)` (`-1` when absent),
`contains(value)`, `filter(pred)`, `find(pred)` (`null` when absent), `sort_by(key)`,
`map(fn)`, `reduce(fn, initial)`, `any(pred)`, `all(pred)`, `append(value)`, `join(sep)`,
`unique()`, `enumerate()` (`{index, value}` items). All return new arrays. Slices:
`xs[1..4]`, `xs[..3]`, `xs[3..]`, `xs[^3..]`.

**Object:** `keys()` (sorted), `values()`, `get(key, default?)` (never raises).

## Native namespaces

The error column is the one to remember: **value** means a failure comes back as data you
must inspect, **raises** means it fails the step unless caught.

### cmd

| Call | Returns | Error |
|---|---|---|
| `cmd.exec(command, timeout?, encoding?)` | `{stdout, stderr, exit_code}` | value: non-zero exit is returned |
| `cmd.exec_all(commands, timeout?, encoding?)` | array of the above, input order | value; runs concurrently |

`command` is an argv array (preferred) or a shell string.

### fs and dir

| Call | Returns | Error |
|---|---|---|
| `fs.read(path, encoding?)` | string | raises |
| `fs.write(path, content, encoding?)` | bool | raises; truncates |
| `fs.append(path, content, encoding?)` | bool | raises; creates the file |
| `fs.exists(path)` | bool | raises on a stat error other than not found |
| `fs.delete(path)` | bool | raises; file or empty directory |
| `fs.join(...segments)` | string | |
| `fs.list(dir)` | string[] | |
| `dir.create(path)` | bool | idempotent, creates parents |
| `dir.exists(path)` | bool | true only for a directory |
| `dir.list(path)` | string[] | immediate entries, sorted |
| `dir.clear(path)` | bool | leaves `path` existing and empty |
| `dir.delete(path)` | bool | raises unless the directory is empty |

### git

Every op takes `dir?` to run against another checkout.

| Call | Returns | Error |
|---|---|---|
| `git.status(paths?, dir?)` | `{stdout, stderr, exit_code}` | value |
| `git.add(paths, dir?)` | `{stdout, stderr, exit_code}` | value |
| `git.commit(message, paths?, dir?)` | `{stdout, stderr, exit_code}` | value |
| `git.diff(target?, dir?)` | string | raises |
| `git.rev_parse(ref, dir?)` | string (short hash) | raises |
| `git.log(n, dir?)` | string (oneline) | raises |

### http

`http.get`, `post`, `put`, `patch`, `delete`, `head`, `options` share one surface:

```mh-fragment
http.post(url: "...", headers: {...}, query: {...},
          body: {...},          // JSON; or text: "...", or form: {...}, pick one
          timeout: 10s,         // default 30s
          follow_redirects: true, raise_for_status: false,
          auth: {bearer: env("TOKEN")},  // or {basic: {user, password}}
          tls: {cert, key, ca, insecure}, proxy: "http://...")
```

Returns `{status, body, headers, ok, json}`. A non-2xx status is a **value** (check
`.ok`) unless `raise_for_status: true`; a transport failure **raises**.
`http.download(url, path, ...)` streams to `path` atomically and returns
`{status, path, bytes, ok, headers}`.

### json, log, time, uuid, os, html

| Call | Returns | Notes |
|---|---|---|
| `json.parse(text)` | any | raises on invalid JSON |
| `json.parse_lines(text)` | any[] | JSON lines |
| `json.stringify(value)` | string | keys sorted |
| `log.info`, `log.warn`, `log.error` `(...values)` | null | prefixed; `log.error` does not raise |
| `time.now()` | string | RFC3339 UTC. Datetimes are plain strings |
| `time.parse(text, layout?)`, `time.format(value, layout?)` | string | `yyyy-MM-dd HH:mm:ss` tokens or Go layouts |
| `time.add(value, duration)` | string | `time.add(time.now(), 7d)` |
| `time.diff(a, b)` | number | seconds in `a - b` |
| `time.compare(a, b)` | number | `-1`, `0`, `1` |
| `time.sleep(duration)` | null | cancellation-aware |
| `uuid.v4()`, `uuid.v7()` | string | v7 sorts by creation time |
| `os.user`, `home_dir`, `hostname`, `platform`, `arch`, `cwd`, `pid`, `executable` `()` | string or number | the host running mhl, never the caller |
| `html.parse(text)` | object | fragment into `{tag, attrs, text, children}` |
| `html.get_element(node, tag?, attrs?)`, `html.get_elements(...)`, `html.get_element_by_id(node, id)` | node, nodes | `null` on a miss |
| `html.get_attribute(element, name, default?)`, `html.get_text(node)`, `html.to_html(node)` | any, string | `get_attribute` never raises |
| `html.escape(text)`, `html.attr_escape(text)` | string | |

## Declared constructs

| Receiver | Methods |
|---|---|
| `agent` | `run(prompt, schema?, skills?)` |
| `router` | `delegate(prompt, schema?)`, `select(prompt)` |
| `memory` | `set(key, value)`, `set(object)`, `get(key, default?)`, `remove(key)`, `append(value)`, by store type |
| `mem` variable | `reset()` |
| `extension mcp` | `call(tool, args)`, `list_tools()`, `discover()` |
| `extension a2a` | `send(message, context?)`, `agent_card()`, `get_task(id, history_length?)`, `cancel(id)` |
| `enum` | `parse(text)`, `values()` |
| `prompt` | `frontmatter` (file-sourced prompts) |
| `schema` | `.content`, `.path` |
| `pipeline`, `workflow` | `run(inputs?)`, inside `describe` only |

Assertions live in the `mhl-testing` skill.
