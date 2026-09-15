package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/cli"
)

const codeReviewSkillFile = `---
name: code-review
description: Review a diff for correctness issues
---
Look for null derefs and off-by-one errors.
`

const securityReviewSkillFile = `---
name: security-review
description: Review a diff for security issues
---
Look for injection and auth bypass issues.
`

// skillReviewerAgentFile declares Reviewer with a skills allow-list and a
// system_prompt hook that: (1) proves nameof(...) identity comparison works
// against a skill's `name` field (the MHL declaration identifier, not the
// frontmatter name), and (2) proves a for-in loop over the `skills`
// parameter can pull frontmatter and content out of each selected skill —
// the whole point of exposing a list of objects instead of a pre-joined
// string.
const skillReviewerAgentFile = `
skill CodeReview from "code-review.md"
skill SecurityReview from "security-review.md"

agent Reviewer {
    command: "echo"
    args: ["${prompt}"]
    skills: [CodeReview, SecurityReview]

    system_prompt: (skills, prompt) -> {
        var instructions = ""
        for (var s in skills) {
            var tag = "other"
            if (s.name == nameof(CodeReview)) { tag = "code" }
            instructions = instructions + "[" + tag + ":" + s.frontmatter.name + "] " + s.content + " "
        }
        return instructions + "REQUEST:" + prompt
    }
}
`

func writeSkillTestFiles(t *testing.T, dir string, agentFile string) {
	t.Helper()
	files := map[string]string{
		"code-review.md":     codeReviewSkillFile,
		"security-review.md": securityReviewSkillFile,
		"agent.mh":           agentFile,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
}

func runSkillDir(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	var buf bytes.Buffer
	err = cli.Run(args, &buf)
	return buf.String(), err
}

// TestRunSkillsComposeSystemPromptWithNameofAndFrontmatter is the core
// end-to-end proof: two skills selected at a call site reach system_prompt
// as a list of {name, frontmatter, content} objects, in call order;
// nameof(CodeReview) identifies a skill by its MHL declaration name (not
// its frontmatter name); frontmatter.name is readable inside the hook; and
// the hook's returned string — not the raw prompt — is what actually
// reaches the "agent" (here, echo, so we can see exactly what was sent).
func TestRunSkillsComposeSystemPromptWithNameofAndFrontmatter(t *testing.T) {
	dir := t.TempDir()
	writeSkillTestFiles(t, dir, skillReviewerAgentFile+`
pipeline ReviewPR {
    step Review {
        var result = Reviewer.run(skills: [CodeReview, SecurityReview], prompt: "check this diff")
        log(result)
    }
}
`)
	out, err := runSkillDir(t, dir, "run", "agent.mh")
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	for _, want := range []string{
		"[code:code-review] Look for null derefs",
		"[other:security-review] Look for injection",
		"REQUEST:check this diff",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q, got: %s", want, out)
		}
	}
}

// TestRunSkillsCallWithNoSkillsSelectedLeavesPromptUnchanged proves a call
// that never mentions `skills:` never invokes system_prompt at all — the
// request sent is the plain prompt, identical to an agent with no skills
// feature in play, even though Reviewer declares both skills: and
// system_prompt.
func TestRunSkillsCallWithNoSkillsSelectedLeavesPromptUnchanged(t *testing.T) {
	dir := t.TempDir()
	writeSkillTestFiles(t, dir, skillReviewerAgentFile+`
pipeline ReviewPR {
    step Review {
        var result = Reviewer.run(prompt: "check this diff")
        log(result)
    }
}
`)
	out, err := runSkillDir(t, dir, "run", "agent.mh")
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	if strings.Contains(out, "REQUEST:") || strings.Contains(out, "code-review") {
		t.Errorf("system_prompt should not have run for a call with no skills selected, got: %s", out)
	}
	if !strings.Contains(out, "check this diff") {
		t.Errorf("output missing the plain prompt, got: %s", out)
	}
}

// TestRunSkillNotPermittedFailsBeforeCallingTheModel proves a skill that
// isn't in the agent's allow-list is rejected before any model call, with a
// message naming what is actually permitted.
func TestRunSkillNotPermittedFailsBeforeCallingTheModel(t *testing.T) {
	dir := t.TempDir()
	writeSkillTestFiles(t, dir, `
skill CodeReview from "code-review.md"
skill SecurityReview from "security-review.md"

agent Reviewer {
    command: "echo"
    args: ["${prompt}"]
    skills: [CodeReview]
    system_prompt: (skills, prompt) -> prompt
}

pipeline ReviewPR {
    step Review {
        var result = Reviewer.run(skills: [SecurityReview], prompt: "check this diff")
        log(result)
    }
}
`)
	out, err := runSkillDir(t, dir, "run", "agent.mh")
	if err == nil {
		t.Fatalf("expected an error, got output: %s", out)
	}
	if !strings.Contains(err.Error(), `skill "SecurityReview" not permitted`) {
		t.Errorf("error missing expected message, got: %v", err)
	}
}

// TestLintRequiresSystemPromptWhenAgentDeclaresSkills proves `mhl lint`
// catches a skills-capable agent with no way to receive them — before any
// pipeline even tries to call it.
func TestLintRequiresSystemPromptWhenAgentDeclaresSkills(t *testing.T) {
	dir := t.TempDir()
	writeSkillTestFiles(t, dir, `
skill CodeReview from "code-review.md"

agent Reviewer {
    command: "echo"
    args: ["${prompt}"]
    skills: [CodeReview]
}
`)
	out, err := runSkillDir(t, dir, "lint", dir)
	if err == nil {
		t.Fatalf("expected lint to fail, got output: %s", out)
	}
	if !strings.Contains(out, "declares skills but no system_prompt") {
		t.Errorf("output missing expected finding: %s", out)
	}
}

// TestLintCatchesTypoedNameofInsideSystemPrompt proves system_prompt's body
// is statically walked exactly like a router's select hook: a typo'd
// nameof(...) argument is a lint-time error, not just a runtime one.
func TestLintCatchesTypoedNameofInsideSystemPrompt(t *testing.T) {
	dir := t.TempDir()
	writeSkillTestFiles(t, dir, `
skill CodeReview from "code-review.md"

agent Reviewer {
    command: "echo"
    args: ["${prompt}"]
    skills: [CodeReview]
    system_prompt: (skills, prompt) -> {
        if (prompt == "") { return nameof(CodeReviewTypo) }
        return prompt
    }
}
`)
	out, err := runSkillDir(t, dir, "lint", dir)
	if err == nil {
		t.Fatalf("expected lint to fail, got output: %s", out)
	}
	if !strings.Contains(out, `nameof: "CodeReviewTypo" is not a declared name`) {
		t.Errorf("output missing expected finding: %s", out)
	}
}

// TestRunPromptFrontmatterIsOptionalAndBackwardCompatible proves a `prompt
// ... from` file with no frontmatter block behaves exactly as it always
// has, and a file that does have one exposes it via PromptName.frontmatter,
// both by field access and by dynamic key.
func TestRunPromptFrontmatterIsOptionalAndBackwardCompatible(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"plain.prompt.md": "Just a plain prompt body, no frontmatter here.",
		"tagged.prompt.md": `---
version: 2
---
A tagged prompt body.`,
		"main.mh": `
prompt Plain() from "plain.prompt.md"
prompt Tagged() from "tagged.prompt.md"
` + wrapStep(`
        log("plain=" + Plain())
        log("tagged=" + Tagged())
        log("version_field", Tagged.frontmatter.version)
        log("version_index", Tagged.frontmatter["version"])
    `),
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	out, err := runSkillDir(t, dir, "run", "main.mh")
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	for _, want := range []string{
		"plain=Just a plain prompt body, no frontmatter here.",
		"tagged=A tagged prompt body.",
		"version_field 2",
		"version_index 2",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q, got: %s", want, out)
		}
	}
}
