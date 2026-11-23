package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/cli"
)

// runVariantProgram writes src as main.mh (plus extra files) into a temp dir
// and runs it with `mhl run`, returning the output.
func runVariantProgram(t *testing.T, src string, files map[string]string) (string, error) {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	main := filepath.Join(dir, "main.mh")
	if err := os.WriteFile(main, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	err := cli.Run([]string{"run", main}, &buf)
	return buf.String(), err
}

// variantPrompt and the cat-based agents echo back exactly what reached
// their stdin, so each test reads which arm an agent was handed.
const variantPrompt = `
prompt Review(file: string) {
    default            { "DEFAULT ${file}" }
    for "claude/*"     { "CLAUDE ${file}" }
    for "claude/opus*" { "OPUS ${file}" }
    for "cli/codex"    { "CODEX ${file}" }
}

agent ByInput {
    input model: string = ""
    command: "sh"
    args: ["-c", "cat"]
    stdin: "${prompt.target(model)}"
}

agent ByExpr {
    input model: string = ""
    command: "sh"
    args: ["-c", "cat"]
    stdin: prompt.target("claude/" + model)
}

agent Plain {
    command: "sh"
    args: ["-c", "cat"]
    stdin: "${prompt}"
}
`

func TestPromptVariantSelectedByAgentTarget(t *testing.T) {
	out, err := runVariantProgram(t, variantPrompt+`
pipeline P {
    step S {
        log("1=" + ByInput.run(prompt: Review(file: "a"), model: "claude/sonnet"))
        log("2=" + ByInput.run(prompt: Review(file: "a"), model: "claude/opus-5"))
        log("3=" + ByInput.run(prompt: Review(file: "a"), model: "cli/codex"))
        log("4=" + ByInput.run(prompt: Review(file: "a"), model: "ollama/qwen"))
        log("5=" + ByExpr.run(prompt: Review(file: "b"), model: "opus-4"))
        log("6=" + Plain.run(prompt: Review(file: "c")))
    }
}
`, nil)
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	for _, want := range []string{"1=CLAUDE a", "2=OPUS a", "3=CODEX a", "4=DEFAULT a", "5=OPUS b", "6=DEFAULT c"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

// A prompt rendered in one step, kept in a pipeline var and handed through a
// tool parameter typed `string`, still carries its variants; any string
// operation on it is the default text.
func TestPromptVariantSurvivesVarsToolsAndSteps(t *testing.T) {
	out, err := runVariantProgram(t, variantPrompt+`
tool Writer {
    generate(prompt: string, model: string): string -> ByInput.run(prompt:, model:)
}

pipeline P {
    var p = ""
    step Render { p = Review(file: "x") }
    step Use {
        log("tool=" + Writer.generate(p, "cli/codex"))
        log("concat=" + ByInput.run(prompt: p + "!", model: "cli/codex"))
        log("method=" + p.target("claude/opus"))
        log("plain=" + "literal".target("cli/codex"))
        log("eq=" + (if (p == "DEFAULT x") "yes" else "no"))
    }
}
`, nil)
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	for _, want := range []string{"tool=CODEX x", "concat=DEFAULT x!", "method=OPUS x", "plain=literal", "eq=yes"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

// A prompt passed as another prompt's argument follows the same target.
func TestPromptVariantNestedFollowsTarget(t *testing.T) {
	out, err := runVariantProgram(t, variantPrompt+`
prompt Wrap(inner: string) { "<${inner}>" }

pipeline P {
    step S {
        log("nested=" + ByInput.run(prompt: Wrap(inner: Review(file: "n")), model: "cli/codex"))
        log("default=" + Plain.run(prompt: Wrap(inner: Review(file: "n"))))
    }
}
`, nil)
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	for _, want := range []string{"nested=<CODEX n>", "default=<DEFAULT n>"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

// Each fallback leg evaluates its own stdin, so it picks its own variant.
func TestPromptVariantFallbackPicksOwnVariant(t *testing.T) {
	out, err := runVariantProgram(t, variantPrompt+`
agent Failing {
    command: "sh"
    args: ["-c", "exit 1"]
    stdin: "${prompt.target(\"claude/sonnet\")}"
    fallback: [Codex]
}

agent Codex {
    command: "sh"
    args: ["-c", "cat"]
    stdin: "${prompt.target(\"cli/codex\")}"
    trace: true
}

pipeline P {
    step S { log("fb=" + Failing.run(prompt: Review(file: "f"))) }
}
`, nil)
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	if !strings.Contains(out, "fb=CODEX f") {
		t.Errorf("fallback did not pick its own variant:\n%s", out)
	}
	if !strings.Contains(out, `agent Codex: prompt Review → for "cli/codex" (target "cli/codex")`) {
		t.Errorf("trace missing the variant pick:\n%s", out)
	}
}

// An expression-form stdin that reads `prompt` is the prompt's destination:
// the text must not also be appended to argv.
func TestPromptVariantExpressionStdinNotAppendedToArgv(t *testing.T) {
	out, err := runVariantProgram(t, variantPrompt+`
agent Argv {
    command: "sh"
    args: ["-c", "printf 'argc=%s;' \"$#\"; cat", "sh"]
    stdin: prompt.target("cli/codex")
}

pipeline P {
    step S { log("r=" + Argv.run(prompt: Review(file: "z"))) }
}
`, nil)
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	if !strings.Contains(out, "r=argc=0;CODEX z") {
		t.Errorf("prompt reached argv too, or wrong variant:\n%s", out)
	}
}

// Variant arms load from Markdown files relative to the declaring module,
// frontmatter stripped, like a plain `from` prompt.
func TestPromptVariantArmsFromFiles(t *testing.T) {
	out, err := runVariantProgram(t, `
import { Review } from "lib/prompts.mh"

agent ByInput {
    input model: string = ""
    command: "sh"
    args: ["-c", "cat"]
    stdin: "${prompt.target(model)}"
}

pipeline P {
    step S {
        log("d=" + ByInput.run(prompt: Review(file: "m")))
        log("c=" + ByInput.run(prompt: Review(file: "m"), model: "claude/x"))
    }
}
`, map[string]string{
		"lib/prompts.mh":       "export prompt Review(file: string) {\n    default from \"review.md\"\n    for \"claude/*\" from \"review.claude.md\"\n}\n",
		"lib/review.md":        "plain review of ${file}",
		"lib/review.claude.md": "---\nstyle: xml\n---\n<review>${file}</review>",
	})
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	for _, want := range []string{"d=plain review of m", "c=<review>m</review>"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestPromptVariantMissingArmFileFails(t *testing.T) {
	_, err := runVariantProgram(t, `
prompt Review(file: string) {
    default { "d ${file}" }
    for "claude/*" from "missing.md"
}
pipeline P { step S { log(Review(file: "a")) } }
`, nil)
	if err == nil || !strings.Contains(err.Error(), `prompt "Review" (for "claude/*") from "missing.md"`) {
		t.Fatalf("err = %v, want the missing arm file named", err)
	}
}

// Two renderings sharing a default text (the default ignores a parameter an
// arm uses) cannot be told apart: picking that arm is an error, not a guess.
func TestPromptVariantAmbiguousDefaultTextFails(t *testing.T) {
	out, err := runVariantProgram(t, `
prompt Ask(topic: string) {
    default { "Answer the question." }
    for "claude/*" { "Answer about ${topic}." }
}
agent ByInput {
    input model: string = ""
    command: "sh"
    args: ["-c", "cat"]
    stdin: "${prompt.target(model)}"
}
pipeline P {
    step S {
        var a = Ask(topic: "go")
        var b = Ask(topic: "rust")
        log(ByInput.run(prompt: a, model: "claude/x"))
    }
}
`, nil)
	if err == nil || !strings.Contains(err.Error()+out, "two renderings share the same default text") {
		t.Fatalf("err = %v, want an ambiguity error\n%s", err, out)
	}
}

// The response cache tells two variants of the same prompt apart.
func TestPromptVariantCacheKeyedByVariant(t *testing.T) {
	out, err := runVariantProgram(t, variantPrompt+`
agent Cached {
    input model: string = ""
    command: "sh"
    args: ["-c", "cat"]
    stdin: "${prompt.target(model)}"
    cache: { enabled: true, ttl: 1h }
}

pipeline P {
    step S {
        log("a=" + Cached.run(prompt: Review(file: "k"), model: "cli/codex"))
        log("b=" + Cached.run(prompt: Review(file: "k"), model: "claude/x"))
    }
}
`, nil)
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	for _, want := range []string{"a=CODEX k", "b=CLAUDE k"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

// A router's select hook sees the default text (routers have no target);
// the agent it picks then chooses its own variant.
func TestPromptVariantRouterSelectSeesDefault(t *testing.T) {
	out, err := runVariantProgram(t, variantPrompt+`
router Desk {
    agents: [ByExprCodex, Plain]
    select: (prompt) -> {
        if (prompt.starts_with("DEFAULT")) return nameof(ByExprCodex)
        return nameof(Plain)
    }
}

agent ByExprCodex {
    command: "sh"
    args: ["-c", "cat"]
    stdin: prompt.target("cli/codex")
}

pipeline P {
    step S { log("routed=" + Desk.delegate(prompt: Review(file: "r"))) }
}
`, nil)
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	if !strings.Contains(out, "routed=CODEX r") {
		t.Errorf("router: want select to see the default text and the agent its variant:\n%s", out)
	}
}
