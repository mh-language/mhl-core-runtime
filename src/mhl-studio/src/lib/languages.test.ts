import { describe, expect, it } from "vitest";
import { languageOf } from "./languages";

describe("languageOf", () => {
  it("goes by name, then extension", () => {
    expect(languageOf("tests/cli_test.py")).toBe("python");
    expect(languageOf("check.sh")).toBe("shellscript");
    expect(languageOf("ci/release.YML")).toBe("yaml");
    expect(languageOf("docker/Dockerfile")).toBe("dockerfile");
    expect(languageOf("Makefile")).toBe("make");
    expect(languageOf(".gitignore")).toBe("ini");
    expect(languageOf("LICENSE")).toBe("plaintext");
    expect(languageOf("examples/brief.txt")).toBe("plaintext");
  });
  it("reads a #! line when the name says nothing", () => {
    expect(languageOf("agilespec", "#!/usr/bin/env python3\nimport sys\n")).toBe("python");
    expect(languageOf("run", "#!/bin/bash\necho hi")).toBe("shellscript");
    expect(languageOf("tool", "#!/usr/bin/env node\n")).toBe("javascript");
    expect(languageOf("notes", "just text")).toBe("plaintext");
    // The name wins over the #! line.
    expect(languageOf("x.sh", "#!/usr/bin/env python3\n")).toBe("shellscript");
  });
});
