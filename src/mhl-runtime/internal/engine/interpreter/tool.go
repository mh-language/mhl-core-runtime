package interpreter

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/yanjustino/mhl-runtime/internal/features/nativeops"
	"github.com/yanjustino/mhl-runtime/internal/lang/ast"
)

func findTool(prog *ast.Program, name string) (*ast.Tool, bool) {
	name = resolveName(prog, name)
	for _, decl := range prog.Decls {
		if decl.Tool != nil && decl.Tool.Name == name {
			return decl.Tool, true
		}
	}
	return nil, false
}

// nativeNamespaces are the reserved `tool` method-body namespaces
// (language-design.md §7), plus `json` and `log` — never looked up against
// user declarations, the same way the bare `log(...)` builtin is reserved
// regardless of what a .mh author might otherwise name a variable.
var nativeNamespaces = map[string]bool{"cmd": true, "git": true, "fs": true, "http": true, "json": true, "log": true}

// evalToolCall resolves and executes a declared `tool` method call, e.g.
// `execution.get_diff()`. Arguments bind positionally to the method's
// declared Params (language-design.md §8 calls tool methods positionally —
// `execution.write_file(fix.file_path, fix.content)` — unlike the
// named-only convention `prompt Name(...)` uses), into a *fresh*
// environment: a tool method call is a real function-call boundary, so it
// never sees the caller step's variables, only its own bound parameters.
func evalToolCall(ctx *evalCtx, tool *ast.Tool, method string, call *ast.Call, depth int) (any, error) {
	var m *ast.ToolMethod
	for _, cand := range tool.Methods {
		if cand.Name == method {
			m = cand
			break
		}
	}
	if m == nil {
		return nil, fmt.Errorf("tool %q has no method %q", tool.Name, method)
	}
	args, err := evalPositionalValues(ctx, call, depth)
	if err != nil {
		return nil, err
	}
	if len(args) != len(m.Params) {
		return nil, fmt.Errorf("tool %q: %s requires %d argument(s), got %d", tool.Name, method, len(m.Params), len(args))
	}
	childEnv := Env{}
	for i, p := range m.Params {
		childEnv[p.Name] = args[i]
	}
	childCtx := &evalCtx{prog: ctx.prog, store: ctx.store, jsonStore: ctx.jsonStore, out: ctx.out, env: childEnv, file: ctx.file, selfTool: tool}
	return invokeCallable(childCtx, m.Body, m.Block, depth)
}

// invokeCallable runs a Body|Block pair (the shape ToolMethod and Lambda
// both share) against callCtx, which already has its parameters bound into
// a fresh Env. A single-expression Body evaluates directly to the call's
// result; a statement Block runs via execBlock — a `return` inside it sets
// the result (see returnSignal, internal/engine/interpreter/exec.go), and a block that
// completes with no `return` evaluates to nil (implicit void, the same nil
// formatValue already prints as "null").
func invokeCallable(callCtx *evalCtx, body *ast.Expr, block []*ast.Statement, depth int) (any, error) {
	if body != nil {
		return evalExprAt(callCtx, body, depth)
	}
	err := execBlock(callCtx, block)
	var sig *returnSignal
	if errors.As(err, &sig) {
		return sig.value, nil
	}
	if err != nil {
		return nil, err
	}
	return nil, nil
}

// nativeOpCall dispatches a `cmd`/`git`/`fs`/`http` namespace call — the
// fixed native operations a tool method body composes (language-design.md
// §7). Arguments are evaluated positional+named (evalCallArgs), since the
// spec mixes both in a single call (`cmd.exec("dotnet test", timeout:
// 120s)`); each operation validates and reads whichever of the two buckets
// it expects, the same style executeMemoryOp already validates its own
// positional args.
func nativeOpCall(ctx *evalCtx, namespace, op string, call *ast.Call, depth int) (any, error) {
	args, err := evalCallArgs(ctx, call, depth)
	if err != nil {
		return nil, err
	}
	switch namespace + "." + op {
	case "cmd.exec":
		timeout, _ := args.duration("timeout")
		if argv, ok := args.stringSliceAt(0); ok {
			result, err := nativeops.ExecArgs(context.Background(), argv, timeout)
			if err != nil {
				return nil, err
			}
			return result, nil
		}
		command, ok := args.stringAt(0)
		if !ok {
			return nil, fmt.Errorf("cmd.exec requires a string command, or an array of argv strings, as its first argument")
		}
		result, err := nativeops.Exec(context.Background(), command, timeout)
		if err != nil {
			return nil, err
		}
		return result, nil
	case "cmd.exec_all":
		timeout, _ := args.duration("timeout")
		commands, ok := args.commandListNamedOrAt("commands", 0)
		if !ok {
			return nil, fmt.Errorf("cmd.exec_all requires an array of commands (each a string or an array of argv strings) as its first argument")
		}
		results, err := nativeops.ExecAll(context.Background(), commands, timeout)
		if err != nil {
			return nil, err
		}
		out := make([]any, len(results))
		for i, r := range results {
			out[i] = r
		}
		return out, nil
	case "git.diff":
		target, _ := args.stringNamedOrFirst("target")
		result, err := nativeops.Diff(context.Background(), target)
		if err != nil {
			return nil, err
		}
		return result, nil
	case "git.add":
		paths, ok := args.stringSliceNamedOrAt("paths", 0)
		if !ok {
			return nil, fmt.Errorf("git.add requires an array of path strings as its first argument")
		}
		result, err := nativeops.Add(context.Background(), paths)
		if err != nil {
			return nil, err
		}
		return result, nil
	case "git.commit":
		message, ok := args.stringNamedOrAt("message", 0)
		if !ok {
			return nil, fmt.Errorf("git.commit requires a string message as its first argument")
		}
		paths, _ := args.stringSliceNamedOrAt("paths", 1)
		result, err := nativeops.Commit(context.Background(), message, paths)
		if err != nil {
			return nil, err
		}
		return result, nil
	case "git.status":
		paths, _ := args.stringSliceNamedOrAt("paths", 0)
		result, err := nativeops.Status(context.Background(), paths)
		if err != nil {
			return nil, err
		}
		return result, nil
	case "git.rev_parse":
		ref, ok := args.stringNamedOrFirst("ref")
		if !ok {
			return nil, fmt.Errorf("git.rev_parse requires a string ref as its first argument")
		}
		return nativeops.RevParse(context.Background(), ref)
	case "git.log":
		n, ok := args.intNamedOrAt("n", 0)
		if !ok {
			return nil, fmt.Errorf("git.log requires an integer n (number of entries) as its first argument")
		}
		return nativeops.Log(context.Background(), n)
	case "fs.read":
		path, ok := args.stringAt(0)
		if !ok {
			return nil, fmt.Errorf("fs.read requires a string path as its first argument")
		}
		return nativeops.Read(path)
	case "fs.exists":
		path, ok := args.stringAt(0)
		if !ok {
			return nil, fmt.Errorf("fs.exists requires a string path as its first argument")
		}
		return nativeops.Exists(path)
	case "fs.write":
		path, ok := args.stringNamedOrAt("path", 0)
		if !ok {
			return nil, fmt.Errorf("fs.write requires a string path")
		}
		content, ok := args.stringNamedOrAt("content", 1)
		if !ok {
			return nil, fmt.Errorf("fs.write requires string content")
		}
		return nativeops.Write(path, content)
	case "fs.append":
		path, ok := args.stringNamedOrAt("path", 0)
		if !ok {
			return nil, fmt.Errorf("fs.append requires a string path")
		}
		content, ok := args.stringNamedOrAt("content", 1)
		if !ok {
			return nil, fmt.Errorf("fs.append requires string content")
		}
		return nativeops.Append(path, content)
	case "fs.delete":
		path, ok := args.stringAt(0)
		if !ok {
			return nil, fmt.Errorf("fs.delete requires a string path as its first argument")
		}
		return nativeops.Delete(path)
	case "fs.join":
		parts, ok := args.allStrings()
		if !ok || len(parts) == 0 {
			return nil, fmt.Errorf("fs.join requires one or more string path segments")
		}
		return nativeops.Join(parts...), nil
	case "fs.list":
		dir, ok := args.stringAt(0)
		if !ok {
			return nil, fmt.Errorf("fs.list requires a string path as its first argument")
		}
		paths, err := nativeops.List(dir)
		if err != nil {
			return nil, err
		}
		out := make([]any, len(paths))
		for i, p := range paths {
			out[i] = p
		}
		return out, nil
	case "http.post":
		url, ok := args.stringNamedOrFirst("url")
		if !ok {
			return nil, fmt.Errorf("http.post requires a string url")
		}
		headers, err := args.stringMap("headers")
		if err != nil {
			return nil, err
		}
		return nativeops.Post(context.Background(), url, headers, args.named["body"])
	case "json.parse":
		text, ok := args.stringAt(0)
		if !ok {
			return nil, fmt.Errorf("json.parse requires a string as its first argument")
		}
		return nativeops.Parse(text)
	case "json.parse_lines":
		text, ok := args.stringAt(0)
		if !ok {
			return nil, fmt.Errorf("json.parse_lines requires a string as its first argument")
		}
		return nativeops.ParseLines(text)
	case "json.stringify":
		if len(args.positional) == 0 {
			return nil, fmt.Errorf("json.stringify requires a value as its first argument")
		}
		return nativeops.Stringify(args.positional[0])
	case "log.info":
		writeLog(ctx, "INFO", args.positional)
		return nil, nil
	case "log.warn":
		writeLog(ctx, "WARN", args.positional)
		return nil, nil
	case "log.error":
		writeLog(ctx, "ERROR", args.positional)
		return nil, nil
	default:
		return nil, fmt.Errorf("%s.%s is not a supported native operation", namespace, op)
	}
}

// callArgs buckets an evaluated call's arguments by whether they were
// positional or named, since native ops (unlike memory/agent calls) accept
// a mix of both in the same call.
type callArgs struct {
	positional []any
	named      map[string]any
}

func (a callArgs) stringAt(i int) (string, bool) {
	if i >= len(a.positional) {
		return "", false
	}
	s, ok := a.positional[i].(string)
	return s, ok
}

// stringSliceAt reads positional[i] as an array of strings — used by
// cmd.exec's argv form (`cmd.exec(["git", "commit", "-m", msg])`). Returns
// false, not an error, both when positional[i] isn't an array at all (so
// the caller can fall back to the plain-string command form) and when it
// is an array but contains a non-string element (a genuine argument error,
// left for the caller's own stringAt fallback to report).
func (a callArgs) stringSliceAt(i int) ([]string, bool) {
	if i >= len(a.positional) {
		return nil, false
	}
	return toStringSlice(a.positional[i])
}

// stringSliceNamedOrAt reads a named array-of-strings argument, falling
// back to the positional slot at i when the name wasn't used — the git.*
// ops' `paths`/`args` parameter is genuinely optional (an empty result
// means "no paths filter"), so ok=false here means "wasn't an array at
// all", not "missing"; callers that require at least one path check len
// themselves.
func (a callArgs) stringSliceNamedOrAt(name string, i int) ([]string, bool) {
	if v, ok := a.named[name]; ok {
		return toStringSlice(v)
	}
	return a.stringSliceAt(i)
}

// toStringSlice converts an already-evaluated MHL array ([]any) into a
// []string, failing (ok=false) if v isn't an array or holds a non-string
// element.
func toStringSlice(v any) ([]string, bool) {
	arr, ok := v.([]any)
	if !ok {
		return nil, false
	}
	out := make([]string, len(arr))
	for i, item := range arr {
		s, ok := item.(string)
		if !ok {
			return nil, false
		}
		out[i] = s
	}
	return out, true
}

// allStrings reads every positional argument as a string — used by fs.join,
// which (unlike stringSliceAt's single-array-argument ops) takes each path
// segment as its own argument (`fs.join(dir, "sub", "file.txt")`). Returns
// false if any positional argument isn't a string.
func (a callArgs) allStrings() ([]string, bool) {
	out := make([]string, len(a.positional))
	for i, v := range a.positional {
		s, ok := v.(string)
		if !ok {
			return nil, false
		}
		out[i] = s
	}
	return out, true
}

// commandListNamedOrAt reads a named or positional array of commands for
// cmd.exec_all — each element is normalized the same way cmd.exec's single
// command already is: a plain string splits on whitespace (no shell
// quoting, matching nativeops.Exec), an array of strings passes through
// verbatim as argv (matching nativeops.ExecArgs). Returns false if the
// argument isn't an array, or holds an element that's neither form.
func (a callArgs) commandListNamedOrAt(name string, i int) ([][]string, bool) {
	v, ok := a.named[name]
	if !ok {
		if i >= len(a.positional) {
			return nil, false
		}
		v = a.positional[i]
	}
	arr, ok := v.([]any)
	if !ok {
		return nil, false
	}
	out := make([][]string, len(arr))
	for idx, item := range arr {
		switch cmd := item.(type) {
		case string:
			out[idx] = strings.Fields(cmd)
		case []any:
			argv, ok := toStringSlice(cmd)
			if !ok {
				return nil, false
			}
			out[idx] = argv
		default:
			return nil, false
		}
	}
	return out, true
}

// intNamedOrAt reads a named integer argument, falling back to the
// positional slot at i — MHL numbers are float64 (eval.go), so this
// accepts a float64 only when it holds an exact integer value.
func (a callArgs) intNamedOrAt(name string, i int) (int, bool) {
	v, ok := a.named[name]
	if !ok {
		if i >= len(a.positional) {
			return 0, false
		}
		v = a.positional[i]
	}
	f, ok := v.(float64)
	if !ok {
		return 0, false
	}
	n := int(f)
	if float64(n) != f {
		return 0, false
	}
	return n, true
}

// stringNamedOrFirst reads a named string argument, falling back to the
// first positional argument when the name wasn't used — both spellings
// appear in language-design.md's own §7/§8 examples depending on the call.
func (a callArgs) stringNamedOrFirst(name string) (string, bool) {
	if v, ok := a.named[name]; ok {
		s, ok := v.(string)
		return s, ok
	}
	return a.stringAt(0)
}

func (a callArgs) stringNamedOrAt(name string, i int) (string, bool) {
	if v, ok := a.named[name]; ok {
		s, ok := v.(string)
		return s, ok
	}
	return a.stringAt(i)
}

func (a callArgs) duration(name string) (time.Duration, bool) {
	v, ok := a.named[name]
	if !ok {
		return 0, false
	}
	d, ok := v.(time.Duration)
	return d, ok
}

func (a callArgs) stringMap(name string) (map[string]string, error) {
	v, ok := a.named[name]
	if !ok {
		return nil, nil
	}
	obj, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s must be an object", name)
	}
	out := make(map[string]string, len(obj))
	for k, fv := range obj {
		s, ok := fv.(string)
		if !ok {
			return nil, fmt.Errorf("%s.%s must be a string", name, k)
		}
		out[k] = s
	}
	return out, nil
}

// evalCallArgs evaluates call's arguments into positional/named buckets. A
// bare Duration literal (e.g. "120s") is captured directly as a
// time.Duration without going through the general evalExpr value system —
// Duration never becomes a first-class MHL runtime value (evalPrimary
// still rejects it elsewhere), it only exists as a native-op argument.
func evalCallArgs(ctx *evalCtx, call *ast.Call, depth int) (callArgs, error) {
	out := callArgs{named: map[string]any{}}
	for _, arg := range call.Args {
		var v any
		if d, ok := ast.DurationValue(arg.Value); ok {
			v = d
		} else {
			ev, err := evalExprAt(ctx, arg.Value, depth)
			if err != nil {
				return callArgs{}, err
			}
			v = ev
		}
		if arg.Name == "" {
			out.positional = append(out.positional, v)
		} else {
			out.named[arg.Name] = v
		}
	}
	return out, nil
}
