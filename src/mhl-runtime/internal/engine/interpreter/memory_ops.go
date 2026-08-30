package interpreter

import (
	"fmt"

	"github.com/mh-language/mhl-core-runtime/internal/features/memory"
	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
)

func findMemory(prog *ast.Program, name string) (*ast.Memory, bool) {
	name = resolveName(prog, name)
	for _, decl := range prog.Decls {
		if decl.Memory != nil && decl.Memory.Name == name {
			return decl.Memory, true
		}
	}
	return nil, false
}

func isMemoryMethod(method string) bool {
	return method == "set" || method == "get" || method == "append" || method == "remove"
}

// memoryProp reads a named property off a memory declaration (e.g. "type",
// "store", "path").
func memoryProp(mem *ast.Memory, name string) (string, bool) {
	for _, prop := range mem.Props {
		if prop.Name == name {
			return ast.StringValue(prop.Value)
		}
	}
	return "", false
}

// memoryPath reads a memory declaration's `path:` and interpolates its
// "${...}" spans against ctx, exactly like an agent's `log:` path
// (agent.go's agentLogPath) — so `path: "data/runs.${context.session_id}.json"`
// gives each run (or each --session) its own file. A missing/empty path, or
// one that interpolates to empty, is an error: every path-backed memory kind
// (json, append_log, jsonl) needs a real file to write to. ctx is nil only
// in direct unit-test calls, which pass plain paths; interpolation is
// skipped there.
func memoryPath(ctx *evalCtx, mem *ast.Memory) (string, error) {
	raw, ok := memoryProp(mem, "path")
	if !ok || raw == "" {
		return "", fmt.Errorf("memory %q has no path", mem.Name)
	}
	if ctx == nil {
		return raw, nil
	}
	path, err := interpolate(ctx, raw)
	if err != nil {
		return "", fmt.Errorf("memory %q path %q: %w", mem.Name, raw, err)
	}
	if path == "" {
		return "", fmt.Errorf("memory %q path %q interpolated to empty", mem.Name, raw)
	}
	return path, nil
}

// executeMemoryOp dispatches a `memory.method(...)` call (language-design.md
// §6 "Gerenciamento de Memória"). `type: "kv"` (with `store: "memory"`),
// `type: "json"`, `type: "append_log"` and `type: "jsonl"` are implemented;
// `type: "vector"` and any other value fail closed with a clear
// "not supported yet" error, mirroring the agent engine dispatch boundary.
//
// The returned value is get's result (nil for set/append's own success
// path — set/append return the value they just wrote, useful for
// chaining/logging, e.g. `log(session_mem.set("attempt", 1))`). Nothing
// here prints anything on its own: get() stays silent unless the caller
// wraps it in log(...) (see memory_test.go), exactly as before — this
// signature change only makes the value available to the expression
// evaluator (eval.go), it doesn't change what's on stdout by itself.
//
// get's key may navigate into a structured (array/object) value with
// "::"-separated segments, e.g. "cfg::retries" or "tags::0" to index into
// an array (see internal/features/memory.splitPathKey/resolvePath) — a key
// with no "::" behaves exactly as before.
func executeMemoryOp(ctx *evalCtx, mem *ast.Memory, method string, call *ast.Call, depth int) (any, error) {
	memType, _ := memoryProp(mem, "type")
	args, err := evalPositionalValues(ctx, call, depth)
	if err != nil {
		return nil, err
	}

	switch memType {
	case "kv":
		memStore, _ := memoryProp(mem, "store")
		if memStore != "memory" {
			return nil, fmt.Errorf("memory %q: store %q is not supported yet", mem.Name, memStore)
		}
		switch method {
		case "set":
			if len(args) != 2 {
				return nil, fmt.Errorf("memory %q: set requires (key, value)", mem.Name)
			}
			key, ok := args[0].(string)
			if !ok {
				return nil, fmt.Errorf("memory %q: key must be a string", mem.Name)
			}
			ctx.store.Set(mem.Name, key, args[1])
			return args[1], nil
		case "get":
			if len(args) != 1 && len(args) != 2 {
				return nil, fmt.Errorf("memory %q: get requires (key) or (key, default)", mem.Name)
			}
			key, ok := args[0].(string)
			if !ok {
				return nil, fmt.Errorf("memory %q: key must be a string", mem.Name)
			}
			var def any
			if len(args) == 2 {
				def = args[1]
			}
			return ctx.store.Get(mem.Name, key, def), nil
		default:
			return nil, fmt.Errorf("memory %q: kv memory has no method %q", mem.Name, method)
		}
	case "json":
		path, err := memoryPath(ctx, mem)
		if err != nil {
			return nil, err
		}
		switch method {
		case "set":
			if len(args) == 1 {
				values, ok := args[0].(map[string]any)
				if !ok {
					return nil, fmt.Errorf("memory %q: set requires (key, value) or a single object", mem.Name)
				}
				if err := ctx.jsonStore.SetAll(path, values); err != nil {
					return nil, err
				}
				return values, nil
			}
			if len(args) != 2 {
				return nil, fmt.Errorf("memory %q: set requires (key, value) or a single object", mem.Name)
			}
			key, ok := args[0].(string)
			if !ok {
				return nil, fmt.Errorf("memory %q: key must be a string", mem.Name)
			}
			if err := ctx.jsonStore.Set(path, key, args[1]); err != nil {
				return nil, err
			}
			return args[1], nil
		case "get":
			if len(args) != 1 && len(args) != 2 {
				return nil, fmt.Errorf("memory %q: get requires (key) or (key, default)", mem.Name)
			}
			key, ok := args[0].(string)
			if !ok {
				return nil, fmt.Errorf("memory %q: key must be a string", mem.Name)
			}
			var def any
			if len(args) == 2 {
				def = args[1]
			}
			return ctx.jsonStore.Get(path, key, def)
		case "remove":
			if len(args) != 1 {
				return nil, fmt.Errorf("memory %q: remove requires (key)", mem.Name)
			}
			key, ok := args[0].(string)
			if !ok {
				return nil, fmt.Errorf("memory %q: key must be a string", mem.Name)
			}
			return ctx.jsonStore.Remove(path, key)
		default:
			return nil, fmt.Errorf("memory %q: json memory has no method %q", mem.Name, method)
		}
	case "append_log":
		path, err := memoryPath(ctx, mem)
		if err != nil {
			return nil, err
		}
		if method != "append" {
			return nil, fmt.Errorf("memory %q: append_log memory has no method %q", mem.Name, method)
		}
		if len(args) != 1 {
			return nil, fmt.Errorf("memory %q: append requires (text)", mem.Name)
		}
		text, ok := args[0].(string)
		if !ok {
			return nil, fmt.Errorf("memory %q: append_log entries must be plain text (a string), not a structured value", mem.Name)
		}
		if err := memory.Append(path, text); err != nil {
			return nil, err
		}
		return text, nil
	case "jsonl":
		path, err := memoryPath(ctx, mem)
		if err != nil {
			return nil, err
		}
		if method != "append" {
			return nil, fmt.Errorf("memory %q: jsonl memory has no method %q", mem.Name, method)
		}
		if len(args) != 1 {
			return nil, fmt.Errorf("memory %q: append requires (text)", mem.Name)
		}
		if err := memory.AppendJSONL(path, args[0]); err != nil {
			return nil, err
		}
		return args[0], nil
	case "":
		return nil, fmt.Errorf("memory %q has no type", mem.Name)
	default:
		return nil, fmt.Errorf("memory %q: type %q is not supported yet", mem.Name, memType)
	}
}
