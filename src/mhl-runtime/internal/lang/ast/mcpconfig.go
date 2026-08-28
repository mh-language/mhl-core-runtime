package ast

import "fmt"

// This file holds the AST-literal-reading parts of an mcp_server declaration's
// config, shared by internal/lang/lint (static checking) and
// internal/features/mcp (registry resolution). Like agentconfig.go it only
// reads literals already present in the parse tree — no side effects.

// mcpProtocolValues is the set of accepted `protocol:` values. Keep in sync
// with mcp.ParseProtocol.
var mcpProtocolValues = []string{"auto", "2026-07-28", "2025-11-25", "2025-06-18"}

// MCPServerProtocol reads an mcp_server's optional `protocol:` property. An
// absent property yields "" (meaning auto) with no error. A present value that
// is not one of mcpProtocolValues is an error, mirroring how agentconfig.go
// rejects an unimplemented retry.backoff.
func MCPServerProtocol(m *MCPServer) (string, error) {
	for _, prop := range m.Props {
		if prop.Name != "protocol" {
			continue
		}
		v, ok := StringValue(prop.Value)
		if !ok {
			return "", fmt.Errorf("mcp_server %q protocol must be a string", m.Name)
		}
		for _, allowed := range mcpProtocolValues {
			if v == allowed {
				return v, nil
			}
		}
		return "", fmt.Errorf(
			"mcp_server %q protocol %q is not supported — use %q, %q, %q, or %q",
			m.Name, v, mcpProtocolValues[0], mcpProtocolValues[1], mcpProtocolValues[2], mcpProtocolValues[3])
	}
	return "", nil
}
