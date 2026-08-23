// Package skills implements the Skill Resolution & Dependency Injection
// subsystem (§4.2 "2. Skill & Dependency Injector"). It resolves a declared
// Skill and prepares an agent invocation payload whose active tools and
// mcp_servers are sandboxed to exactly the skill's declared scope, with
// system instructions merged from the agent's base prompt plus the skill's
// instructions.
//
// The subsystem is fail-closed (ADR-2): any resolution error aborts the
// invocation rather than falling back to the agent's full tool set.
//
// Scope: skill resolution/injection only. MCP transport and credential
// resolution mechanics are out of scope for this package.
package skills

import (
	"fmt"

	"github.com/yanjustino/mhl-runtime/internal/lang/ast"
)

// SkillDefinition is the resolved, runtime-facing view of an ast.Skill.
type SkillDefinition struct {
	Name               string
	Description        string
	Tools              []string
	MCPServers         []string
	InputSchema        map[string]string
	OutputSchema       map[string]string
	SystemInstructions string
}

// AgentConfig is the resolved, runtime-facing view of an agent's
// configuration relevant to skill invocation. An agent may declare a broader
// tool/mcp_server set than any individual skill; those broader lists MUST NOT
// leak into a scoped skill invocation.
type AgentConfig struct {
	Name             string
	Engine           string
	BaseSystemPrompt string
	Tools            []string
	MCPServers       []string
}

// AgentInvocationPayload is the sandboxed payload handed to the model/engine
// for the duration of a single skill invocation.
type AgentInvocationPayload struct {
	Engine             string
	SystemInstructions string
	ActiveTools        []string
	ActiveMCPServers   []string
	InputParameters    map[string]interface{}
}

// SkillRuntimeResolver resolves skills from a parsed program and prepares
// sandboxed agent invocation payloads.
type SkillRuntimeResolver struct{}

// PrepareAgentPayloadForSkill merges the skill's instructions into the agent's
// call context and restricts the agent's active tools/mcp_servers to exactly
// the skill's declared scope (ADR-2, RF-2, AC-2, AC-3).
//
// It is fail-closed: a nil agent or nil skill aborts the invocation with an
// error instead of granting the agent's full access.
func (s *SkillRuntimeResolver) PrepareAgentPayloadForSkill(
	agent *AgentConfig,
	skill *SkillDefinition,
	inputArgs map[string]interface{},
) (*AgentInvocationPayload, error) {
	if agent == nil {
		return nil, fmt.Errorf("skills: cannot prepare payload for a nil agent")
	}
	if skill == nil {
		// Fail closed: never widen access when the skill is unresolvable.
		return nil, fmt.Errorf("skills: cannot invoke a nil/unresolved skill for agent %q", agent.Name)
	}

	// 1. Concatenate the agent's base prompt with the skill's specific
	//    instructions, tagged with the active skill name (AC-3).
	combinedInstructions := fmt.Sprintf(
		"%s\n\n[ACTIVE SKILL: %s]\n%s",
		agent.BaseSystemPrompt,
		skill.Name,
		skill.SystemInstructions,
	)

	// 2. Tool/MCP sandboxing: the payload receives ONLY the tools and
	//    mcp_servers declared by the skill (AC-2). The agent's broader
	//    configuration is deliberately not consulted here.
	scopedTools := append([]string{}, skill.Tools...)
	scopedMCP := append([]string{}, skill.MCPServers...)

	return &AgentInvocationPayload{
		Engine:             agent.Engine,
		SystemInstructions: combinedInstructions,
		ActiveTools:        scopedTools,
		ActiveMCPServers:   scopedMCP,
		InputParameters:    inputArgs,
	}, nil
}

// ResolveSkill looks up a declared skill by name in a parsed program and
// returns its resolved definition. It is fail-closed: an unknown skill name
// yields an error rather than a nil/empty definition, so callers cannot
// silently proceed with a widened tool set.
func ResolveSkill(prog *ast.Program, name string) (*SkillDefinition, error) {
	if prog == nil {
		return nil, fmt.Errorf("skills: cannot resolve %q in a nil program", name)
	}
	for _, def := range ListSkills(prog) {
		if def.Name == name {
			return def, nil
		}
	}
	return nil, fmt.Errorf("skills: skill %q is not declared in the current project", name)
}

// ListSkills returns the resolved definitions of every skill declared in the
// program, in declaration order. It backs the `mhl skills list` command.
func ListSkills(prog *ast.Program) []*SkillDefinition {
	var out []*SkillDefinition
	if prog == nil {
		return out
	}
	for _, d := range prog.Decls {
		if d == nil || d.Skill == nil {
			continue
		}
		out = append(out, skillFromAST(d.Skill))
	}
	return out
}

// skillFromAST projects an ast.Skill onto a resolved SkillDefinition.
func skillFromAST(s *ast.Skill) *SkillDefinition {
	def := &SkillDefinition{
		Name:         s.Name,
		InputSchema:  map[string]string{},
		OutputSchema: map[string]string{},
	}
	for _, m := range s.Body {
		switch {
		case m.Input != nil:
			for _, f := range m.Input.Fields {
				def.InputSchema[f.Name] = f.Type
			}
		case m.Output != nil:
			for _, f := range m.Output.Fields {
				def.OutputSchema[f.Name] = f.Type
			}
		case m.Prop != nil:
			applySkillProp(def, m.Prop)
		}
	}
	return def
}

// AgentFromAST projects an ast.Agent onto a resolved AgentConfig, reading the
// agent's engine, base system prompt, and its (broader) declared tools and
// mcp_servers. The broader lists are captured for inspection only; they are
// never copied into a scoped skill payload.
func AgentFromAST(a *ast.Agent) *AgentConfig {
	cfg := &AgentConfig{Name: a.Name}
	for _, p := range a.Props {
		switch p.Name {
		case "engine":
			if v, ok := ast.StringValue(p.Value); ok {
				cfg.Engine = v
			}
		case "system_prompt", "system_instructions":
			if v, ok := ast.StringValue(p.Value); ok {
				cfg.BaseSystemPrompt = v
			}
		case "tools":
			cfg.Tools = refListValue(p.Value)
		case "mcp_servers":
			cfg.MCPServers = refListValue(p.Value)
		}
	}
	return cfg
}

func applySkillProp(def *SkillDefinition, p *ast.Property) {
	switch p.Name {
	case "description":
		if v, ok := ast.StringValue(p.Value); ok {
			def.Description = v
		}
	case "system_instructions":
		if v, ok := ast.StringValue(p.Value); ok {
			def.SystemInstructions = v
		}
	case "tools":
		def.Tools = refListValue(p.Value)
	case "mcp_servers":
		def.MCPServers = refListValue(p.Value)
	}
}
