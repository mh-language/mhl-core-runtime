package types

import "sort"

// JSONSchema renders t as a JSON Schema fragment (the dialect MCP tool
// `inputSchema` and A2A skill schemas use — plain `{"type": ...}`, no
// `$schema` header). It mirrors Type.String(): a projection of the same
// vocabulary into a different surface.
//
//   - Any        → {}                       (an empty schema accepts anything)
//   - string     → {"type": "string"}
//   - number     → {"type": "number"}
//   - bool       → {"type": "boolean"}
//   - T[]        → {"type": "array", "items": <schema(T)>}   (items omitted when Elem is nil)
//   - {a: T, …}  → {"type": "object", "properties": {a: <schema(T)>, …},
//     "required": [<every declared field>],
//     "additionalProperties": true}
//     Every declared field is required and extra fields are allowed, matching
//     Check's structural (not exact) object rule.
//   - enum X     → {"type": "string", "description": "mhl enum X"}
//     Best-effort: a Type of Kind EnumKind carries no variant list (see the
//     Type doc), so the concrete `"enum": [...]` list — when wanted — is the
//     caller's to add from the `enum` declaration.
func (t Type) JSONSchema() map[string]any {
	switch t.Kind {
	case StringKind:
		return map[string]any{"type": "string"}
	case NumberKind:
		return map[string]any{"type": "number"}
	case BoolKind:
		return map[string]any{"type": "boolean"}
	case EnumKind:
		return map[string]any{"type": "string", "description": "mhl enum " + t.Name}
	case ArrayKind:
		s := map[string]any{"type": "array"}
		if t.Elem != nil {
			s["items"] = t.Elem.JSONSchema()
		}
		return s
	case ObjectKind:
		if t.Fields == nil {
			return map[string]any{"type": "object", "additionalProperties": true}
		}
		props := make(map[string]any, len(t.Fields))
		required := make([]string, 0, len(t.Fields))
		for name, ft := range t.Fields {
			props[name] = ft.JSONSchema()
			required = append(required, name)
		}
		sort.Strings(required)
		return map[string]any{
			"type":                 "object",
			"properties":           props,
			"required":             required,
			"additionalProperties": true,
		}
	default: // AnyKind
		return map[string]any{}
	}
}
