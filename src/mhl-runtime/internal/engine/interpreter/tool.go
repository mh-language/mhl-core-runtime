package interpreter

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/mh-language/mhl-core-runtime/internal/features/auth"
	"github.com/mh-language/mhl-core-runtime/internal/features/nativeops"
	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
	"github.com/mh-language/mhl-core-runtime/internal/lang/types"
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
// (language-design.md §7), plus `json`, `log`, and `time` — never looked up
// against user declarations, the same way the bare `log(...)` builtin is
// reserved regardless of what a .mh author might otherwise name a variable.
var nativeNamespaces = map[string]bool{"cmd": true, "git": true, "fs": true, "http": true, "json": true, "log": true, "time": true}

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
	required := requiredParamCount(m.Params)
	if len(args) < required || len(args) > len(m.Params) {
		return nil, fmt.Errorf("tool %q: %s %s, got %d", tool.Name, method, paramArityText(required, len(m.Params)), len(args))
	}
	// Bind the supplied arguments, then fill each omitted trailing parameter
	// from its default — evaluated in the callee's own scope, built up left
	// to right so a later default may read an earlier parameter.
	childEnv := Env{}
	childCtx := &evalCtx{prog: ctx.prog, store: ctx.store, jsonStore: ctx.jsonStore, out: ctx.out, env: childEnv, file: ctx.file, selfTool: tool, cctx: ctx.cctx, aliasTypes: ctx.aliasTypes, registry: ctx.registry}
	bound := make([]any, len(m.Params))
	for i, p := range m.Params {
		var v any
		switch {
		case i < len(args):
			v = args[i]
		case p.Default == nil:
			// Reachable only when a non-defaulted param follows a defaulted
			// one — lint flags that, but lint does not block `mhl run`.
			return nil, fmt.Errorf("tool %q: %s: missing argument for parameter %q (it has no default but follows one that does)", tool.Name, method, p.Name)
		default:
			dv, err := evalExprAt(childCtx, p.Default, depth)
			if err != nil {
				return nil, fmt.Errorf("tool %q: %s: default for parameter %q: %w", tool.Name, method, p.Name, err)
			}
			v = dv
		}
		bound[i] = v
		childEnv[p.Name] = v
	}
	for i, p := range m.Params {
		if p.Type == nil {
			continue
		}
		declared, ok := types.FromExprAlias(p.Type, ctx.aliasTypes)
		if !ok {
			return nil, fmt.Errorf("tool %q: %s: parameter %q has an unrecognized type %q", tool.Name, method, p.Name, p.Type)
		}
		if err := types.Check(fmt.Sprintf("tool %q: %s: parameter %q", tool.Name, method, p.Name), declared, bound[i]); err != nil {
			return nil, err
		}
	}
	result, err := invokeCallable(childCtx, m.Body, m.Block, depth)
	if err != nil {
		return nil, err
	}
	if m.Returns != nil {
		declared, ok := types.FromExprAlias(m.Returns, ctx.aliasTypes)
		if !ok {
			return nil, fmt.Errorf("tool %q: %s: unrecognized return type %q", tool.Name, method, m.Returns)
		}
		if err := types.Check(fmt.Sprintf("tool %q: %s: return value", tool.Name, method), declared, result); err != nil {
			return nil, err
		}
	}
	return result, nil
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
			result, err := nativeops.ExecArgs(goctxOf(ctx), argv, timeout)
			if err != nil {
				return nil, err
			}
			return result, nil
		}
		command, ok := args.stringAt(0)
		if !ok {
			return nil, fmt.Errorf("cmd.exec requires a string command, or an array of argv strings, as its first argument")
		}
		result, err := nativeops.Exec(goctxOf(ctx), command, timeout)
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
		results, err := nativeops.ExecAll(goctxOf(ctx), commands, timeout)
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
		result, err := nativeops.Diff(goctxOf(ctx), target, args.stringNamed("dir"))
		if err != nil {
			return nil, err
		}
		return result, nil
	case "git.add":
		paths, ok := args.stringSliceNamedOrAt("paths", 0)
		if !ok {
			return nil, fmt.Errorf("git.add requires an array of path strings as its first argument")
		}
		result, err := nativeops.Add(goctxOf(ctx), paths, args.stringNamed("dir"))
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
		result, err := nativeops.Commit(goctxOf(ctx), message, paths, args.stringNamed("dir"))
		if err != nil {
			return nil, err
		}
		return result, nil
	case "git.status":
		paths, _ := args.stringSliceNamedOrAt("paths", 0)
		result, err := nativeops.Status(goctxOf(ctx), paths, args.stringNamed("dir"))
		if err != nil {
			return nil, err
		}
		return result, nil
	case "git.rev_parse":
		ref, ok := args.stringNamedOrFirst("ref")
		if !ok {
			return nil, fmt.Errorf("git.rev_parse requires a string ref as its first argument")
		}
		return nativeops.RevParse(goctxOf(ctx), ref, args.stringNamed("dir"))
	case "git.log":
		n, ok := args.intNamedOrAt("n", 0)
		if !ok {
			return nil, fmt.Errorf("git.log requires an integer n (number of entries) as its first argument")
		}
		return nativeops.Log(goctxOf(ctx), n, args.stringNamed("dir"))
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
	case "http.get":
		return httpCall(goctxOf(ctx), "GET", args)
	case "http.post":
		return httpCall(goctxOf(ctx), "POST", args)
	case "http.put":
		return httpCall(goctxOf(ctx), "PUT", args)
	case "http.patch":
		return httpCall(goctxOf(ctx), "PATCH", args)
	case "http.delete":
		return httpCall(goctxOf(ctx), "DELETE", args)
	case "http.head":
		return httpCall(goctxOf(ctx), "HEAD", args)
	case "http.options":
		return httpCall(goctxOf(ctx), "OPTIONS", args)
	case "http.download":
		url, ok := args.stringNamedOrFirst("url")
		if !ok {
			return nil, fmt.Errorf("http.download requires a string url")
		}
		path, ok := args.stringNamedOrAt("path", 1)
		if !ok {
			return nil, fmt.Errorf("http.download requires a string destination path")
		}
		opts, err := httpOptions(args)
		if err != nil {
			return nil, err
		}
		return nativeops.Download(goctxOf(ctx), url, path, opts)
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
	case "time.now":
		return nativeops.TimeNow(), nil
	case "time.parse":
		text, ok := args.stringAt(0)
		if !ok {
			return nil, fmt.Errorf("time.parse requires a string as its first argument")
		}
		layout, _ := args.stringNamedOrAt("layout", 1)
		return nativeops.TimeParse(text, layout)
	case "time.format":
		value, ok := args.stringAt(0)
		if !ok {
			return nil, fmt.Errorf("time.format requires a string value as its first argument")
		}
		layout, _ := args.stringNamedOrAt("layout", 1)
		return nativeops.TimeFormat(value, layout)
	case "time.add":
		value, ok := args.stringAt(0)
		if !ok {
			return nil, fmt.Errorf("time.add requires a string value as its first argument")
		}
		d, ok := args.durationNamedOrAt("duration", 1)
		if !ok {
			return nil, fmt.Errorf("time.add requires a duration as its second argument")
		}
		return nativeops.TimeAdd(value, d)
	case "time.diff":
		a, ok := args.stringAt(0)
		if !ok {
			return nil, fmt.Errorf("time.diff requires a string as its first argument")
		}
		b, ok := args.stringAt(1)
		if !ok {
			return nil, fmt.Errorf("time.diff requires a string as its second argument")
		}
		return nativeops.TimeDiff(a, b)
	case "time.compare":
		a, ok := args.stringAt(0)
		if !ok {
			return nil, fmt.Errorf("time.compare requires a string as its first argument")
		}
		b, ok := args.stringAt(1)
		if !ok {
			return nil, fmt.Errorf("time.compare requires a string as its second argument")
		}
		return nativeops.TimeCompare(a, b)
	default:
		return nil, fmt.Errorf("%s.%s is not a supported native operation", namespace, op)
	}
}

// httpCall is the shared body of every http.<verb> native op: it resolves
// the URL (named `url:` or first positional, like http.post always allowed),
// reads the optional parameters into a nativeops.Options, and issues the
// request. gctx is the run's Go context (goctxOf(ctx)) — a run-level cancel
// aborts an in-flight request; nativeops.Do still applies its own `timeout:`
// bound on top.
func httpCall(gctx context.Context, method string, args callArgs) (any, error) {
	url, ok := args.stringNamedOrFirst("url")
	if !ok {
		return nil, fmt.Errorf("http.%s requires a string url", strings.ToLower(method))
	}
	opts, err := httpOptions(args)
	if err != nil {
		return nil, err
	}
	return nativeops.Do(gctx, method, url, opts)
}

// httpOptions reads every optional named argument an http.<verb> call
// accepts into a nativeops.Options. `headers`/`query`/`form` are
// string→string maps; `body` is any value; `text` is a raw string;
// `timeout` is a duration; `follow_redirects`/`raise_for_status` are bools;
// `auth` and `tls` are nested objects.
func httpOptions(args callArgs) (nativeops.Options, error) {
	var opts nativeops.Options

	headers, err := args.stringMap("headers")
	if err != nil {
		return opts, err
	}
	opts.Headers = headers
	for name, value := range headers {
		switch http.CanonicalHeaderKey(name) {
		case "Authorization", "Proxy-Authorization", "Cookie":
			auth.Register(value)
		}
	}

	query, err := args.stringMap("query")
	if err != nil {
		return opts, err
	}
	opts.Query = query

	form, err := args.stringMap("form")
	if err != nil {
		return opts, err
	}
	opts.Form = form

	if v, ok := args.named["body"]; ok {
		opts.Body = v
	}
	if v, ok := args.named["text"]; ok {
		s, ok := v.(string)
		if !ok {
			return opts, fmt.Errorf("text must be a string")
		}
		opts.Text = &s
	}

	if d, ok := args.duration("timeout"); ok {
		opts.Timeout = d
	}

	if v, ok := args.named["follow_redirects"]; ok {
		b, ok := v.(bool)
		if !ok {
			return opts, fmt.Errorf("follow_redirects must be a bool")
		}
		opts.FollowRedirects = &b
	}
	if v, ok := args.named["raise_for_status"]; ok {
		b, ok := v.(bool)
		if !ok {
			return opts, fmt.Errorf("raise_for_status must be a bool")
		}
		opts.RaiseForStatus = b
	}

	if v, ok := args.named["auth"]; ok {
		auth, err := httpAuth(v)
		if err != nil {
			return opts, err
		}
		opts.Auth = auth
	}

	if v, ok := args.named["tls"]; ok {
		t, err := httpTLS(v)
		if err != nil {
			return opts, err
		}
		opts.TLS = t
	}

	if v, ok := args.named["proxy"]; ok {
		s, ok := v.(string)
		if !ok {
			return opts, fmt.Errorf("proxy must be a string")
		}
		opts.Proxy = s
		if u, perr := url.Parse(s); perr == nil {
			if pw, has := u.User.Password(); has {
				auth.Register(pw)
			}
		}
	}

	return opts, nil
}

// httpAuth reads a `bearer`/`basic` `auth:` object into an AuthOptions,
// registering the token / password so it is masked in logs and errors.
func httpAuth(v any) (*nativeops.AuthOptions, error) {
	obj, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("auth must be an object")
	}
	a := &nativeops.AuthOptions{}
	bearer, err := stringField("auth.bearer", obj, "bearer")
	if err != nil {
		return nil, err
	}
	a.Bearer = bearer

	if bv, ok := obj["basic"]; ok {
		basic, ok := bv.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("auth.basic must be an object")
		}
		if a.BasicUser, err = stringField("auth.basic.user", basic, "user"); err != nil {
			return nil, err
		}
		if a.BasicPassword, err = stringField("auth.basic.password", basic, "password"); err != nil {
			return nil, err
		}
	}
	auth.Register(a.Bearer)
	auth.Register(a.BasicPassword)
	return a, nil
}

// httpTLS reads a `cert`/`key`/`ca`/`insecure` `tls:` object into a
// TLSOptions.
func httpTLS(v any) (*nativeops.TLSOptions, error) {
	obj, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("tls must be an object")
	}
	t := &nativeops.TLSOptions{}
	var err error
	if t.Cert, err = stringField("tls.cert", obj, "cert"); err != nil {
		return nil, err
	}
	if t.Key, err = stringField("tls.key", obj, "key"); err != nil {
		return nil, err
	}
	if t.CA, err = stringField("tls.ca", obj, "ca"); err != nil {
		return nil, err
	}
	if iv, ok := obj["insecure"]; ok {
		b, ok := iv.(bool)
		if !ok {
			return nil, fmt.Errorf("tls.insecure must be a bool")
		}
		t.Insecure = b
	}
	return t, nil
}

// stringField reads an optional string entry from an already-evaluated
// object value; a missing key yields "", a non-string value is an error.
func stringField(label string, obj map[string]any, key string) (string, error) {
	v, ok := obj[key]
	if !ok {
		return "", nil
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("%s must be a string", label)
	}
	return s, nil
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

// stringNamed reads a named-only string argument, returning "" when it was
// not supplied — used for genuinely optional named parameters like the
// git.* ops' `dir:` (the working directory to run git in), where absence
// means "use the current directory", not an error.
func (a callArgs) stringNamed(name string) string {
	if v, ok := a.named[name]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func (a callArgs) duration(name string) (time.Duration, bool) {
	v, ok := a.named[name]
	if !ok {
		return 0, false
	}
	d, ok := v.(time.Duration)
	return d, ok
}

// durationNamedOrAt reads a named duration argument, falling back to the
// positional slot at i — unlike cmd.exec's timeout: (always named),
// time.add's duration argument is written positionally
// (time.add(dt, 7d)), so duration(name) alone (named-only) isn't enough.
func (a callArgs) durationNamedOrAt(name string, i int) (time.Duration, bool) {
	if v, ok := a.named[name]; ok {
		d, ok := v.(time.Duration)
		return d, ok
	}
	if i >= len(a.positional) {
		return 0, false
	}
	d, ok := a.positional[i].(time.Duration)
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
