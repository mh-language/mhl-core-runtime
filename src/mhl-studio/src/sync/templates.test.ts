import { execFileSync } from "node:child_process";
import { mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import { promptParams } from "./prompts";
import { declNameFor, isEnvName, NEW_FILE_ITEMS, newFilePath, newFileSource } from "./templates";
import { graphOf, MHL } from "./testutil";

describe("declNameFor", () => {
  it("derives a PascalCase name from the path", () => {
    expect(declNameFor("tools/order_utils.mh")).toBe("OrderUtils");
    expect(declNameFor("flows/review-loop.mh")).toBe("ReviewLoop");
    expect(declNameFor("2024.mh")).toBe("X2024");
    expect(declNameFor("---.mh")).toBe("Main");
    expect(declNameFor("tools/order_utils.mhl")).toBe("OrderUtils");
  });
});

describe("env files", () => {
  it("names", () => {
    for (const yes of [".env", ".env.local", "conf/.env.example", "prod.env"]) expect(isEnvName(yes), yes).toBe(true);
    for (const no of [".envrc", "env", ".env-old", "a/.environment", "x.mh"]) expect(isEnvName(no), no).toBe(false);
  });
  it("keeps an env name, else appends .env", () => {
    expect(newFilePath(".env", "env")).toBe(".env");
    expect(newFilePath(".env.local", "env")).toBe(".env.local");
    expect(newFilePath("deploy/prod", "env")).toBe("deploy/prod.env");
  });
});

describe("mhl source files", () => {
  it("keeps .mh or the .mhl alias, else appends .mh", () => {
    expect(newFilePath("flows/a.mh", "workflow")).toBe("flows/a.mh");
    expect(newFilePath("flows/a.mhl", "workflow")).toBe("flows/a.mhl");
    expect(newFilePath("flows/a", "workflow")).toBe("flows/a.mh");
  });
});

describe.skipIf(!MHL)("new file templates", () => {
  for (const item of NEW_FILE_ITEMS.filter((i) => i.kind !== "prompt-md" && i.kind !== "schema-json" && i.kind !== "env")) {
    it(`${item.label} parses and lints clean`, () => {
      const src = newFileSource(item.kind, "Sample");
      const g = graphOf(src);
      expect(g.ok, src).toBe(true);
      expect(g.diagnostics, src).toEqual([]);
      if (item.kind === "empty") return;
      const declared = [...g.declarations.map((d) => d.name), ...g.workflows.map((w) => w.name)];
      expect(declared).toContain("Sample");
    });
  }

  it("Environment (.env) is loaded by mhl from the workspace root", () => {
    const dir = mkdtempSync(join(tmpdir(), "mhl-env-"));
    try {
      writeFileSync(join(dir, ".env"), `${newFileSource("env", "Sample")}MHL_STUDIO_ENV_PROBE="from .env"\n`);
      writeFileSync(
        join(dir, "main.mh"),
        `test t {\n    describe d {\n        are_equal(env("MHL_STUDIO_ENV_PROBE", "unset"), "from .env")\n    }\n}\n`,
      );
      const { MHL_STUDIO_ENV_PROBE: _, ...env } = process.env;
      expect(execFileSync(MHL!, ["test", "main.mh"], { cwd: dir, env, encoding: "utf8" })).toContain("1 passed, 0 failed");
    } finally {
      rmSync(dir, { recursive: true, force: true });
    }
  });

  it("JSON Schema loads with mhl as a strict object schema", () => {
    const text = newFileSource("schema-json", "Sample");
    const parsed = JSON.parse(text);
    expect(parsed).toMatchObject({ type: "object", additionalProperties: false });
    // Claude Code's --json-schema rejects the 2020-12 meta-schema it would name.
    expect(parsed).not.toHaveProperty("$schema");
    expect([...parsed.required].sort()).toEqual(Object.keys(parsed.properties).sort());
    expect(newFilePath("schemas/answer", "schema-json")).toBe("schemas/answer.schema.json");
    expect(newFilePath("data.json", "schema-json")).toBe("data.json");
    const dir = mkdtempSync(join(tmpdir(), "mhl-schema-"));
    try {
      writeFileSync(join(dir, "answer.schema.json"), text);
      writeFileSync(
        join(dir, "main.mh"),
        `schema Answer from "./answer.schema.json"\n\ntest t {\n    describe d {\n        are_equal(json.parse(Answer.content).type, "object")\n    }\n}\n`,
      );
      expect(execFileSync(MHL!, ["test", join(dir, "main.mh")], { encoding: "utf8" })).toContain("1 passed, 0 failed");
    } finally {
      rmSync(dir, { recursive: true, force: true });
    }
  });

  it("Prompt (Markdown) declares its placeholders and renders with mhl", () => {
    const md = newFileSource("prompt-md", "Sample");
    expect(promptParams(md)).toEqual(["language", "code"]);
    expect(newFilePath("prompts/review", "prompt-md")).toBe("prompts/review.prompt.md");
    expect(newFilePath("notes.md", "prompt-md")).toBe("notes.md");
    expect(newFilePath("flows/main", "workflow")).toBe("flows/main.mh");
    // The template, used as `prompt X(...) from "file"`, renders through the real runtime.
    const dir = mkdtempSync(join(tmpdir(), "mhl-md-"));
    try {
      writeFileSync(join(dir, "review.prompt.md"), md);
      writeFileSync(
        join(dir, "main.mh"),
        `prompt Review(language: string, code: string) from "./review.prompt.md"\n\ntest t {\n    describe d {\n        var r = Review(language: "go", code: "x := 1")\n        are_equal(r.contains("Review the following go code"), true)\n    }\n}\n`,
      );
      const out = execFileSync(MHL!, ["test", join(dir, "main.mh")], { encoding: "utf8" });
      expect(out).toContain("1 passed, 0 failed");
    } finally {
      rmSync(dir, { recursive: true, force: true });
    }
  });
});
