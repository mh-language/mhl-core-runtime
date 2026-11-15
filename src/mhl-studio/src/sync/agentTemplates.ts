// Ready-to-use agent templates: five agent CLIs × three roles, each with the
// flags that make that role safe and predictable on that CLI. Distilled from
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

export type Backend = "claude" | "codex" | "copilot" | "antigravity" | "devin";
export type Role = "generator" | "coder" | "reader";

export interface BackendInfo {
  id: Backend;
  label: string;
  /** The executable the agent runs (and that must be on PATH). */
  command: string;
  /** Prefix of the default agent name: Claude → ClaudeCoder. */
  prefix: string;
  /** The CLI version the flags were checked against. */
  checked: string;
}

export const BACKENDS: BackendInfo[] = [
  { id: "claude", label: "Claude Code", command: "claude", prefix: "Claude", checked: "claude 2.1.278" },
  { id: "codex", label: "Codex CLI", command: "codex", prefix: "Codex", checked: "codex-cli 0.147.0" },
  { id: "copilot", label: "GitHub Copilot CLI", command: "copilot", prefix: "Copilot", checked: "copilot 1.0.110" },
  { id: "antigravity", label: "Antigravity (agy)", command: "agy", prefix: "Antigravity", checked: "agy 1.5.8" },
  { id: "devin", label: "Devin CLI", command: "devin", prefix: "Devin", checked: "devin 3000.12.43" },
];

export const ROLES: { id: Role; label: string; hint: string; suffix: string }[] = [
  { id: "generator", label: "Structured answer", hint: "no tools; answers in JSON shaped by schema:", suffix: "Generator" },
  { id: "coder", label: "Coder", hint: "reads, edits and runs builds/tests in a repository", suffix: "Coder" },
  { id: "reader", label: "Reader", hint: "only reads a repository (and extra folders)", suffix: "Reader" },
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

const TIMEOUT: Record<Role, string> = { generator: "10m", reader: "20m", coder: "60m" };

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
  }
}

/** The default declaration name: ClaudeCoder, CodexGenerator, ... */
export function agentName(backend: Backend, role: Role): string {
  return BACKENDS.find((b) => b.id === backend)!.prefix + ROLES.find((r) => r.id === role)!.suffix;
}

/** Where the agent puts a call's `schema:`, as `mhl graph` reports it (Declaration.schema). */
export function schemaPlacement(backend: Backend, role: Role): "args" | "stdin" | "none" {
  if (role !== "generator" || backend === "devin") return "none";
  return backend === "copilot" ? "stdin" : "args";
}

/** Renders one template as mhl source. */
export function renderAgent(backend: Backend, role: Role, opts: RenderOptions = {}): AgentTemplate {
  const info = BACKENDS.find((b) => b.id === backend)!;
  const roleInfo = ROLES.find((r) => r.id === role)!;
  const s = spec(backend, role, opts.dir ?? "");
  const name = opts.name ?? agentName(backend, role);
  const I = "    ";
  const lines: string[] = [
    `// ${info.label} — ${roleInfo.label.toLowerCase()} (${roleInfo.hint})`,
    ...s.notes.map((n) => `// ${n}`),
    `// Flags checked against ${info.checked}; if a call fails after a CLI update, compare with \`${info.command} --help\`.`,
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
