// The palette: Step Functions-style "states", each rendered as the `.mh`
// step it stands for. Indentation is 4 spaces, like the samples and skills.

import type { Declaration, ToolMethod } from "../lib/types";

export type StateKind =
  | "pass"
  | "agent"
  | "tool"
  | "choice"
  | "parallel"
  | "approval"
  | "succeed"
  | "fail";

export interface PaletteItem {
  kind: StateKind;
  label: string;
  hint: string;
}

/** Default step name prefix of each kind of new step. */
export const STATE_PREFIX: Record<StateKind, string> = {
  agent: "Ask",
  tool: "Call",
  pass: "Step",
  choice: "Choose",
  parallel: "Fan",
  approval: "Approve",
  succeed: "Done",
  fail: "Reject",
};

/**
 * The flow states the palette offers. "agent" and "tool" steps are not in
 * it: they are made by dragging an agent or a tool method onto the diagram.
 */
export const PALETTE: PaletteItem[] = [
  { kind: "pass", label: "Pass", hint: "An empty step — fill it in" },
  { kind: "choice", label: "Choice", hint: "goto match — branch on a value" },
  { kind: "parallel", label: "Parallel", hint: "parallel { … } — run branches concurrently" },
  { kind: "approval", label: "Human approval", hint: "pause(…) until resumed with a decision" },
  { kind: "succeed", label: "Succeed", hint: "complete() — end the run successfully" },
  { kind: "fail", label: "Fail", hint: "fail(…) — end the run with an error" },
];

export interface TemplateContext {
  name: string;
  /** Indentation of the step line itself. */
  indent: string;
  agents: Declaration[];
  tools: Declaration[];
  /** Names of the other top-level nodes (choice targets). */
  others: string[];
  /** For a "tool" step, the method to call (else the first one declared). */
  call?: { tool: string; method: ToolMethod };
  /** For an "agent" step, the agent to call (else the first one declared). */
  agent?: string;
  /** For an "agent" step, its `prompt:` expression (else a TODO string). */
  prompt?: string;
  /** For an "agent" step, a `schema:` expression to pass too. */
  schema?: string;
}

function defaultCall(tools: Declaration[]): TemplateContext["call"] {
  for (const t of tools) {
    const m = t.methods?.find((x) => !x.internal);
    if (m) return { tool: t.name, method: m };
  }
  return undefined;
}

/** A placeholder value of a declared type, for a generated call. */
export function placeholder(type: string | undefined): string {
  if (!type) return "null";
  if (type.endsWith("[]")) return "[]";
  switch (type) {
    case "string":
      return '""';
    case "number":
      return "0";
    case "bool":
      return "false";
    case "object":
      return "{}";
    default:
      return "null";
  }
}

/**
 * `Tool.method("", 0)` — tool methods bind positionally, so one placeholder
 * per required parameter, in order; defaulted ones are left out.
 */
export function callText(tool: string, m: ToolMethod): string {
  const args = m.args.filter((a) => a.default === undefined).map((a) => placeholder(a.type));
  return `${tool}.${m.name}(${args.join(", ")})`;
}

const IND = "    ";

function block(header: string, body: string[], indent: string): string {
  if (body.length === 0) return `${indent}${header} {\n${indent}}`;
  return `${indent}${header} {\n${body.map((l) => (l ? indent + IND + l : "")).join("\n")}\n${indent}}`;
}

/** The `.mh` text of a new state, starting at `ctx.indent` (no trailing newline). */
export function renderState(kind: StateKind, ctx: TemplateContext): string {
  const { name, indent } = ctx;
  switch (kind) {
    case "pass":
      return block(`step ${name}`, [], indent);
    case "agent": {
      const agent = ctx.agent ?? ctx.agents[0]?.name ?? "Agent";
      const schema = ctx.schema ? `, schema: ${ctx.schema}` : "";
      return block(`step ${name}`, [`var reply = ${agent}.run(prompt: ${ctx.prompt ?? '"TODO: describe the task"'}${schema})`, `log(reply)`], indent);
    }
    case "tool": {
      const call = ctx.call ?? defaultCall(ctx.tools);
      return block(`step ${name}`, [`var result = ${call ? callText(call.tool, call.method) : "Tool.method()"}`, `log(result)`], indent);
    }
    case "choice": {
      const [a, b] = ctx.others;
      return block(
        `step ${name}`,
        [
          `goto match "a" {`,
          `${IND}"a" -> ${a ?? 'fail("TODO: pick a target step")'}`,
          ...(b ? [`${IND}"b" -> ${b}`] : []),
          `${IND}_ -> fail("no branch matched")`,
          `}`,
        ],
        indent,
      );
    }
    case "parallel":
      return `${indent}parallel ${name} {\n${block(`step ${name}Left`, [], indent + IND)}\n${block(`step ${name}Right`, [], indent + IND)}\n${indent}}`;
    case "approval":
      return block(`step ${name}`, [`pause("waiting for approval")`], indent);
    case "succeed":
      return block(`step ${name}`, [`complete()`], indent);
    case "fail":
      return block(`step ${name}`, [`fail("rejected")`], indent);
  }
}

/** A fresh `prefix<N>` not in `taken`. */
export function uniqueName(prefix: string, taken: Iterable<string>): string {
  const used = new Set(taken);
  if (!used.has(prefix)) return prefix;
  for (let i = 2; ; i++) if (!used.has(`${prefix}${i}`)) return `${prefix}${i}`;
}

export function newWorkflowSource(name: string): string {
  return `workflow ${name} {
    description: "TODO: what this workflow does"

    input topic: string = "world"

    step Start {
        log("hello " + topic)
    }
}
`;
}

export const IDENT = /^[A-Za-z_][A-Za-z0-9_]*$/;
export const DURATION = /^[0-9]+(ms|s|m|h|d)$/;

export function newToolSource(name: string, indent = "", exported = false): string {
  return `${indent}${exported ? "export " : ""}tool ${name} {
${indent}    // Methods a step, an agent hook or another tool can call as ${name}.method(...).
${indent}    greet(name: string): string -> "hello " + name
${indent}}`;
}

/** What the explorer's "+" can create: a new file holding one declaration. */
export type NewFileKind = "workflow" | "tool" | "agent" | "prompt" | "prompt-md" | "schema-json" | "memory" | "mcp" | "type" | "enum" | "test" | "empty";

export interface NewFileItem {
  kind: NewFileKind;
  label: string;
  hint: string;
  icon: string;
  /** Suggested path, before the user types one. */
  path: string;
}

export const NEW_FILE_ITEMS: NewFileItem[] = [
  { kind: "workflow", label: "Workflow", hint: "steps, transitions and inputs", icon: "⇢", path: "workflow.mh" },
  { kind: "tool", label: "Tool", hint: "methods steps and agents call", icon: "🛠", path: "tools.mh" },
  { kind: "agent", label: "Agent…", hint: "ready-made: Claude, Codex, Copilot, Antigravity or Devin, as generator, coder or reader", icon: "⚙", path: "agents.mh" },
  { kind: "prompt", label: "Prompt", hint: "a reusable, parameterized prompt", icon: "❝", path: "prompts.mh" },
  { kind: "prompt-md", label: "Prompt (Markdown)", hint: "a prompt body in Markdown — drag it onto the diagram", icon: "M↓", path: "prompts/review.prompt.md" },
  { kind: "schema-json", label: "JSON Schema", hint: "the shape of an agent's answer — drag it onto a step", icon: "{ }", path: "schemas/answer.schema.json" },
  { kind: "memory", label: "Memory", hint: "state kept across steps and runs", icon: "▤", path: "memory.mh" },
  { kind: "mcp", label: "MCP server", hint: "extension mcp — call an MCP server's tools", icon: "⧉", path: "mcp.mh" },
  { kind: "type", label: "Type", hint: "an object shape for inputs and results", icon: "{}", path: "types.mh" },
  { kind: "enum", label: "Enum", hint: "a closed set of values", icon: "≡", path: "enums.mh" },
  { kind: "test", label: "Test", hint: "test { describe … } — run with mhl test", icon: "✓", path: "workflow_test.mh" },
  { kind: "empty", label: "Empty file", hint: "start from scratch", icon: "□", path: "untitled.mh" },
];

/** A PascalCase declaration name from a file path: `tools/order_utils.mh` → `OrderUtils`. */
export function declNameFor(path: string, fallback = "Main"): string {
  const base = path.split(/[\\/]/).pop()!.replace(/\.mh$/, "");
  const name = base.replace(/(^|[^A-Za-z0-9])([a-z])/g, (_, __, c: string) => c.toUpperCase()).replace(/[^A-Za-z0-9_]/g, "");
  if (!name) return fallback;
  return /^[A-Za-z_]/.test(name) ? name : `X${name}`;
}

/** The starting source of a new file of the given kind; `name` names its declaration. */
export function newFileSource(kind: NewFileKind, name: string): string {
  switch (kind) {
    case "workflow":
      return newWorkflowSource(name);
    case "tool":
      return `// \`export\` lets a workflow file use it: import { ${name} } from "./<this file>"\n` + newToolSource(name, "", true) + "\n";
    case "agent":
      return `// An agent runs a model CLI (or an endpoint). A step calls it with
// ${name}.run(prompt: "..."); stdin: "\${prompt}" sends that prompt on standard
// input — never in argv, which caps at ~32K characters on Windows and shows in \`ps\`.
// Ready-made agents per CLI and role: + → Agent in the explorer.
// \`export\` lets a workflow file use it: import { ${name} } from "./<this file>"
export agent ${name} {
    command: "claude"
    args: ["-p", "Read the content below and answer what it asks.", "--output-format", "text"]
    stdin: "\${prompt}"
    timeout: 5m
}
`;
    case "prompt":
      return `// A prompt is a reusable template, called with named arguments:
// Agent.run(prompt: ${name}(text: "...")).
export prompt ${name}(text: string, words: number = 50) {
    "Summarize the text below in at most \${words} words.

\${text}"
}
`;
    case "memory":
      return `// Memory keeps state across steps and runs: ${name}.set("key", value),
// ${name}.get("key"). type "json" persists to path.
export memory ${name} {
    type: "json"
    path: ".mhl/memory/${name.toLowerCase()}.json"
}
`;
    case "mcp":
      return `// An MCP server as an extension: ${name}.call("tool_name", { ...args }).
export extension mcp ${name} {
    transport: "stdio"
    command: "npx"
    args: ["-y", "@modelcontextprotocol/server-filesystem", "."]
}
`;
    case "type":
      return `// A type names an object shape, e.g. for \`input order: ${name}\` or a
// tool method's parameter.
export type ${name} = { id: string, total: number }
`;
    case "enum":
      return `// An enum is a closed set of values: ${name}.draft, ${name}.done.
export enum ${name} { draft, review, done }
`;
    case "test":
      return `// Run with: mhl test <this file>. Import what you test, e.g.
// import { MyWorkflow } from "./workflow.mh"
test ${name} {
    describe it_works {
        is_true(true)
    }
}
`;
    case "prompt-md":
      return newMarkdownPromptSource();
    case "schema-json":
      return newJsonSchemaSource();
    case "empty":
      return "";
  }
}

/** A new Markdown prompt body: `${name}` placeholders become its parameters. */
export function newMarkdownPromptSource(): string {
  return `---
description: Reviews a change and lists the problems it finds
---
Review the following \${language} code and list every problem you find,
most severe first. For each one, quote the line and say how to fix it.

\${code}
`;
}

/**
 * A new JSON Schema for an agent's structured answer — the shape engines
 * with strict structured output accept (every property required, no extra
 * ones), used as `Agent.run(prompt: ..., schema: Answer.content)`.
 */
export function newJsonSchemaSource(): string {
  return `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "title": "Answer",
  "type": "object",
  "properties": {
    "summary": { "type": "string", "description": "One paragraph answering the task" },
    "confidence": { "type": "number", "minimum": 0, "maximum": 1 },
    "findings": {
      "type": "array",
      "items": { "type": "string" }
    }
  },
  "required": ["summary", "confidence", "findings"],
  "additionalProperties": false
}
`;
}

/** The file a new item of `kind` named `rel` is created as: its extension added when missing. */
export function newFilePath(rel: string, kind: NewFileKind): string {
  if (kind === "prompt-md") return /\.md$/i.test(rel) ? rel : `${rel.replace(/\.prompt$/, "")}.prompt.md`;
  if (kind === "schema-json") return /\.json$/i.test(rel) ? rel : `${rel.replace(/\.schema$/, "")}.schema.json`;
  return rel.endsWith(".mh") ? rel : `${rel}.mh`;
}

/** A name not in `taken`: base, base2, base3, ... */
export function freshVar(base: string, taken: Iterable<string>): string {
  const used = new Set(taken);
  const b = IDENT.test(base) ? base : "result";
  if (!used.has(b)) return b;
  for (let i = 2; ; i++) if (!used.has(`${b}${i}`)) return `${b}${i}`;
}

/** `var <method> = Tool.method(…)` — the statement a dropped tool method adds to a step. */
export function toolCallStatement(tool: string, m: ToolMethod, taken: Iterable<string>): string {
  return `var ${freshVar(m.name, taken)} = ${callText(tool, m)}`;
}

/** `var reply = Agent.run(prompt: "…")` — the statement a dropped agent adds to a step. */
export function agentCallStatement(agent: string, taken: Iterable<string>, prompt = '"TODO: describe the task"', schema?: string): string {
  return `var ${freshVar("reply", taken)} = ${agent}.run(prompt: ${prompt}${schema ? `, schema: ${schema}` : ""})`;
}
