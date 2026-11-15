import { execFileSync } from "node:child_process";
import { chmodSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import { BACKENDS, ROLES, agentName, renderAgent, schemaExprFor, schemaPlacement } from "./agentTemplates";
import { graphOf, MHL } from "./testutil";

const ALL = BACKENDS.flatMap((b) => ROLES.map((r) => [b.id, r.id] as const));

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
      for (const [backend, role] of ALL) {
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

describe("schemaExprFor", () => {
  it("picks the schema's text, its path for Codex, nothing when the agent ignores it", () => {
    expect(schemaExprFor("Answer", { detail: "claude", schema: "args" })).toBe("Answer.content");
    expect(schemaExprFor("Answer", { detail: "/usr/local/bin/codex", schema: "args" })).toBe("Answer.path");
    expect(schemaExprFor("Answer", { detail: "copilot", schema: "stdin" })).toBe("Answer.content");
    expect(schemaExprFor("Answer", { detail: "devin", schema: "none" })).toBeNull();
    expect(schemaExprFor("Answer", undefined)).toBe("Answer.content");
  });
});
