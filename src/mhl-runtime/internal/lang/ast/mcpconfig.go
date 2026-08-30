package ast

import "fmt"

// This file holds the AST-literal-reading part of an `extension mcp`
// declaration's config, shared by internal/lang/lint (static checking) and
// internal/features/mcp (config resolution). Like agentconfig.go it only
// reads literals already present in the parse tree — no side effects.

// mcpProtocolValues is the set of accepted `protocol:` values. Keep in sync
// with mcp.ParseProtocol.
var mcpProtocolValues = []string{"auto", "2026-07-28", "2025-11-25", "2025-06-18"}

// MCPProtocolFromProps reads an `extension mcp` declaration's optional
// `protocol:` property from its property list. An absent property yields ""
// (meaning auto) with no error; a value that is not one of mcpProtocolValues
// is an error, mirroring how agentconfig.go rejects an unimplemented
// retry.backoff. name is the declaration name, used only in the message.
func MCPProtocolFromProps(name string, props []*Property) (string, error) {
	for _, prop := range props {
		if prop.Name != "protocol" {
			continue
		}
		v, ok := StringValue(prop.Value)
		if !ok {
			return "", fmt.Errorf("mcp %q protocol must be a string", name)
		}
		for _, allowed := range mcpProtocolValues {
			if v == allowed {
				return v, nil
			}
		}
		return "", fmt.Errorf(
			"mcp %q protocol %q is not supported — use %q, %q, %q, or %q",
			name, v, mcpProtocolValues[0], mcpProtocolValues[1], mcpProtocolValues[2], mcpProtocolValues[3])
	}
	return "", nil
}
