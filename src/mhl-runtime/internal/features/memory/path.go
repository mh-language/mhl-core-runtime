package memory

import (
	"strconv"
	"strings"
)

// pathDelimiter separates a get() key from the navigation path into a
// structured (array/object) value, e.g. "cfg::retries" or "tags::0".
const pathDelimiter = "::"

// splitPathKey splits a get() key into its base store key and an optional
// navigation path: "cfg::retries" -> ("cfg", ["retries"]), "tags::0" ->
// ("tags", ["0"]) to index into an array, and a plain "attempt" (no
// delimiter) -> ("attempt", nil), unchanged from before path navigation
// existed.
func splitPathKey(key string) (base string, path []string) {
	parts := strings.Split(key, pathDelimiter)
	return parts[0], parts[1:]
}

// resolvePath walks path into value, returning the nested value and whether
// every segment resolved. An object segment looks up a map key; a segment
// that parses as a non-negative integer indexes into an array. Any segment
// that doesn't match stops navigation and reports not found, the same as a
// plain missing key.
func resolvePath(value any, path []string) (any, bool) {
	cur := value
	for _, seg := range path {
		switch v := cur.(type) {
		case map[string]any:
			next, ok := v[seg]
			if !ok {
				return nil, false
			}
			cur = next
		case []any:
			idx, err := strconv.Atoi(seg)
			if err != nil || idx < 0 || idx >= len(v) {
				return nil, false
			}
			cur = v[idx]
		default:
			return nil, false
		}
	}
	return cur, true
}

// removePath deletes the value at path from within container, returning the
// (possibly new, in the slice-splice case) container and whether anything
// was removed. Mirrors resolvePath's navigation: an object segment deletes a
// map key, an integer segment splices an array element out. Any segment
// that doesn't match reports not found, same as resolvePath.
func removePath(container any, path []string) (any, bool) {
	seg := path[0]
	switch v := container.(type) {
	case map[string]any:
		if len(path) == 1 {
			if _, ok := v[seg]; !ok {
				return container, false
			}
			delete(v, seg)
			return v, true
		}
		next, ok := v[seg]
		if !ok {
			return container, false
		}
		updated, removed := removePath(next, path[1:])
		if removed {
			v[seg] = updated
		}
		return v, removed
	case []any:
		idx, err := strconv.Atoi(seg)
		if err != nil || idx < 0 || idx >= len(v) {
			return container, false
		}
		if len(path) == 1 {
			out := make([]any, 0, len(v)-1)
			out = append(out, v[:idx]...)
			out = append(out, v[idx+1:]...)
			return out, true
		}
		updated, removed := removePath(v[idx], path[1:])
		if removed {
			v[idx] = updated
		}
		return v, removed
	default:
		return container, false
	}
}
