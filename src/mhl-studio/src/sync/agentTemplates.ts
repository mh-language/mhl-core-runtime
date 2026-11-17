// Ready-to-use agent templates: five agent CLIs × three roles, each with the
// flags that make that role safe and predictable on that CLI, plus two HTTP
// APIs called with curl (see the API section below). Distilled from
// agents running in production (every flag below was kept for a measured
// reason, noted next to it); flags drift between CLI versions, so every
// template names the version its flags were checked against.
//
// Common rules:
// - The prompt goes on stdin, never argv, wherever the CLI allows it: argv
//   caps at ~32K characters on Windows and is visible in `ps`.
// - A role is a permission profile: a *generator* has no tools at all and
//   answers in JSON shaped by `schema:`; a *coder* reads, edits and runs
//   builds/tests inside its repository only; a *reader* only reads it.
// - Model and effort are per-call inputs; empty means the CLI's default.

export type Backend = "claude" | "codex" | "copilot" | "antigravity" | "devin" | "claude-api" | "openai-api";
export type Role = "generator" | "coder" | "reader" | "text";

export interface BackendInfo {
  id: Backend;
  label: string;
  /** An agent CLI, or an HTTP API reached with curl. */
  kind: "cli" | "api";
  /** The executable the agent runs: the CLI, or `sh` for an API. */
  command: string;
  /** What must be on PATH for it to run. */
  requires: string[];
  /** The roles it offers. */
  roles: Role[];
  /** Prefix of the default agent name: Claude → ClaudeCoder. */
  prefix: string;
  /** The CLI version (or API version) the template was checked against. */
  checked: string;
}

const CLI_ROLES: Role[] = ["generator", "coder", "reader"];
const API_ROLES: Role[] = ["generator", "text"];
const cli = (id: Backend, label: string, command: string, prefix: string, checked: string): BackendInfo => ({
  id,
  label,
  kind: "cli",
  command,
  requires: [command],
  roles: CLI_ROLES,
  prefix,
  checked,
});
const api = (id: Backend, label: string, prefix: string, checked: string): BackendInfo => ({
  id,
  label,
  kind: "api",
  command: "sh",
  requires: ["curl", "jq"],
  roles: API_ROLES,
  prefix,
  checked,
});

export const BACKENDS: BackendInfo[] = [
  cli("claude", "Claude Code", "claude", "Claude", "claude 2.1.278"),
  cli("codex", "Codex CLI", "codex", "Codex", "codex-cli 0.147.0"),
  cli("copilot", "GitHub Copilot CLI", "copilot", "Copilot", "copilot 1.0.110"),
  cli("antigravity", "Antigravity (agy)", "agy", "Antigravity", "agy 1.5.8"),
  cli("devin", "Devin CLI", "devin", "Devin", "devin 3000.12.43"),
  api("claude-api", "Claude API", "ClaudeApi", "the Messages API, anthropic-version 2026-10-21"),
  api("openai-api", "OpenAI-compatible API", "OpenAi", "the Chat Completions API"),
];

export const ROLES: { id: Role; label: string; hint: string; suffix: string }[] = [
  { id: "generator", label: "Structured answer", hint: "no tools; answers in JSON shaped by schema:", suffix: "Generator" },
  { id: "coder", label: "Coder", hint: "reads, edits and runs builds/tests in a repository", suffix: "Coder" },
  { id: "reader", label: "Reader", hint: "only reads a repository (and extra folders)", suffix: "Reader" },
  { id: "text", label: "Text answer", hint: "no tools; answers in plain text", suffix: "Text" },
];

/** A companion file a template needs next to it (Devin's permission config). */
export interface Companion {
  /** File name, created in the same folder as the agent's file. */
  name: string;
  content: string;
}

export interface AgentTemplate {
  /** The `agent` declaration (no trailing newline). */
  source: string;
  companions: Companion[];
}

export interface RenderOptions {
  /** The declaration name (default: prefix + role suffix, e.g. ClaudeCoder). */
  name?: string;
  /** The folder of the agent's file, relative to the workspace root ("" = root): where companions live. */
  dir?: string;
}

type Arg = [expr: string, note?: string];

const q = (s: string) => JSON.stringify(s);

/** `...(if (v.is_empty()) [] else ["--flag", v])`: the flag only when the input is set. */
const opt = (flag: string, input: string, value = input): string => `...(if (${input}.is_empty()) [] else [${q(flag)}, ${value}])`;

const DIRS: Arg = ["...dirs.flat_map((d) -> [\"--add-dir\", d])", "one --add-dir per extra folder it may read"];

const TIMEOUT: Record<Role, string> = { generator: "10m", text: "10m", reader: "20m", coder: "60m" };

interface Spec {
  description: string;
  notes: string[];
  inputs: string[];
  cwd?: string;
  args: Arg[];
  stdin?: string;
  retry: { max: number; delay: string; on: (number | string)[] };
  companions?: Companion[];
}

function claude(role: Role): Spec {
  const model: Arg = [opt("--model", "model"), "an alias (sonnet, opus, haiku) or a full model id"];
  const effort: Arg = [opt("--effort", "effort"), "low | medium | high | xhigh | max"];
  const retry = { max: role === "generator" ? 3 : 2, delay: role === "generator" ? "2s" : "5s", on: [500, 503, ...(role === "generator" ? ["timeout"] : []), "rate_limit", "overloaded"] };
  const effortInput = `input effort: string = "medium"`;
  if (role === "generator")
    return {
      description: "Answers in JSON shaped by the call's schema, with Claude Code and no tools.",
      notes: [
        "Call it with schema: X.content (the schema's JSON text).",
        "Without --effort extended thinking stays on: measured 99s/US$0.30 per call vs 57s/US$0.14 with medium.",
      ],
      inputs: [`input model: string = ""`, effortInput],
      args: [
        ['"-p"'],
        [q("Read the content below and answer exactly as it asks. When you call StructuredOutput, put the schema's fields directly at the root of its argument."), "the prompt itself comes on stdin; the second sentence stops a wrapped answer that costs a retry turn"],
        ['"--output-format", "json"', "stdout is a JSON envelope: the answer is in .structured_output, cost in .total_cost_usd"],
        ['"--json-schema", "${schema}"', "inline schema text"],
        model,
        effort,
        ['"--tools", ""', 'no built-in tools at all (--allowed-tools "" does NOT block anything)'],
        ['"--strict-mcp-config", "--setting-sources", "project"', "skip the user's ~/.claude plugins, skills and MCP servers (~44k input tokens per call otherwise)"],
        ['"--no-session-persistence"'],
      ],
      stdin: "${prompt}",
      retry,
    };
  if (role === "coder")
    return {
      description: "Carries out a coding task in a repository with Claude Code: edits files, runs builds and tests.",
      notes: ["Claude refuses to read outside its working directory by default."],
      inputs: ["input repo: string", `input model: string = ""`, effortInput],
      cwd: "repo",
      args: [
        ['"-p", "Read the content below and carry out the task it describes."', "the task comes on stdin"],
        ['"--output-format", "json"', "stdout is a JSON envelope: the final message is in .result"],
        model,
        effort,
        ['"--permission-mode", "acceptEdits"', "edits without asking"],
        ['"--allowed-tools", "Bash"', "builds and tests run without a prompt"],
        ['"--disallowed-tools", "WebFetch,WebSearch"', "no web"],
        ['"--no-session-persistence"'],
      ],
      stdin: "${prompt}",
      retry,
    };
  return {
    description: "Answers questions about a repository with Claude Code, without editing or running anything.",
    notes: ["Read, Grep and Glob need no permission in print mode; everything that writes or runs is denied."],
    inputs: ["input repo: string", `input model: string = ""`, effortInput, "input dirs: string[] = []"],
    cwd: "repo",
    args: [
      ['"-p", "Read the content below and answer what it asks."', "the question comes on stdin"],
      DIRS,
      ['"--output-format", "json"', "stdout is a JSON envelope: the answer is in .result"],
      model,
      effort,
      ['"--disallowed-tools", "Bash,Edit,Write,MultiEdit,NotebookEdit,WebFetch,WebSearch"'],
      ['"--no-session-persistence"'],
    ],
    stdin: "${prompt}",
    retry,
  };
}

function codex(role: Role): Spec {
  const model: Arg = [opt("--model", "model"), "a model id the account can use"];
  const effort: Arg = [opt("-c", "effort", `"model_reasoning_effort=" + effort`), "low | medium | high | xhigh (the server refuses a value the model does not list)"];
  const common: Arg[] = [['"--ephemeral"', "no session files"], ['"--ignore-user-config"', "never the user's ~/.codex config"]];
  const tail: Arg[] = [['"--color", "never"'], model, effort, ['"--json"', "stdout is JSONL events: the answer is the last agent_message"]];
  const retry = { max: role === "generator" ? 3 : 2, delay: role === "generator" ? "2s" : "5s", on: [500, 503, ...(role === "generator" ? ["timeout"] : []), "rate_limit"] };
  if (role === "generator")
    return {
      description: "Answers in JSON shaped by the call's schema, with Codex in a read-only sandbox.",
      notes: [
        "Call it with schema: X.path — Codex takes the schema as a FILE path (--output-schema).",
        "Point workdir at an empty folder: the model can still read whatever is under it.",
      ],
      inputs: [`input model: string = ""`, `input effort: string = ""`, `input workdir: string = "."`],
      args: [
        ['"exec"'],
        ['"--sandbox", "read-only"'],
        ['"--cd", workdir', "Codex's own working root, independent of mhl's"],
        ['"--skip-git-repo-check"', "workdir need not be a git repository (safe: read-only)"],
        ...common,
        ['"--ignore-rules"', "no AGENTS.md / rules from workdir"],
        ...tail,
        ['"--output-schema", "${schema}"'],
        ['"-"', "read the prompt from stdin"],
      ],
      stdin: "${prompt}",
      retry,
    };
  if (role === "coder")
    return {
      description: "Carries out a coding task in a repository with Codex: edits files, runs builds and tests.",
      notes: ["The repository's AGENTS.md is respected (no --ignore-rules)."],
      inputs: ["input repo: string", `input model: string = ""`, `input effort: string = ""`],
      cwd: "repo",
      args: [
        ['"exec"'],
        ['"--sandbox", "workspace-write"', "writes only inside the repository"],
        ['"-c", "sandbox_workspace_write.network_access=true"', "without network even 127.0.0.1 is denied: test runners and package restores fail"],
        ['"--cd", "."'],
        ...common,
        ...tail,
        ['"-"', "read the task from stdin"],
      ],
      stdin: "${prompt}",
      retry,
    };
  return {
    description: "Answers questions about a repository with Codex in a read-only sandbox.",
    notes: ["Codex reads only through its terminal: tell it in the prompt to use ls, cat, sed -n or rg (anything that writes fails)."],
    inputs: ["input repo: string", `input model: string = ""`, `input effort: string = ""`],
    cwd: "repo",
    args: [
      ['"exec"'],
      ['"--sandbox", "read-only"'],
      ['"--skip-git-repo-check"', "repo need not be a git repository (safe: read-only)"],
      ['"--cd", "."'],
      ...common,
      ...tail,
      ['"-"', "read the question from stdin"],
    ],
    stdin: "${prompt}",
    retry,
  };
}

function copilot(role: Role): Spec {
  const model: Arg = [opt("--model", "model"), "an id of the account's catalogue, or auto"];
  const effort: Arg = [`...(if (effort.is_empty() || model == "auto") [] else ["--reasoning-effort", effort])`, "low | medium | high | xhigh | max; the CLI refuses it with the auto model"];
  const head: Arg[] = [model, effort, ['"--output-format", "json"', "stdout is JSONL events"], ['"--no-ask-user", "--no-color"']];
  const inputs = [`input model: string = ""`, `input effort: string = ""`];
  if (role === "generator")
    return {
      description: "Answers in JSON shaped by the call's schema, with GitHub Copilot CLI and no tools.",
      notes: [
        "Copilot has no schema flag: the schema rides in the prompt (stdin below). Call it with schema: X.content",
        "and cut the JSON object out of the reply. With -p Copilot ignores stdin, so there is none here.",
      ],
      inputs: [...inputs, `input workdir: string = "."`],
      cwd: "workdir",
      args: [
        ...head,
        ['"--available-tools="', "an empty list: no tools at all"],
        ['"--disable-builtin-mcps"'],
        ['"--no-custom-instructions"', "no AGENTS.md / instruction files"],
        ['"--disallow-temp-dir"'],
      ],
      stdin: "${prompt}\\n\\nAnswer only with a JSON object, without Markdown or any other text, that follows this JSON Schema:\\n${schema}",
      retry: { max: 3, delay: "2s", on: [500, 503, "timeout", "timed out", "rate_limit", "rate limit", "Failed to load models", "ECONNRESET", "ETIMEDOUT"] },
    };
  if (role === "coder")
    return {
      description: "Carries out a coding task in a repository with GitHub Copilot CLI: shell and writes allowed, web denied.",
      notes: [
        "--sandbox is required: without it a script could write outside the repository (the path check only sees",
        "the command text); with it (Seatbelt on macOS) that write gets EPERM while builds and 127.0.0.1 keep working.",
      ],
      inputs: ["input repo: string", ...inputs],
      cwd: "repo",
      args: [
        ...head,
        ['"--sandbox"'],
        ['"--allow-tool", "shell", "--allow-tool", "write"'],
        ['"--deny-tool", "url"', "no web"],
        ['"--disable-builtin-mcps"'],
        ['"--disallow-temp-dir"'],
      ],
      stdin: "${prompt}",
      // Only what fails before any work: a second implementation on top of a
      // half-done first one is worse than a failure.
      retry: { max: 2, delay: "5s", on: ["Failed to load models"] },
    };
  return {
    description: "Answers questions about a repository with GitHub Copilot CLI, without editing or running anything.",
    notes: ["It reads with view, rg and glob; a refused tool does not end the conversation."],
    inputs: ["input repo: string", ...inputs, "input dirs: string[] = []"],
    cwd: "repo",
    args: [
      DIRS,
      ...head,
      ['"--deny-tool", "shell", "--deny-tool", "write", "--deny-tool", "url", "--deny-tool", "memory"'],
      ['"--disable-builtin-mcps"'],
      ['"--disallow-temp-dir"'],
    ],
    stdin: "${prompt}",
    retry: { max: 2, delay: "5s", on: [500, 503, "rate_limit", "rate limit", "Failed to load models", "ECONNRESET", "ETIMEDOUT"] },
  };
}

/** agy's --settings: permission rules for this session only (deny beats ask beats allow). */
function agyRules(allow: string[], deny: string[]): string {
  return q(JSON.stringify({ permissions: { allow, deny } }));
}

function antigravity(role: Role): Spec {
  const model: Arg = [opt("--model", "model"), "an id from `agy models` (the effort is part of the id: -low | -medium | -high)"];
  const prompt: Arg = ['"-p", "${prompt}"', "argv: agy reads stdin only as NDJSON events, which needs the prompt JSON-escaped (prompts over ~30K chars hit Windows' limit)"];
  const retry = { max: role === "generator" ? 3 : 2, delay: role === "generator" ? "2s" : "5s", on: [500, 503, ...(role === "generator" ? ["timeout"] : []), "rate_limit", "RESOURCE_EXHAUSTED", "UNAVAILABLE"] };
  const net = ["read_url(*)", "execute_url(*)", "mcp(*)"];
  if (role === "generator")
    return {
      description: "Answers in JSON shaped by the call's schema, with Antigravity and no tools.",
      notes: [
        "Call it with schema: X.content. agy 1.4 needed an absolute \"$id\" in the schema to resolve \"$ref\", and",
        "Gemini rejects an enum containing \"\" (400 INVALID_ARGUMENT).",
        "agy has no working-directory flag: point workdir at an empty folder (it lists and greps its workspace freely).",
      ],
      inputs: [`input model: string = ""`, `input workdir: string = "."`],
      cwd: "workdir",
      args: [
        prompt,
        ['"--output-format", "json"'],
        ['"--json-schema", "${schema}"'],
        model,
        ['"--settings", ' + agyRules([], ["read_file(*)", "write_file(*)", "command(*)", "unsandboxed(*)", ...net]), "deny every tool; `unsandboxed(*)` too, or a command outside the OS sandbox passes"],
        ['"--disable-slash-commands"'],
      ],
      retry,
    };
  if (role === "coder")
    return {
      description: "Carries out a coding task in a repository with Antigravity: edits files, runs builds and tests.",
      notes: ["Network through the terminal (package restores) stays available; browser and MCP are denied."],
      inputs: ["input repo: string", `input model: string = ""`],
      cwd: "repo",
      args: [
        prompt,
        ['"--output-format", "json"'],
        model,
        ['"--mode", "accept-edits"', "edits without asking"],
        ['"--settings", ' + agyRules(["write_file(*)", "command(*)", "unsandboxed(*)"], net)],
        ['"--disable-slash-commands"'],
      ],
      retry,
    };
  return {
    description: "Answers questions about a repository with Antigravity, without editing or running anything.",
    notes: ["Reading inside the workspace (the repo plus --add-dir folders) is allowed; outside it is refused."],
    inputs: ["input repo: string", `input model: string = ""`, "input dirs: string[] = []"],
    cwd: "repo",
    args: [
      DIRS,
      prompt,
      ['"--output-format", "json"'],
      model,
      ['"--settings", ' + agyRules([], ["write_file(*)", "command(*)", "unsandboxed(*)", ...net])],
      ['"--disable-slash-commands"'],
    ],
    retry,
  };
}

/** Devin's permission config for a role (passed with --config). */
function devinConfig(role: Role): string {
  const allow = role === "coder" ? ["read", "edit", "grep", "glob", "exec"] : role === "reader" ? ["read", "grep", "glob"] : [];
  const deny = role === "coder" ? ["mcp__*", "Fetch(*)"] : role === "reader" ? ["edit", "exec", "mcp__*", "Fetch(*)"] : ["read", "edit", "grep", "glob", "exec", "mcp__*", "Fetch(*)"];
  return (
    JSON.stringify(
      {
        notify: "never",
        permissions: { allow, ask: [], deny },
        // agents_standard: the repository's .agents/skills/ are honoured.
        read_config_from: { agents_standard: role !== "generator", cursor: false, windsurf: false, claude: false },
        version: 1,
        theme_mode: "nocolor",
        shell: { setup_complete: true },
      },
      null,
      2,
    ) + "\n"
  );
}

function devin(role: Role, dir: string): Spec {
  const file = `devin-${role}.json`;
  const rel = dir ? `${dir}/${file}` : file;
  const config: Arg = [`"--config", os.cwd() + ${q(`/${rel}`)}`, `permissions in ${file}; absolute because the CLI runs in its own folder (mhl runs from the workspace root)`];
  const model: Arg = [opt("--model", "model"), "a Devin model id"];
  const head: Arg[] = [config, model, ['"--respect-workspace-trust", "false"', "print mode cannot show the trust prompt"]];
  const prompt: Arg = ['"-p", "${prompt}"', "argv: Devin reads a prompt only inline or from --prompt-file (no stdin)"];
  const companions = [{ name: file, content: devinConfig(role) }];
  const transient = [500, 503, "rate_limit", "retryable", "unavailable", "Connection error"];
  if (role === "generator")
    return {
      description: "Answers in JSON with Devin CLI and every tool denied.",
      notes: [
        "Devin has no structured-output flag: put the JSON Schema in the prompt and check the reply.",
        "Keep --config: without it Devin may read and run anything in its working folder. Calls failing with",
        "\"connection error\"/\"unavailable\" were once seen with --config plus --permission-mode; investigate before dropping it.",
      ],
      inputs: [`input model: string = ""`],
      args: [...head, ['"--permission-mode", "auto"'], prompt],
      retry: { max: 5, delay: "5s", on: [...transient.slice(0, 2), "timeout", "empty response", ...transient.slice(2)] },
      companions,
    };
  if (role === "coder")
    return {
      description: "Carries out a coding task in a repository with Devin CLI: edits files and runs commands.",
      notes: ["No --sandbox: it forces autonomous mode, which refused even writes in the repository."],
      inputs: ["input repo: string", `input model: string = ""`],
      cwd: "repo",
      args: [...head, ['"--permission-mode", "accept-edits"', "edits without asking; exec is allowed by the config"], prompt],
      retry: { max: 2, delay: "5s", on: transient },
      companions,
    };
  return {
    description: "Answers questions about a repository with Devin CLI, without editing or running anything.",
    notes: ["auto approves read tools only; the config denies edit and exec (Devin ends the conversation at the first refusal)."],
    inputs: ["input repo: string", `input model: string = ""`],
    cwd: "repo",
    args: [...head, ['"--permission-mode", "auto"'], prompt],
    retry: { max: 2, delay: "5s", on: transient },
    companions,
  };
}

// ---- HTTP APIs, called with curl -------------------------------------------
//
// The agent runs a POSIX sh script (`command: "sh"`, the script as one
// """...""" args element: mhl passes it through untouched, so `$1` is the
// shell's). Rules every API script keeps:
// - The prompt comes on stdin and `jq -Rs` turns it into the JSON body, so
//   any quote, newline or `$` in it is escaped (splicing it into a JSON
//   string by hand breaks on the first quote).
// - Each per-call input is its own argv element ("${model}" → $1, ...), never
//   text spliced into the script: no shell injection.
// - The API key never reaches curl's argv (visible in `ps`): it goes in a
//   0600 header file from mktemp, removed on exit.
// - A failure exits non-zero with "HTTP <status> <type> <code>: <message>
//   (request id <id>)" on stderr — the id the provider's support asks for,
//   from its request-id / x-request-id response header. The script, not retry_on, decides what is transient: "[retry]"
//   is added only for a network failure, 408, 409, 5xx, or a 429 that is a
//   rate limit — never a 429 for exhausted credit, quota or a spend limit,
//   which no retry fixes. So retry_on is ["[retry]", "timeout"], no status
//   numbers (a number in an error message would match one).
// - stdout is the answer alone (the JSON text for a structured answer);
//   a truncated or refused answer fails instead of returning half a JSON.

/**
 * POSTs $body to `url` and leaves a 2xx answer's body in $resp; on any
 * failure prints the classified error (see above) and exits 1.
 */
function request(url: string): string[] {
  return [
    String.raw`out=$(printf '%s' "$body" | curl -sS -H "@$hdr" -D "$rh" --data-binary @- -w '\n%{http_code}' "${url}"); rc=$?`,
    String.raw`if [ $rc -ne 0 ]; then case $rc in 7|28|52|55|56) echo "[retry] network failure (curl exit $rc)" >&2;; esac; exit 1; fi`,
    String.raw`status=$(printf '%s' "$out" | tail -n 1); resp=$(printf '%s' "$out" | sed '$d')`,
    String.raw`case $status in 2??) ;; *)`,
    String.raw`    err=$(printf '%s' "$resp" | jq -r '(if type == "array" then .[0] else . end) | .error | ([.type, .code] | map(select(. != null) | tostring) | join(" ")) + ": " + (.message // "")' 2>/dev/null) || err=$resp`,
    String.raw`    case $status in 408|409|5??) retry="[retry] " ;; 429) case $err in *insufficient_quota*|*credit_balance*|*spend_limit*|*usage_limit*|*billing*) retry="" ;; *) retry="[retry] " ;; esac ;; *) retry="" ;; esac`,
    String.raw`    rid=$(tr -d '\r' < "$rh" | awk -F': *' 'tolower($1) == "request-id" || tolower($1) == "x-request-id" { id = $2 } END { print id }')`,
    String.raw`    [ -z "$rid" ] || err="$err (request id $rid)"`,
    String.raw`    printf 'HTTP %s %s%s\n' "$status" "$retry" "$err" >&2; exit 1 ;;`,
    String.raw`esac`,
  ];
}

/** Every API template's retry: what the script marked transient, and the agent's own timeout. */
const API_RETRY = { max: 3, delay: "2s", on: ["[retry]", "timeout"] };

/** Two 0600 temp files, removed on exit: the request's headers (with the key) and the response's. */
const HEADER_FILE = [String.raw`hdr=$(mktemp) && rh=$(mktemp) || exit 2`, String.raw`trap 'rm -f "$hdr" "$rh"' EXIT`];

function script(lines: string[]): Arg {
  const I = "            ";
  return [`"""\n${lines.map((l) => I + l).join("\n")}\n        """`, "the request; inputs arrive as $1, $2, ... and the prompt on stdin"];
}

function claudeApi(role: Role): Spec {
  const structured = role === "generator";
  return {
    description: structured
      ? "Answers in JSON shaped by the call's schema, through the Claude API (Messages API) with curl."
      : "Answers in plain text through the Claude API (Messages API) with curl.",
    notes: [
      ...(structured ? ["Call it with schema: X.content (the schema's JSON text); the answer is that JSON, ready for json.parse."] : []),
      "Needs ANTHROPIC_API_KEY in the environment, and curl and jq on PATH, under a POSIX sh (macOS, Linux, WSL).",
      "effort: low | medium | high | xhigh | max (empty: the model's default). Fails on stop_reason max_tokens or refusal.",
      "fallbacks: on a refusal the API re-runs the request on another model in the same call — \"default\" picks it by refusal",
      "category, a model id names it, \"\" turns it off (required for a model without server-side fallback, e.g. Haiku).",
    ],
    inputs: [
      `input model: string = "claude-opus-5-5"`,
      `input effort: string = ""`,
      `input max_tokens: number = 16000`,
      `input base_url: string = "https://api.anthropic.com/v1"`,
      `input fallbacks: string = "default"`,
    ],
    args: [
      ['"-c"'],
      script([
        String.raw`[ -n "$ANTHROPIC_API_KEY" ] || { echo "ANTHROPIC_API_KEY is not set" >&2; exit 2; }`,
        ...HEADER_FILE,
        String.raw`printf 'x-api-key: %s\nanthropic-version: 2026-10-21\ncontent-type: application/json\n' "$ANTHROPIC_API_KEY" > "$hdr"`,
        String.raw`[ -z "$5" ] || printf 'anthropic-beta: server-side-fallback-2026-07-01\n' >> "$hdr"`,
        String.raw`body=$(jq -Rs --arg model "$1" --arg effort "$2" --argjson max "$3" --arg fallbacks "$5" --arg schema "$6" '{model: $model, max_tokens: $max, messages: [{role: "user", content: .}]} + ({} | if $effort != "" then .effort = $effort else . end | if $schema != "" then .format = {type: "json_schema", schema: ($schema | fromjson)} else . end | if . == {} then {} else {output_config: .} end) + (if $fallbacks == "" then {} elif $fallbacks == "default" then {fallbacks: "default"} else {fallbacks: [{model: $fallbacks}]} end)') || exit 2`,
        ...request("$4/messages"),
        String.raw`printf '%s' "$resp" | jq -je 'if .stop_reason == "end_turn" then [.content[] | select(.type == "text") | .text] | join("") else error("stop_reason \(.stop_reason)") end'`,
      ]),
      ['"claude-api"', "$0: the name in sh's own error messages"],
      ['"${model}", "${effort}", "${max_tokens}", "${base_url}", "${fallbacks}"', "$1 to $5"],
      ...(structured ? ([['"${schema}"', "$6: output_config.format, a JSON Schema the answer must match"]] as Arg[]) : []),
    ],
    stdin: "${prompt}",
    retry: API_RETRY,
  };
}

function openaiApi(role: Role): Spec {
  const structured = role === "generator";
  return {
    description: structured
      ? "Answers in JSON shaped by the call's schema, through an OpenAI-compatible Chat Completions API with curl."
      : "Answers in plain text through an OpenAI-compatible Chat Completions API with curl.",
    notes: [
      ...(structured ? ["Call it with schema: X.content (the schema's JSON text); the answer is that JSON, ready for json.parse."] : []),
      "Any OpenAI-compatible server: set base_url (and key_env, the variable holding its key) per call or as defaults.",
      "On api.openai.com it sends store: false — OpenAI's docs disagree on whether chat completions are stored by default.",
      "Needs that key in the environment, and curl and jq on PATH, under a POSIX sh (macOS, Linux, WSL).",
      "effort is sent as reasoning_effort (empty: not sent; gpt-6-astra takes low and up, not none). Fails on a refusal or a finish_reason other than stop.",
      ...(structured
        ? [
            "strict (default true) makes the schema binding; it then needs every property in required, additionalProperties: false",
            "and an object at the root — the API answers 400 otherwise. strict: false accepts any schema but only steers the answer.",
          ]
        : []),
    ],
    inputs: [
      `input model: string = "gpt-6-astra"`,
      `input effort: string = ""`,
      `input base_url: string = "https://api.openai.com/v1"`,
      `input key_env: string = "OPENAI_API_KEY"`,
      ...(structured ? ["input strict: bool = true"] : []),
    ],
    args: [
      ['"-c"'],
      script([
        String.raw`[ -n "$4" ] && key=$(printenv "$4") && [ -n "$key" ] || { echo "$4 is not set" >&2; exit 2; }`,
        ...HEADER_FILE,
        String.raw`printf 'authorization: Bearer %s\ncontent-type: application/json\n' "$key" > "$hdr"`,
        String.raw`body=$(jq -Rs --arg model "$1" --arg effort "$2" --arg base "$3" --arg strict "$5" --arg schema "$6" '{model: $model, messages: [{role: "user", content: .}]} + (if ($base | test("^https://api[.]openai[.]com(/|$)")) then {store: false} else {} end) + (if $effort != "" then {reasoning_effort: $effort} else {} end) + (if $schema != "" then {response_format: {type: "json_schema", json_schema: {name: "answer", strict: ($strict == "true"), schema: ($schema | fromjson)}}} else {} end)') || exit 2`,
        ...request("$3/chat/completions"),
        String.raw`printf '%s' "$resp" | jq -je '.choices[0] | if .message.refusal then error("refusal: \(.message.refusal)") elif .finish_reason == "stop" then .message.content else error("finish_reason \(.finish_reason)") end'`,
      ]),
      ['"openai-api"', "$0: the name in sh's own error messages"],
      ['"${model}", "${effort}", "${base_url}", "${key_env}"', "$1 to $4"],
      ...(structured ? ([['"${strict}", "${schema}"', "$5, $6: response_format json_schema, the shape the answer must match"]] as Arg[]) : []),
    ],
    stdin: "${prompt}",
    retry: API_RETRY,
  };
}

function spec(backend: Backend, role: Role, dir: string): Spec {
  switch (backend) {
    case "claude":
      return claude(role);
    case "codex":
      return codex(role);
    case "copilot":
      return copilot(role);
    case "antigravity":
      return antigravity(role);
    case "devin":
      return devin(role, dir);
    case "claude-api":
      return claudeApi(role);
    case "openai-api":
      return openaiApi(role);
  }
}

/** The default declaration name: ClaudeCoder, CodexGenerator, ... */
export function agentName(backend: Backend, role: Role): string {
  return BACKENDS.find((b) => b.id === backend)!.prefix + ROLES.find((r) => r.id === role)!.suffix;
}

/** Where the agent puts a call's `schema:`, as `mhl graph` reports it (Declaration.schema). */
export function schemaPlacement(backend: Backend, role: Role): "args" | "stdin" | "none" {
  if (role !== "generator" || backend === "devin") return "none";
  if (BACKENDS.find((b) => b.id === backend)!.kind === "api") return "args";
  return backend === "copilot" ? "stdin" : "args";
}

/** Renders one template as mhl source. */
export function renderAgent(backend: Backend, role: Role, opts: RenderOptions = {}): AgentTemplate {
  const info = BACKENDS.find((b) => b.id === backend)!;
  const roleInfo = ROLES.find((r) => r.id === role)!;
  if (!info.roles.includes(role)) throw new Error(`${info.label} has no ${roleInfo.label.toLowerCase()} template`);
  const s = spec(backend, role, opts.dir ?? "");
  const name = opts.name ?? agentName(backend, role);
  const I = "    ";
  const lines: string[] = [
    `// ${info.label} — ${roleInfo.label.toLowerCase()} (${roleInfo.hint})`,
    ...s.notes.map((n) => `// ${n}`),
    info.kind === "api"
      ? `// Request shape checked against ${info.checked}; if a call fails, compare with the provider's API reference.`
      : `// Flags checked against ${info.checked}; if a call fails after a CLI update, compare with \`${info.command} --help\`.`,
    `export agent ${name} {`,
    `${I}description: ${q(s.description)}`,
    ...s.inputs.map((i) => `${I}${i}`),
    `${I}command: ${q(info.command)}`,
  ];
  if (s.cwd) lines.push(`${I}cwd: ${s.cwd}`);
  lines.push(`${I}args: [`);
  s.args.forEach(([expr, note], i) => {
    const comma = i < s.args.length - 1 ? "," : "";
    lines.push(`${I}${I}${expr}${comma}${note ? ` // ${note}` : ""}`);
  });
  lines.push(`${I}]`);
  if (s.stdin) lines.push(`${I}stdin: "${s.stdin}"`);
  lines.push(
    `${I}timeout: ${TIMEOUT[role]}`,
    `${I}retry: {`,
    `${I}${I}max_attempts: ${s.retry.max}`,
    `${I}${I}delay: ${s.retry.delay}`,
    `${I}${I}retry_on: [${s.retry.on.map((v) => (typeof v === "number" ? String(v) : q(v))).join(", ")}]`,
    `${I}}`,
    `}`,
  );
  return { source: lines.join("\n"), companions: s.companions ?? [] };
}

/** The `schema:` expression for a declared schema, given the agent it goes to. */
export function schemaExprFor(schemaName: string, agent: { detail?: string; schema?: string } | undefined): string | null {
  if (agent?.schema === "none") return null;
  const command = (agent?.detail ?? "").split(/[\\/]/).pop();
  return `${schemaName}.${command === "codex" ? "path" : "content"}`;
}
