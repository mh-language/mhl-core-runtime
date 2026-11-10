package ast

import (
	"fmt"
	"strings"
)

// agentPropertyNames is the set of `agent { ... }` property names the
// runtime actually reads (internal/engine/interpreter/agent.go,
// agent_timeout.go, agent_hooks.go and this package's agentconfig.go), in
// the order the "unknown property" diagnostic lists them. Both
// internal/lang/lint (checkAgentProperties) and the interpreter (which
// refuses to run an agent carrying an unknown property, so a typo never
// silently runs the agent in the wrong place) read it through
// IsAgentProperty/AgentPropertyList. Keep internal/lsp/properties.go's
// agentPropertyItems in sync by hand.
var agentPropertyNames = []string{
	"engine", "command", "args", "stdin", "cwd", "endpoint", "temperature",
	"log", "log_mode", "trace", "retry", "cache", "rate_limit", "fallback",
	"timeout", "kill_grace", "skills", "system_prompt", "before", "after",
	"description",
}

var agentPropertySet = func() map[string]bool {
	m := make(map[string]bool, len(agentPropertyNames))
	for _, n := range agentPropertyNames {
		m[n] = true
	}
	return m
}()

// AgentPropertyNames returns every agent property name, in diagnostic order.
func AgentPropertyNames() []string { return append([]string(nil), agentPropertyNames...) }

// IsAgentProperty reports whether name is a property an agent body may
// declare.
func IsAgentProperty(name string) bool { return agentPropertySet[name] }

// AgentPropertyList is every agent property name, comma-separated, for
// diagnostics.
func AgentPropertyList() string { return strings.Join(agentPropertyNames, ", ") }

// agentRunArgNames are the named arguments `Agent.run(...)` (and a router's
// `.delegate(...)`, which forwards its call to the chosen agent) reads.
// Besides these, a call may name the agent's own declared inputs
// (Agent.Inputs) — which is also why an input may not take one of these
// names. Anything else used to be accepted and silently ignored — a call
// passing `model: "x"` ran with the declared model — so it is now an error,
// both in mhl lint and at run time.
var agentRunArgNames = []string{"prompt", "schema", "skills", "timeout"}

// IsAgentRunArg reports whether name is an argument Agent.run accepts.
func IsAgentRunArg(name string) bool {
	for _, n := range agentRunArgNames {
		if n == name {
			return true
		}
	}
	return false
}

// AgentRunArgList is every Agent.run argument name, comma-separated, for
// diagnostics.
func AgentRunArgList() string { return strings.Join(agentRunArgNames, ", ") }

// AgentRunAccepts describes what agent's `.run(...)` accepts, for the
// "unknown argument" diagnostic: the run arguments, plus its declared inputs
// when it has any.
func AgentRunAccepts(agent *Agent) string {
	accepts := AgentRunArgList()
	if agent != nil && len(agent.Inputs) > 0 {
		accepts += " and the inputs " + strings.Join(agent.InputNames(), ", ")
	}
	return accepts
}

// LogModes are the accepted values of an agent's `log_mode:` property:
// "append" (the default — every attempt adds to the file) or "truncate"
// (each attempt starts the file empty).
var LogModes = []string{"append", "truncate"}

// AgentLogMode reads an agent's `log_mode:` property. It must be a string
// literal naming one of LogModes; absent means "append". truncate is true
// for "truncate".
func AgentLogMode(agent *Agent) (truncate bool, err error) {
	for _, p := range agent.Props {
		if p.Name != "log_mode" {
			continue
		}
		s, ok := StringValue(p.Value)
		switch {
		case ok && s == "append":
			return false, nil
		case ok && s == "truncate":
			return true, nil
		}
		return false, agentError(agent, `log_mode must be "append" or "truncate"`)
	}
	return false, nil
}

func agentError(agent *Agent, msg string) error {
	name := agent.Name
	if name == "" {
		name = "<inline>"
	}
	return fmt.Errorf("agent %q: %s", name, msg)
}
