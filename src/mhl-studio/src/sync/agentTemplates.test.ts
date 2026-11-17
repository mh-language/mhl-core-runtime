import { execFile, execFileSync } from "node:child_process";
import { createServer, type IncomingMessage, type ServerResponse } from "node:http";
import { promisify } from "node:util";
import { chmodSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import { BACKENDS, agentName, renderAgent, schemaExprFor, schemaPlacement } from "./agentTemplates";
import { graphOf, MHL } from "./testutil";

const ALL = BACKENDS.flatMap((b) => b.roles.map((r) => [b.id, r] as const));
const CLIS = ALL.filter(([b]) => BACKENDS.find((x) => x.id === b)!.kind === "cli");
const APIS = ALL.filter(([b]) => BACKENDS.find((x) => x.id === b)!.kind === "api");

const has = (cmd: string) => {
  try {
    execFileSync("sh", ["-c", `command -v ${cmd}`]);
    return true;
  } catch {
    return false;
  }
};

describe.skipIf(!MHL)("agent templates", () => {
  it.each(ALL)("%s %s parses and lints clean, and the graph reads its shape", (backend, role) => {
    const t = renderAgent(backend, role);
    const g = graphOf(t.source);
    expect(g.ok, t.source).toBe(true);
    expect(g.diagnostics, t.source).toEqual([]);
    const d = g.declarations.find((x) => x.name === agentName(backend, role))!;
    expect(d).toMatchObject({ kind: "agent", detail: BACKENDS.find((b) => b.id === backend)!.command, schema: schemaPlacement(backend, role) });
    // The prompt never lands at the end of argv by accident: stdin, or an explicit "${prompt}".
    expect(t.source.includes('stdin: "${prompt}') || t.source.includes('"-p", "${prompt}"')).toBe(true);
    expect(t.companions.length > 0).toBe(backend === "devin");
  });

  it("every template runs: inputs, optional flags, prompt and schema reach the CLI", () => {
    const dir = mkdtempSync(join(tmpdir(), "mhl-agents-"));
    try {
      // A stand-in CLI that prints its arguments and its stdin.
      const fake = join(dir, "fakecli");
      writeFileSync(fake, '#!/bin/sh\nprintf "ARGS:"; for a in "$@"; do printf " [%s]" "$a"; done; printf "\\nSTDIN:"; cat\n');
      chmodSync(fake, 0o755);
      const blocks: string[] = [];
      const tests: string[] = [];
      for (const [backend, role] of CLIS) {
        const t = renderAgent(backend, role);
        for (const c of t.companions) writeFileSync(join(dir, c.name), c.content);
        blocks.push(t.source.replace(/command: "[^"]+"/, `command: ${JSON.stringify(fake)}`));
        const name = agentName(backend, role);
        const inputs = new Set([...t.source.matchAll(/^\s+input (\w+)/gm)].map((m) => m[1]));
        const placement = schemaPlacement(backend, role);
        const args = ['prompt: "THE-PROMPT"', 'model: "m1"'];
        if (placement !== "none") args.push('schema: "THE-SCHEMA"');
        if (inputs.has("effort")) args.push('effort: "high"');
        if (inputs.has("repo")) args.push('repo: "."');
        if (inputs.has("workdir")) args.push('workdir: "."');
        if (inputs.has("dirs")) args.push('dirs: ["/extra"]');
        const checks = ['is_true(r.contains("[--model] [m1]"))', 'is_true(r.contains("THE-PROMPT"))'];
        if (placement !== "none") checks.push('is_true(r.contains("THE-SCHEMA"))');
        if (inputs.has("dirs")) checks.push('is_true(r.contains("[--add-dir] [/extra]"))');
        if (inputs.has("effort")) {
          const flag = backend === "claude" ? "[--effort] [high]" : backend === "codex" ? "[model_reasoning_effort=high]" : "[--reasoning-effort] [high]";
          checks.push(`is_true(r.contains(${JSON.stringify(flag)}))`);
        }
        tests.push(`    describe ${name} {\n        var r = ${name}.run(${args.join(", ")})\n        ${checks.join("\n        ")}\n    }`);
      }
      // Without a model the flag is left out entirely.
      tests.push(`    describe empty_model_omits_the_flag {\n        var r = ClaudeReader.run(prompt: "p", repo: ".")\n        is_false(r.contains("--model"))\n    }`);
      writeFileSync(join(dir, "main.mh"), `${blocks.join("\n\n")}\n\ntest agent_templates {\n${tests.join("\n")}\n}\n`);
      const out = execFileSync(MHL!, ["test", "main.mh"], { cwd: dir, encoding: "utf8" });
      expect(out).toMatch(/ 0 failed, 0 incomplete — PASSED/);
    } finally {
      rmSync(dir, { recursive: true, force: true });
    }
  });
});

/**
 * A stand-in for the Claude Messages API and an OpenAI-compatible Chat
 * Completions API: checks the key header, answers 529/429 to the first two
 * calls of a "RATE" prompt, refuses a "REFUSE" prompt unless the request
 * carries Claude's server-side fallbacks (then answers through a fallback
 * block, as the API does), and otherwise echoes the prompt, the request's
 * other fields and its anthropic-beta header back as the answer's JSON text.
 */
function mockApi(): Promise<{ url: string; close: () => void; hits: Map<string, number> }> {
  const attempts = new Map<string, number>();
  const hits = new Map<string, number>();
  const server = createServer((req: IncomingMessage, res: ServerResponse) => {
    let raw = "";
    req.on("data", (c) => (raw += c));
    req.on("end", () => {
      const send = (code: number, body: unknown) => {
        // Each provider's request id header: request-id (Anthropic), x-request-id (OpenAI).
        const id = req.url === "/v1/messages" ? { "request-id": "req-claude" } : { "x-request-id": "req-openai" };
        res.writeHead(code, { "content-type": "application/json", ...id });
        res.end(JSON.stringify(body));
      };
      const body = JSON.parse(raw);
      const claude = req.url === "/v1/messages";
      const authed = claude ? req.headers["x-api-key"] === "test-key" && req.headers["anthropic-version"] === "2026-10-21" : req.headers.authorization === "Bearer test-key";
      if (!authed) return send(401, { type: "error", error: { type: "authentication_error", message: "bad key" } });
      const prompt: string = body.messages[0].content;
      hits.set(prompt, (hits.get(prompt) ?? 0) + 1);
      if (prompt.startsWith("RATE")) {
        const n = (attempts.get(prompt) ?? 0) + 1;
        attempts.set(prompt, n);
        if (n < 3)
          return claude
            ? send(529, { type: "error", error: { type: "overloaded_error", message: `attempt ${n}` } })
            : send(429, { error: { type: "requests", code: "rate_limit_exceeded", message: `Rate limit reached (attempt ${n})` } });
      }
      // Failures no retry may repeat, and one that every retry must.
      if (prompt === "QUOTA") return send(429, { error: { type: "insufficient_quota", code: "insufficient_quota", message: "You exceeded your current quota" } });
      if (prompt === "BAD") return send(400, { type: "error", error: { type: "invalid_request_error", message: "max_tokens: 500 is too large" } });
      if (prompt === "DOWN") return send(529, { type: "error", error: { type: "overloaded_error", message: "Overloaded" } });
      const { messages: _, ...request } = body;
      const text = JSON.stringify({ echo: prompt, request, beta: req.headers["anthropic-beta"] ?? "" });
      if (claude && prompt.startsWith("REFUSE")) {
        if (!body.fallbacks) return send(200, { type: "message", stop_reason: "refusal", content: [] });
        const fallback = { type: "fallback", from: { model: body.model }, to: { model: "claude-opus-4-8" } };
        return send(200, { type: "message", stop_reason: "end_turn", content: [fallback, { type: "thinking", thinking: "" }, { type: "text", text }] });
      }
      if (claude) send(200, { type: "message", stop_reason: "end_turn", content: [{ type: "thinking", thinking: "" }, { type: "text", text }] });
      else send(200, { choices: [{ finish_reason: "stop", message: { role: "assistant", content: text, refusal: null } }] });
    });
  });
  return new Promise((resolve) =>
    server.listen(0, "127.0.0.1", () => {
      const { port } = server.address() as { port: number };
      resolve({ url: `http://127.0.0.1:${port}/v1`, close: () => server.close(), hits });
    }),
  );
}

describe.skipIf(!MHL || !has("curl") || !has("jq"))("API agent templates (curl)", () => {
  it("send the prompt escaped, the schema and inputs in the provider's shape, retry 429/529, and return only the answer", async () => {
    const dir = mkdtempSync(join(tmpdir(), "mhl-api-agents-"));
    const api = await mockApi();
    try {
      // Every character a hand-built JSON body or a shell would trip on.
      // (.mh source text: \" and \n and \\ are mhl string escapes.)
      const tricky = 'He said \\"hi\\"\\nline 2: $(echo PWNED) `uname` $HOME \'single\' ünïcödé \\\\ backslash';
      writeFileSync(join(dir, "answer.schema.json"), '{"type": "object", "properties": {"echo": {"type": "string"}}, "required": ["echo"]}');
      const blocks = APIS.map(([b, r]) => renderAgent(b, r).source);
      const call = (b: string, r: string, prompt: string) => {
        const args = [`prompt: ${prompt}`, `base_url: ${JSON.stringify(api.url)}`];
        if (r === "generator") args.push("schema: Answer.content", 'effort: "low"');
        return `${agentName(b as never, r as never)}.run(${args.join(", ")})`;
      };
      const tests = APIS.map(([b, r]) => {
        const name = agentName(b, r);
        const checks = ["is_true(a.echo == tricky)", `is_true(json.parse(${call(b, r, `"RATE ${name}"`)}).echo == "RATE ${name}")`];
        if (r === "generator")
          checks.push(
            b === "claude-api"
              ? 'is_true(a.request.output_config.format.type == "json_schema")\n        is_true(a.request.output_config.effort == "low")'
              : 'is_true(a.request.response_format.json_schema.schema.required[0] == "echo")\n        is_true(a.request.response_format.json_schema.strict == true)\n        is_true(a.request.reasoning_effort == "low")',
          );
        else checks.push(b === "claude-api" ? 'is_true(a.request.model == "claude-opus-5-5")' : 'is_true(a.request.keys() == ["model"])\n        is_true(a.request.model == "gpt-6-astra")');
        if (b === "claude-api") {
          // Refusal fallbacks: on by default, a model id names one, "" turns them off (no beta header either).
          const url = JSON.stringify(api.url);
          checks.push(
            'is_true(a.request.fallbacks == "default")',
            'is_true(a.beta == "server-side-fallback-2026-07-01")',
            `is_true(json.parse(${call(b, r, '"REFUSE"')}).echo == "REFUSE")`,
            `var named = json.parse(${name}.run(prompt: "p", base_url: ${url}, fallbacks: "claude-opus-4-8"${r === "generator" ? ", schema: Answer.content" : ""}))`,
            'is_true(named.request.fallbacks[0].model == "claude-opus-4-8")',
            `var off = json.parse(${name}.run(prompt: "p", base_url: ${url}, fallbacks: ""${r === "generator" ? ", schema: Answer.content" : ""}))`,
            'is_false(off.request.keys().contains("fallbacks"))',
            'is_true(off.beta == "")',
          );
        }
        return `    describe ${name} {\n        var tricky = "${tricky}"\n        var a = json.parse(${call(b, r, "tricky")})\n        ${checks.join("\n        ")}\n    }`;
      });
      writeFileSync(join(dir, "main.mh"), `schema Answer from "answer.schema.json"\n\n${blocks.join("\n\n")}\n\ntest api_templates {\n${tests.join("\n")}\n}\n`);
      const { stdout } = await promisify(execFile)(MHL!, ["test", "main.mh"], {
        cwd: dir,
        encoding: "utf8",
        env: { ...process.env, ANTHROPIC_API_KEY: "test-key", OPENAI_API_KEY: "test-key" },
      });
      expect(stdout).toMatch(/ 0 failed, 0 incomplete — PASSED/);
    } finally {
      api.close();
      rmSync(dir, { recursive: true, force: true });
    }
  }, 60_000);

  it("retry only what is transient: never exhausted quota or a bad request, always overload and network failures", async () => {
    const dir = mkdtempSync(join(tmpdir(), "mhl-api-fail-"));
    const api = await mockApi();
    try {
      const run = (agent: string, prompt: string, url = api.url) =>
        `workflow W${prompt} {\n    step S {\n        log(${agent}.run(prompt: "${prompt}", base_url: ${JSON.stringify(url)}))\n    }\n}`;
      const sources = [renderAgent("claude-api", "text").source, renderAgent("openai-api", "text").source];
      // A port nothing listens on: the server is closed before the run.
      const closed = await mockApi();
      closed.close();
      const flows = [run("OpenAiText", "QUOTA"), run("ClaudeApiText", "BAD"), run("ClaudeApiText", "DOWN"), run("ClaudeApiText", "NET", closed.url)];
      writeFileSync(join(dir, "main.mh"), `${sources.join("\n\n")}\n\n${flows.join("\n\n")}\n`);
      const fail = async (wf: string) => {
        try {
          await promisify(execFile)(MHL!, ["run", "main.mh", "--workflow", wf], { cwd: dir, encoding: "utf8", env: { ...process.env, ANTHROPIC_API_KEY: "test-key", OPENAI_API_KEY: "test-key" } });
        } catch (e) {
          const { stdout, stderr } = e as { stdout: string; stderr: string };
          return stdout + stderr;
        }
        throw new Error(`${wf} should have failed`);
      };
      const quota = await fail("WQUOTA");
      expect(quota).toContain("HTTP 429 insufficient_quota insufficient_quota: You exceeded your current quota (request id req-openai)");
      expect(quota).not.toContain("[retry]");
      expect(api.hits.get("QUOTA")).toBe(1);
      const bad = await fail("WBAD");
      expect(bad).toContain("HTTP 400 invalid_request_error: max_tokens: 500 is too large (request id req-claude)");
      expect(api.hits.get("BAD")).toBe(1); // the 500 in its message must not count as a status
      const down = await fail("WDOWN");
      expect(down).toContain("HTTP 529 [retry] overloaded_error: Overloaded (request id req-claude)");
      expect(api.hits.get("DOWN")).toBe(3);
      expect(await fail("WNET")).toContain("[retry] network failure (curl exit 7)");
    } finally {
      api.close();
      rmSync(dir, { recursive: true, force: true });
    }
  }, 90_000);
});

describe.skipIf(!MHL || !has("jq"))("OpenAI-compatible template", () => {
  it("sends store: false to api.openai.com only", async () => {
    const dir = mkdtempSync(join(tmpdir(), "mhl-openai-store-"));
    try {
      // A curl stand-in on PATH: saves the request body, answers 200 with a header dump.
      const bin = join(dir, "bin");
      mkdirSync(bin);
      writeFileSync(
        join(bin, "curl"),
        '#!/bin/sh\nwhile [ $# -gt 0 ]; do [ "$1" = "-D" ] && { shift; printf \'HTTP/1.1 200 OK\\r\\n\' > "$1"; }; shift; done\n' +
          'cat > "$CAPTURE"\nprintf \'{"choices":[{"finish_reason":"stop","message":{"content":"ok","refusal":null}}]}\\n200\'\n',
      );
      chmodSync(join(bin, "curl"), 0o755);
      const urls = { official: "https://api.openai.com/v1", other: "https://llm.example.com/v1", lookalike: "https://api.openai.com.evil.test/v1" };
      const flows = Object.entries(urls).map(([k, u]) => `workflow ${k} {\n    step S {\n        log(OpenAiText.run(prompt: "p", base_url: ${JSON.stringify(u)}))\n    }\n}`);
      writeFileSync(join(dir, "main.mh"), `${renderAgent("openai-api", "text").source}\n\n${flows.join("\n\n")}\n`);
      const sent: Record<string, Record<string, unknown>> = {};
      for (const k of Object.keys(urls)) {
        const capture = join(dir, `${k}.json`);
        await promisify(execFile)(MHL!, ["run", "main.mh", "--workflow", k], {
          cwd: dir,
          env: { ...process.env, PATH: `${bin}:${process.env.PATH}`, OPENAI_API_KEY: "k", CAPTURE: capture },
        });
        sent[k] = JSON.parse(readFileSync(capture, "utf8"));
      }
      expect(sent.official.store).toBe(false);
      expect(sent.other).not.toHaveProperty("store");
      expect(sent.lookalike).not.toHaveProperty("store");
    } finally {
      rmSync(dir, { recursive: true, force: true });
    }
  });
});

describe("schemaExprFor", () => {
  it("picks the schema's text, its path for Codex, nothing when the agent ignores it", () => {
    expect(schemaExprFor("Answer", { detail: "claude", schema: "args" })).toBe("Answer.content");
    expect(schemaExprFor("Answer", { detail: "/usr/local/bin/codex", schema: "args" })).toBe("Answer.path");
    expect(schemaExprFor("Answer", { detail: "copilot", schema: "stdin" })).toBe("Answer.content");
    expect(schemaExprFor("Answer", { detail: "devin", schema: "none" })).toBeNull();
    expect(schemaExprFor("Answer", undefined)).toBe("Answer.content");
  });
});
