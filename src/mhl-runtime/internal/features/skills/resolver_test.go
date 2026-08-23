package skills_test

import (
	"sort"
	"strings"
	"testing"

	"github.com/yanjustino/mhl-runtime/internal/features/skills"
	"github.com/yanjustino/mhl-runtime/internal/lang/ast"
	"github.com/yanjustino/mhl-runtime/internal/lang/parser"
)

// projectSource declares an agent whose tools/mcp_servers are deliberately
// broader than the skill it invokes, plus that skill, exactly as in §3.2/§3.4.
const projectSource = `
export skill CodeAuditorSkill {
    description: "Analisa código."
    tools: [execution.read_file, execution.git_diff]
    mcp_servers: [PostgresDB]

    input {
        target_file: string
        strict_mode: boolean
    }

    output {
        vulnerabilities_found: int
        report_markdown: string
    }

    system_instructions: """
    Você é um auditor de segurança sênior.
    """
}

agent ClaudeCoder {
    engine: "anthropic/claude-3-5-sonnet"
    system_prompt: "You are ClaudeCoder, a senior engineer."
    mcp_servers: [PostgresDB, GitHubServer]
    tools: [execution.read_file, execution.write_file, execution.git_diff]
}
`

func resolveAgent(t *testing.T, prog *ast.Program, name string) *skills.AgentConfig {
	t.Helper()
	for _, d := range prog.Decls {
		if d.Agent != nil && d.Agent.Name == name {
			return skills.AgentFromAST(d.Agent)
		}
	}
	t.Fatalf("agent %q not found", name)
	return nil
}

// AC-2 / IC-2: a scoped skill payload must contain exactly the skill's declared
// tools and mcp_servers, and nothing from the agent's broader configuration.
func TestSkillScopeAllowlist(t *testing.T) {
	prog, err := parser.Parse(projectSource)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	skill, err := skills.ResolveSkill(prog, "CodeAuditorSkill")
	if err != nil {
		t.Fatalf("resolve skill: %v", err)
	}
	agent := resolveAgent(t, prog, "ClaudeCoder")

	var r skills.SkillRuntimeResolver
	payload, err := r.PrepareAgentPayloadForSkill(agent, skill, map[string]interface{}{
		"target_file": "src/main.go",
		"strict_mode": true,
	})
	if err != nil {
		t.Fatalf("prepare payload: %v", err)
	}

	wantTools := []string{"execution.read_file", "execution.git_diff"}
	wantMCP := []string{"PostgresDB"}

	if !equalSet(payload.ActiveTools, wantTools) {
		t.Errorf("ActiveTools = %v, want exactly %v", payload.ActiveTools, wantTools)
	}
	if !equalSet(payload.ActiveMCPServers, wantMCP) {
		t.Errorf("ActiveMCPServers = %v, want exactly %v", payload.ActiveMCPServers, wantMCP)
	}

	// Explicitly assert nothing from the agent's broader set leaked through.
	for _, leaked := range []string{"execution.write_file"} {
		if contains(payload.ActiveTools, leaked) {
			t.Errorf("agent-only tool %q leaked into scoped payload", leaked)
		}
	}
	if contains(payload.ActiveMCPServers, "GitHubServer") {
		t.Errorf("agent-only mcp_server GitHubServer leaked into scoped payload")
	}
}

// AC-3: the merged instructions must contain both the agent base prompt and the
// skill's system_instructions, tagged with the active skill name.
func TestMergedInstructionsTagged(t *testing.T) {
	prog, err := parser.Parse(projectSource)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	skill, err := skills.ResolveSkill(prog, "CodeAuditorSkill")
	if err != nil {
		t.Fatalf("resolve skill: %v", err)
	}
	agent := resolveAgent(t, prog, "ClaudeCoder")

	var r skills.SkillRuntimeResolver
	payload, err := r.PrepareAgentPayloadForSkill(agent, skill, nil)
	if err != nil {
		t.Fatalf("prepare payload: %v", err)
	}

	if !strings.Contains(payload.SystemInstructions, agent.BaseSystemPrompt) {
		t.Errorf("merged instructions missing agent base prompt:\n%s", payload.SystemInstructions)
	}
	if !strings.Contains(payload.SystemInstructions, "auditor de segurança sênior") {
		t.Errorf("merged instructions missing skill system_instructions:\n%s", payload.SystemInstructions)
	}
	if !strings.Contains(payload.SystemInstructions, "[ACTIVE SKILL: CodeAuditorSkill]") {
		t.Errorf("merged instructions missing active-skill tag:\n%s", payload.SystemInstructions)
	}
}

// Failure path: an unresolvable skill reference must abort rather than widen
// access, and preparing a payload for a nil skill must also fail closed.
func TestFailClosedOnUnresolvableSkill(t *testing.T) {
	prog, err := parser.Parse(projectSource)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if _, err := skills.ResolveSkill(prog, "NoSuchSkill"); err == nil {
		t.Fatalf("expected error resolving unknown skill, got nil")
	}

	agent := resolveAgent(t, prog, "ClaudeCoder")
	var r skills.SkillRuntimeResolver
	if _, err := r.PrepareAgentPayloadForSkill(agent, nil, nil); err == nil {
		t.Fatalf("expected fail-closed error for nil skill, got nil")
	}
}

func TestListSkills(t *testing.T) {
	prog, err := parser.Parse(projectSource)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	defs := skills.ListSkills(prog)
	if len(defs) != 1 {
		t.Fatalf("ListSkills len = %d, want 1", len(defs))
	}
	if defs[0].Name != "CodeAuditorSkill" {
		t.Errorf("skill name = %q, want CodeAuditorSkill", defs[0].Name)
	}
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

func equalSet(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	g := append([]string{}, got...)
	w := append([]string{}, want...)
	sort.Strings(g)
	sort.Strings(w)
	for i := range g {
		if g[i] != w[i] {
			return false
		}
	}
	return true
}
