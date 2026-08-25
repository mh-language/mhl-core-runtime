package lsp

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/yanjustino/mhl-runtime/internal/lang/ast"
	"github.com/yanjustino/mhl-runtime/internal/lang/parser"
)

// symbolKind identifies what a declared top-level name is, which in turn
// decides what completion.go offers after "name." (an agent gets `run`, a
// tool gets its own declared methods, ...).
type symbolKind int

const (
	symAgent symbolKind = iota
	symMemory
	symTool
	symPrompt
	symSkill
	symPipeline
	symMCPServer
	symNative
)

func (k symbolKind) label() string {
	switch k {
	case symAgent:
		return "agent"
	case symMemory:
		return "memory"
	case symTool:
		return "tool"
	case symPrompt:
		return "prompt"
	case symSkill:
		return "skill"
	case symPipeline:
		return "pipeline"
	case symMCPServer:
		return "mcp_server"
	case symNative:
		return "native"
	default:
		return ""
	}
}

// symbol is one declared top-level name, as gathered from a parsed AST or a
// best-effort regex scan of unparseable buffer text.
type symbol struct {
	Name    string
	Kind    symbolKind
	Methods []string // dot-callable members, e.g. an agent's "run" or a tool's declared method names
}

// symbolsFromProgram walks a successfully parsed AST and returns every
// top-level declared symbol, memory/tool member methods included.
func symbolsFromProgram(prog *ast.Program) []symbol {
	var syms []symbol
	for _, decl := range prog.Decls {
		switch {
		case decl.Agent != nil && decl.Agent.Name != "":
			syms = append(syms, symbol{Name: decl.Agent.Name, Kind: symAgent, Methods: []string{"run"}})
		case decl.Memory != nil:
			syms = append(syms, symbol{Name: decl.Memory.Name, Kind: symMemory, Methods: memoryMethods(decl.Memory)})
		case decl.Tool != nil:
			methods := make([]string, 0, len(decl.Tool.Methods))
			for _, m := range decl.Tool.Methods {
				methods = append(methods, m.Name)
			}
			syms = append(syms, symbol{Name: decl.Tool.Name, Kind: symTool, Methods: methods})
		case decl.Prompt != nil:
			syms = append(syms, symbol{Name: decl.Prompt.Name, Kind: symPrompt})
		case decl.Skill != nil:
			syms = append(syms, symbol{Name: decl.Skill.Name, Kind: symSkill})
		case decl.Pipeline != nil:
			syms = append(syms, symbol{Name: decl.Pipeline.Name, Kind: symPipeline})
		case decl.MCPServer != nil:
			syms = append(syms, symbol{Name: decl.MCPServer.Name, Kind: symMCPServer})
		}
	}
	return syms
}

// memoryMethods mirrors internal/lang/lint.checkMemoryOp's per-type method
// set, so completion offers exactly the operations lint would accept.
func memoryMethods(mem *ast.Memory) []string {
	memType := ""
	for _, p := range mem.Props {
		if p.Name == "type" {
			memType, _ = ast.StringValue(p.Value)
		}
	}
	return memoryMethodsForType(memType)
}

func memoryMethodsForType(memType string) []string {
	switch memType {
	case "kv", "json":
		return []string{"get", "set"}
	case "append_log", "jsonl":
		return []string{"append"}
	default:
		return []string{"get", "set", "append"}
	}
}

// declRe recognizes a top-level `<keyword> <Name>` declaration line even in
// source that doesn't fully parse yet — the common case while the user is
// mid-edit (most obviously: the exact moment they've typed "name." and are
// choosing a member off it, which by construction is invalid syntax until
// the member name is finished). Name and kind alone are cheap to recover
// this way; memberMethodsFromText below recovers the dot-callable members
// too, since that's exactly the information member completion needs most
// when the buffer won't parse. An optional leading `loop` (as in `loop
// pipeline X`) is skipped, not captured — it's a modifier on `pipeline`, not
// a declaration kind of its own.
var declRe = regexp.MustCompile(`(?m)^\s*(?:export\s+)?(?:loop\s+)?(agent|memory|tool|prompt|skill|pipeline|mcp_server)\s+([A-Za-z_][A-Za-z0-9_]*)`)

func symbolsFromText(src string) []symbol {
	var syms []symbol
	for _, m := range declRe.FindAllStringSubmatch(src, -1) {
		kind, ok := kindFromKeyword(m[1])
		if !ok {
			continue
		}
		s := symbol{Name: m[2], Kind: kind}
		switch kind {
		case symAgent:
			s.Methods = []string{"run"}
		case symMemory:
			s.Methods = memoryMethodsFromText(src, m[2])
		case symTool:
			s.Methods = toolMethodsFromText(src, m[2])
		}
		syms = append(syms, s)
	}
	return syms
}

// toolMethodDeclRe matches a tool method declaration `name(params) ->` —
// the one shape (ast.ToolMethod) that's unambiguous even scanned out of
// context, since ordinary calls inside a method body are never followed by
// `->`.
var toolMethodDeclRe = regexp.MustCompile(`(?m)^\s*([A-Za-z_][A-Za-z0-9_]*)\s*\([^()]*\)\s*->`)

func toolMethodsFromText(src, name string) []string {
	body, ok := extractBlock(src, "tool", name)
	if !ok {
		return nil
	}
	var methods []string
	for _, m := range toolMethodDeclRe.FindAllStringSubmatch(body, -1) {
		methods = append(methods, m[1])
	}
	return methods
}

var memoryTypeRe = regexp.MustCompile(`\btype\s*:\s*"([^"]*)"`)

func memoryMethodsFromText(src, name string) []string {
	body, ok := extractBlock(src, "memory", name)
	if !ok {
		return memoryMethodsForType("")
	}
	memType := ""
	if m := memoryTypeRe.FindStringSubmatch(body); m != nil {
		memType = m[1]
	}
	return memoryMethodsForType(memType)
}

// extractBlock finds `<keyword> <name> { ... }` in src and returns the
// content between its outermost braces, brace-depth matched so a nested
// `{`/`}` inside a value never truncates the block early. String and
// double-quoted-string contents are skipped while counting so a stray
// brace character inside a string literal can't confuse the depth count
// either. ok is false when the declaration isn't found or its opening
// brace is never closed (typically: the user hasn't typed the closing `}`
// yet).
func extractBlock(src, keyword, name string) (string, bool) {
	declStart := regexp.MustCompile(`\b` + regexp.QuoteMeta(keyword) + `\s+` + regexp.QuoteMeta(name) + `\b`).FindStringIndex(src)
	if declStart == nil {
		return "", false
	}
	rest := src[declStart[1]:]
	brace := strings.IndexByte(rest, '{')
	if brace < 0 {
		return "", false
	}
	body := rest[brace+1:]

	depth := 1
	inString := false
	for i := 0; i < len(body); i++ {
		switch c := body[i]; c {
		case '"':
			inString = !inString
		case '{':
			if !inString {
				depth++
			}
		case '}':
			if !inString {
				depth--
				if depth == 0 {
					return body[:i], true
				}
			}
		}
	}
	return "", false
}

func kindFromKeyword(kw string) (symbolKind, bool) {
	switch kw {
	case "agent":
		return symAgent, true
	case "memory":
		return symMemory, true
	case "tool":
		return symTool, true
	case "prompt":
		return symPrompt, true
	case "skill":
		return symSkill, true
	case "pipeline":
		return symPipeline, true
	case "mcp_server":
		return symMCPServer, true
	}
	return 0, false
}

// nativeSymbols are the built-in cmd/git/fs/http/json/log namespaces —
// never declared in any .mh source, so symbolsFromProgram/symbolsFromText
// can't find them, yet a .mh author calls their members constantly. Method
// sets mirror the case labels nativeOpCall actually implements
// (internal/engine/interpreter/tool.go) and evalLogCall's log.info/warn/error
// (internal/engine/interpreter/eval.go); keep both lists in sync by hand —
// there's no single source these are generated from.
var nativeSymbols = []symbol{
	{Name: "cmd", Kind: symNative, Methods: []string{"exec", "exec_all"}},
	{Name: "git", Kind: symNative, Methods: []string{"diff", "add", "commit", "status", "rev_parse", "log"}},
	{Name: "fs", Kind: symNative, Methods: []string{"read", "exists", "write", "append", "delete", "list", "join"}},
	{Name: "http", Kind: symNative, Methods: []string{"post"}},
	{Name: "json", Kind: symNative, Methods: []string{"parse", "parse_lines", "stringify"}},
	{Name: "log", Kind: symNative, Methods: []string{"info", "warn", "error"}},
}

// documentSymbols returns every symbol visible from path/text: the fixed
// native namespaces, the buffer's own declarations (parsed properly when
// text is syntactically valid, regex-recovered otherwise so completion keeps
// working mid-edit), plus every declaration in sibling .mh files under the
// same directory — a plain, import-oblivious "everything nearby is in
// scope" approximation that's good enough for completion (lint.Source is
// what actually enforces real import/use rules). Native symbols are listed
// first so they win dedupeSymbols' "first occurrence wins" tie-break,
// matching how the runtime itself always resolves e.g. "log" as the
// built-in namespace regardless of what a .mh author declares (eval.go's
// nativeNamespaces check runs before any user-declaration lookup).
func documentSymbols(path, text string) []symbol {
	syms := append([]symbol{}, nativeSymbols...)
	if prog, err := parser.Parse(text); err == nil {
		syms = append(syms, symbolsFromProgram(prog)...)
	} else {
		syms = append(syms, symbolsFromText(text)...)
	}
	syms = append(syms, workspaceSymbols(path)...)
	return dedupeSymbols(syms)
}

// workspaceSymbols scans every other .mh file in path's directory (one
// level, not recursive — mirrors how a project typically groups related
// .mh files together) and returns their declared symbols.
func workspaceSymbols(path string) []symbol {
	dir := filepath.Dir(path)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var syms []symbol
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".mh") {
			continue
		}
		full := filepath.Join(dir, e.Name())
		if full == path {
			continue
		}
		src, err := os.ReadFile(full)
		if err != nil {
			continue
		}
		prog, err := parser.Parse(string(src))
		if err != nil {
			continue
		}
		syms = append(syms, symbolsFromProgram(prog)...)
	}
	return syms
}

// dedupeSymbols keeps the first occurrence of each name (the buffer's own
// declaration, when present, wins over a workspace one) and sorts the
// result for stable completion ordering.
func dedupeSymbols(syms []symbol) []symbol {
	seen := make(map[string]bool, len(syms))
	out := make([]symbol, 0, len(syms))
	for _, s := range syms {
		if seen[s.Name] {
			continue
		}
		seen[s.Name] = true
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
