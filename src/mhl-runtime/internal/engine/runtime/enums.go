package runtime

import (
	"fmt"
	"strings"

	"github.com/mh-language/mhl-core-runtime/internal/engine/value"
	"github.com/mh-language/mhl-core-runtime/internal/lang/types"
)

// ConvertEnums converts a variant name into its enum value wherever dt
// declares an enum — at the top, or at any depth through array elements and
// object fields. A pipeline input arrives as text (a CLI flag, a JSON string
// from an MCP/A2A caller, a test's `Name.run(inputs: ...)`); without this an
// enum-typed input stayed a plain string, so it never equalled `Kind.x` and
// an exhaustive `match` over it failed at run time. A name that is not a
// variant is rejected listing the valid ones. Anything else is returned
// unchanged for types.Check to judge.
func ConvertEnums(label string, dt types.Type, v any, enums map[string][]string) (any, error) {
	switch dt.Kind {
	case types.EnumKind:
		name, ok := v.(string)
		if !ok {
			return v, nil
		}
		variants := enums[dt.Name]
		for _, variant := range variants {
			if variant == name {
				return value.Enum{Enum: dt.Name, Variant: name}, nil
			}
		}
		return nil, fmt.Errorf("%s: %q is not a variant of enum %s (use %s)", label, name, dt.Name, strings.Join(variants, " | "))
	case types.ArrayKind:
		items, ok := v.([]any)
		if !ok || dt.Elem == nil {
			return v, nil
		}
		out := make([]any, len(items))
		for i, item := range items {
			cv, err := ConvertEnums(fmt.Sprintf("%s[%d]", label, i), *dt.Elem, item, enums)
			if err != nil {
				return nil, err
			}
			out[i] = cv
		}
		return out, nil
	case types.ObjectKind:
		obj, ok := v.(map[string]any)
		if !ok || dt.Fields == nil {
			return v, nil
		}
		out := make(map[string]any, len(obj))
		for k, fv := range obj {
			ft, declared := dt.Fields[k]
			if !declared || fv == nil {
				out[k] = fv
				continue
			}
			cv, err := ConvertEnums(fmt.Sprintf("%s.%s", label, k), ft, fv, enums)
			if err != nil {
				return nil, err
			}
			out[k] = cv
		}
		return out, nil
	}
	return v, nil
}
