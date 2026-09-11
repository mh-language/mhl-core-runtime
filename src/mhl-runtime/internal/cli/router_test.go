package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/cli"
)

// TestRunRouterDelegateSelectHookPicksAgent exercises `.delegate(...)`
// end-to-end through `mhl run` when the deterministic `select` hook resolves
// the agent — the router's decider would fail loudly if the LLM cascade
// ever ran, so a passing run proves it didn't.
func TestRunRouterDelegateSelectHookPicksAgent(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	src := `
agent Billing { command: "echo" args: ["billing-said:"] trace: true }
agent Support { command: "echo" args: ["support-said:"] trace: true }
agent BrokenDecider { command: "this-command-does-not-exist-and-must-never-run" }

router Frontdesk {
    agents: [Billing, Support]
    select: (prompt) -> {
        if (prompt == "invoice question") { return "Billing" }
        return null
    }
    decider: BrokenDecider
}

pipeline P {
    step S {
        var response = Frontdesk.delegate(prompt: "invoice question")
    }
}
`
	if err := os.WriteFile(main, []byte(src), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	var buf bytes.Buffer
	if err := cli.Run([]string{"run", main}, &buf); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(buf.String(), "agent Billing response:\nbilling-said: invoice question\n") {
		t.Errorf("unexpected output: %s", buf.String())
	}
}

// TestRunRouterDelegateCascadesToLLMDecision exercises the LLM decision
// cascade end-to-end: `select` is absent, so the router's `decider` agent
// decides, and the chosen agent then runs.
func TestRunRouterDelegateCascadesToLLMDecision(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	src := `
agent Billing { command: "echo" args: ["billing-said:"] }
agent Support { command: "echo" args: ["support-said:"] trace: true }
agent Decider { command: "sh" args: ["-c", "printf 'Support'"] }

router Frontdesk {
    agents: [Billing, Support]
    decider: Decider
}

pipeline P {
    step S {
        var response = Frontdesk.delegate(prompt: "my account is locked")
    }
}
`
	if err := os.WriteFile(main, []byte(src), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	var buf bytes.Buffer
	if err := cli.Run([]string{"run", main}, &buf); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(buf.String(), "agent Support response:\nsupport-said: my account is locked\n") {
		t.Errorf("unexpected output: %s", buf.String())
	}
}

// TestRunRouterDelegateSelectTypoErrorsClearly proves that a select hook
// returning a name that doesn't match any declared agent (e.g. a typo) is
// rejected with a specific, immediate error naming the bad value and the
// real agent names — not a silent misroute or a confusing "no command"
// error from an unconfigured decision call.
func TestRunRouterDelegateSelectTypoErrorsClearly(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	src := `
agent Billing { command: "echo" args: ["billing-said:"] }
agent Support { command: "echo" args: ["support-said:"] }

router Frontdesk {
    agents: [Billing, Support]
    select: (prompt) -> "Biling"
}

pipeline P {
    step S {
        var response = Frontdesk.delegate(prompt: "hi")
    }
}
`
	if err := os.WriteFile(main, []byte(src), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	var buf bytes.Buffer
	err := cli.Run([]string{"run", main}, &buf)
	if err == nil {
		t.Fatal("expected an error for a select return that names no declared agent")
	}
	if !strings.Contains(err.Error(), `select returned "Biling"`) || !strings.Contains(err.Error(), "Billing, Support") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestRunRouterDelegateSelectUsingNameofResolvesAndIsIDEFriendly exercises
// nameof(...) end-to-end inside a router's select hook: the returned name is
// validated (both at mhl lint time and here at run time) against the
// declared agents instead of being a free-floating string literal a typo
// could silently break, and `mhl lint` accepts it cleanly.
func TestRunRouterDelegateSelectUsingNameofResolvesAndIsIDEFriendly(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	src := `
agent Billing { command: "echo" args: ["billing-said:"] }
agent Support { command: "echo" args: ["support-said:"] trace: true }

router Frontdesk {
    agents: [Billing, Support]
    select: (prompt) -> {
        if (prompt.contains("invoice")) { return nameof(Billing) }
        return nameof(Support)
    }
}

pipeline P {
    step S {
        var response = Frontdesk.delegate(prompt: "my account is locked")
    }
}
`
	if err := os.WriteFile(main, []byte(src), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	var lintBuf bytes.Buffer
	if err := cli.Run([]string{"lint", main}, &lintBuf); err != nil {
		t.Fatalf("lint: %v (%s)", err, lintBuf.String())
	}

	var buf bytes.Buffer
	if err := cli.Run([]string{"run", main}, &buf); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(buf.String(), "agent Support response:\nsupport-said: my account is locked\n") {
		t.Errorf("unexpected output: %s", buf.String())
	}
}

// TestRunRouterDelegateSelectUsingNameofTypoIsRejectedByLint proves the
// typo-catching payoff: mhl lint rejects nameof(Biling) statically, before
// mhl run is ever invoked.
func TestRunRouterDelegateSelectUsingNameofTypoIsRejectedByLint(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	src := `
agent Billing { command: "echo" }

router Frontdesk {
    agents: [Billing]
    select: (prompt) -> nameof(Biling)
}
`
	if err := os.WriteFile(main, []byte(src), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	var buf bytes.Buffer
	err := cli.Run([]string{"lint", main}, &buf)
	if err == nil {
		t.Fatal("expected mhl lint to reject the typo'd nameof argument")
	}
	if !strings.Contains(buf.String(), `nameof: "Biling" is not a declared name`) {
		t.Errorf("unexpected lint output: %s", buf.String())
	}
}

// TestRunRouterDelegatePurelyDeterministicRouterNeedsNoDecisionEngine proves
// the LLM cascade is entirely optional end-to-end: a router that declares
// only `select` (no `decider` at all) still runs correctly through `mhl
// run`, and both `mhl lint` and `mhl run --dry-run` accept it without
// complaint.
func TestRunRouterDelegatePurelyDeterministicRouterNeedsNoDecisionEngine(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	src := `
agent Billing { command: "echo" args: ["billing-said:"] }
agent Support { command: "echo" args: ["support-said:"] trace: true }

router Frontdesk {
    agents: [Billing, Support]
    select: (prompt) -> {
        if (prompt.contains("invoice")) { return "Billing" }
        return "Support"
    }
}

pipeline P {
    step S {
        var response = Frontdesk.delegate(prompt: "my account is locked")
    }
}
`
	if err := os.WriteFile(main, []byte(src), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	var lintBuf bytes.Buffer
	if err := cli.Run([]string{"lint", main}, &lintBuf); err != nil {
		t.Fatalf("lint: %v (%s)", err, lintBuf.String())
	}

	var dryRunBuf bytes.Buffer
	if err := cli.Run([]string{"run", main, "--dry-run"}, &dryRunBuf); err != nil {
		t.Fatalf("dry-run: %v (%s)", err, dryRunBuf.String())
	}

	var buf bytes.Buffer
	if err := cli.Run([]string{"run", main}, &buf); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(buf.String(), "agent Support response:\nsupport-said: my account is locked\n") {
		t.Errorf("unexpected output: %s", buf.String())
	}
}

// TestRunRouterDelegateErrorsOnUndeclaredAgentRef proves an `agents: [...]`
// entry naming an undeclared agent fails the run with a clear error.
func TestRunRouterDelegateErrorsOnUndeclaredAgentRef(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	src := `
router Frontdesk {
    agents: [Nope]
}

pipeline P {
    step S {
        var response = Frontdesk.delegate(prompt: "hi")
    }
}
`
	if err := os.WriteFile(main, []byte(src), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	var buf bytes.Buffer
	err := cli.Run([]string{"run", main}, &buf)
	if err == nil {
		t.Fatal("expected an error for a router referencing an undeclared agent")
	}
	if !strings.Contains(err.Error(), `agent "Nope" is not declared`) {
		t.Errorf("unexpected error: %v", err)
	}
}
