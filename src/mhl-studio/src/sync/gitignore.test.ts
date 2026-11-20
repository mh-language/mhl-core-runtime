import { describe, expect, it } from "vitest";
import { ignoreLine, isSecretEnv, withEnvIgnored } from "./gitignore";

describe("withEnvIgnored", () => {
  it("creates .gitignore for the root .env", () => {
    expect(withEnvIgnored(null, ".env")).toBe("# Secrets (added by mhl studio)\n.env\n");
  });

  it("appends after the existing lines, adding a missing final newline", () => {
    expect(withEnvIgnored("node_modules/", ".env")).toBe("node_modules/\n\n# Secrets (added by mhl studio)\n.env\n");
    expect(withEnvIgnored("dist\n", ".env.local")).toBe("dist\n\n# Secrets (added by mhl studio)\n/.env.local\n");
  });

  it("adds the header once", () => {
    const once = withEnvIgnored(null, ".env.local")!;
    expect(withEnvIgnored(once, "deploy/prod.env")).toBe("# Secrets (added by mhl studio)\n/.env.local\n/deploy/prod.env\n");
  });

  it("leaves a .gitignore that already covers the file alone", () => {
    for (const [text, rel] of [
      [".env\n", ".env"],
      ["/.env\n", ".env"],
      ["  .env  \r\n", ".env"],
      [".env*\n", ".env.local"],
      [".env.*\n", ".env.production"],
      ["*.env\n", "deploy/prod.env"],
      ["deploy/prod.env\n", "deploy/prod.env"],
    ]) {
      expect(withEnvIgnored(text, rel), `${rel} in ${JSON.stringify(text)}`).toBeNull();
    }
  });

  it("does not take a commented or partial line as covering", () => {
    expect(withEnvIgnored("# .env\n", ".env")).toContain("\n.env\n");
    expect(withEnvIgnored(".envrc\n", ".env")).toContain("\n.env\n");
  });

  it("never ignores a committed template or a non-env file", () => {
    expect(isSecretEnv(".env.example")).toBe(false);
    expect(isSecretEnv("config/.env.sample")).toBe(false);
    expect(withEnvIgnored(null, ".env.example")).toBeNull();
    expect(withEnvIgnored(null, "main.mh")).toBeNull();
  });

  it("anchors every file but the root .env", () => {
    expect(ignoreLine(".env")).toBe(".env");
    expect(ignoreLine("sub/.env")).toBe("/sub/.env");
    expect(ignoreLine("prod.env")).toBe("/prod.env");
  });
});
