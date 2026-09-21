package lsp

import (
	"regexp"
	"strings"
)

// keywords is every reserved word in the MHL grammar (internal/lang/ast),
// offered as a plain keyword completion whenever the cursor isn't in a
// member-access position.
var keywords = []string{
	"agent", "router", "memory", "tool", "prompt", "pipeline", "workflow", "extension", "extensible", "loop", "partial",
	"import", "from", "as", "export", "input", "step", "entry", "test", "describe",
	"var", "const", "type", "enum", "match", "if", "else", "while", "for", "in", "try", "catch", "finally",
	"return", "break", "goto", "route", "spawn", "wait", "parallel", "timeout", "max", "true", "false", "null",
	"kind", "manifest", "properties",
}

// memberAccessRe matches an in-progress "name.partial" at the very end of
// the text before the cursor, capturing the target identifier. Used to
// switch completion from "everything" to "target's members only".
var memberAccessRe = regexp.MustCompile(`([A-Za-z_][A-Za-z0-9_]*)\.[A-Za-z0-9_]*$`)

// typeAnnotationRe matches an in-progress "name: partialType" at the very
// end of the text before the cursor — the shape of a `input name: ` or tool
// method `param: ` type annotation (see internal/lang/types' vocabulary).
// isTypeAnnotationPosition additionally
// restricts where this fires so it never fires on an ordinary `key: value`
// property (e.g. `agent { command: }`).
var typeAnnotationRe = regexp.MustCompile(`\b[A-Za-z_][A-Za-z0-9_]*\s*:\s*[A-Za-z_]*$`)

// declarationSnippet is one keyword's tab-through example body, in LSP
// snippet syntax ($1/${2:default}/$0 for the final cursor stop; same
// numbered placeholder repeated mirrors what's typed across occurrences).
// A literal `$` that must survive as mhl's own `${...}` interpolation syntax
// (e.g. an agent's args: ["-p", "${prompt}"]) is written `\$` so the client
// doesn't mistake it for a tabstop — see the "agent" entry below.
type declarationSnippet struct {
	body   string
	detail string
}

// declarationSnippets gives a handful of top-level declaration keywords a
// fill-in-the-placeholders example, in place of just the bare word, so
// `mhl.mh`'s reference example (docs/site/Docs-Reference.dc.html) is one Tab
// away instead of a context switch to the docs. Kept to the constructs whose
// shape isn't obvious from the keyword alone; deliberately not exhaustive —
// see completionAt's general keyword loop for how this is applied.
var declarationSnippets = map[string]declarationSnippet{
	"memory": {
		detail: `memory Name { type: "kv"|"json"|"append_log"|"jsonl", path? }`,
		body: "memory ${1:Name} {\n" +
			"\ttype: \"${2:json}\"\n" +
			"\tpath: \"${3:.mhl/state.json}\"\n" +
			"}\n$0",
	},
	"agent": {
		detail: `agent Name { engine, command, args, ... }`,
		body: "agent ${1:Name} {\n" +
			"\tengine: \"${2:cli/claude-code}\"\n" +
			"\tcommand: \"${3:claude}\"\n" +
			"\targs: [\"-p\", \"\\${prompt}\"]\n" +
			"}\n$0",
	},
	"tool": {
		detail: `tool Name { method(param: type) -> ... }`,
		body: "tool ${1:Name} {\n" +
			"\t${2:method}(${3:param}: ${4:string}) -> {\n" +
			"\t\treturn ${3:param}\n" +
			"\t}\n" +
			"}\n$0",
	},
	"prompt": {
		detail: `prompt Name(param: type) { """ ... """ }`,
		body: "prompt ${1:Name}(${2:param}: ${3:string}) {\n" +
			"\t\"\"\"\n" +
			"\t${4:Instructions for the agent.}\n" +
			"\t\"\"\"\n" +
			"}\n$0",
	},
	"pipeline": {
		detail: `pipeline Name { step Name { ... } }`,
		body: "pipeline ${1:Name} {\n" +
			"\tstep ${2:First} {\n" +
			"\t\t${0:log.info(\"running\")}\n" +
			"\t}\n" +
			"}",
	},
	"workflow": {
		detail: `workflow Name { step Name { ... } } — goto-capable pipeline`,
		body: "workflow ${1:Name} {\n" +
			"\tstep ${2:First} {\n" +
			"\t\t${0:log.info(\"running\")}\n" +
			"\t}\n" +
			"}",
	},
	"router": {
		detail: `router Name { agents: [...], select: (prompt) -> ... }`,
		body: "router ${1:Name} {\n" +
			"\tagents: [${2:AgentA}, ${3:AgentB}]\n\n" +
			"\tselect: (prompt) -> {\n" +
			"\t\treturn nameof(${2:AgentA})\n" +
			"\t}\n" +
			"}\n$0",
	},
	"extension": {
		detail: `extension mcp Name { transport, command/url, ... }`,
		body: "extension mcp ${1:Name} {\n" +
			"\ttransport: \"${2:stdio}\"\n" +
			"\tcommand: \"${3:npx}\"\n" +
			"\targs: [${4:\"-y\", \"@modelcontextprotocol/server-filesystem\", \".\"}]\n" +
			"}\n$0",
	},
	"enum": {
		detail: `enum Name { Variant, ... }`,
		body:   "enum ${1:Status} { ${2:Draft}, ${3:Published}, ${4:Archived} }\n$0",
	},
	"test": {
		detail: `test Name { describe name { assertions... } }`,
		body: "test ${1:Name} {\n" +
			"\tdescribe ${2:behavior} {\n" +
			"\t\t${0:are_equal(1, 1)}\n" +
			"\t}\n" +
			"}",
	},
}

// plainTextItems downgrades every snippet-format item back to a bare keyword
// insertion, for a client whose "initialize" capabilities didn't claim
// snippetSupport — sending it InsertTextFormat=2 anyway would risk it
// inserting the literal "${1:Name}" placeholder syntax as text instead of
// rendering tabstops.
func plainTextItems(items []completionItem) []completionItem {
	for i := range items {
		if items[i].InsertTextFormat == insertTextFormatSnippet {
			items[i].Kind = kindKeyword
			items[i].InsertText = ""
			items[i].InsertTextFormat = 0
		}
	}
	return items
}

// typeKeywords is internal/lang/types' declarable vocabulary, offered
// whenever the cursor sits in a recognized type-annotation position.
// SessionContext/StepContext/FailureContext are the three builtin object
// shapes the pipeline lifecycle hooks bind their lambda parameter to
// (types.go's `aliases` table) — real global types, usable in any `: Type`
// position, not just inferred for a hook's own parameter (see
// hookcontext.go for that inference).
var typeKeywords = []string{"string", "number", "bool", "array", "object", "any", "SessionContext", "StepContext", "FailureContext"}

// isTypeAnnotationPosition is a pragmatic heuristic, not a real parse —
// consistent with blockStack/classifyHeader's own best-effort approach
// elsewhere in this package. It recognizes two shapes: a pipeline `input
// name: ` line (line itself starts with "input "), and a tool/prompt method
// parameter list (`name(param: ` — the enclosing block is blockOther or
// blockTool (a tool method's parameter list sits directly inside the
// tool's own body), and the line has an unclosed "(" before the match).
func isTypeAnnotationPosition(linePrefix, text string, pos position) bool {
	if strings.HasPrefix(strings.TrimSpace(linePrefix), "input ") {
		return true
	}
	stack := blockStack(textUpToPosition(text, pos))
	if len(stack) == 0 {
		return false
	}
	if top := stack[len(stack)-1].Kind; top != blockOther && top != blockTool {
		return false
	}
	return strings.Count(linePrefix, "(") > strings.Count(linePrefix, ")")
}

// completionAt computes the completion list for path/text at pos, following
// three modes: member completion right after "target." (only target's own
// methods), property-name completion when the cursor sits directly inside a
// recognized declaration body or nested config object (an `agent { }`, a
// `pipeline { }`/`loop pipeline { }`, or one of their own nested `checkpoint
// { }`/`repeat { }`/`retry { }`/`cache { }`/`rate_limit { }` blocks — see
// blockStack/propertyItemsFor), appended to the general list rather than
// replacing it since the block classifier is best-effort, not authoritative
// — or general completion everywhere else (keywords + every symbol in
// scope).
func completionAt(path, text string, pos position) []completionItem {
	linePrefix := textBeforePosition(text, pos)

	if m := memberAccessRe.FindStringSubmatch(linePrefix); m != nil {
		target := m[1]
		if target == "self" {
			return selfCompletionAt(path, text, pos)
		}
		if items, ok := hookParamCompletionAt(text, pos, target); ok {
			return items
		}
		for _, s := range documentSymbols(path, text) {
			if s.Name == target {
				return methodItems(path, s)
			}
		}
		return nil
	}

	if typeAnnotationRe.MatchString(linePrefix) && isTypeAnnotationPosition(linePrefix, text, pos) {
		items := make([]completionItem, 0, len(typeKeywords))
		for _, kw := range typeKeywords {
			items = append(items, completionItem{Label: kw, Kind: kindKeyword})
		}
		// A `type X = ...` alias or an `enum` name is usable anywhere a
		// builtin type keyword is.
		for _, s := range documentSymbols(path, text) {
			if s.Kind == symType || s.Kind == symEnum {
				items = append(items, completionItem{Label: s.Name, Kind: kindKeyword, Detail: s.Kind.label()})
			}
		}
		return items
	}

	items := make([]completionItem, 0, len(keywords))
	for _, kw := range keywords {
		item := completionItem{Label: kw, Kind: kindKeyword}
		if sn, ok := declarationSnippets[kw]; ok {
			item.Kind = kindSnippet
			item.Detail = sn.detail
			item.InsertText = sn.body
			item.InsertTextFormat = insertTextFormatSnippet
		}
		items = append(items, item)
	}
	for _, s := range documentSymbols(path, text) {
		items = append(items, completionItem{
			Label:  s.Name,
			Kind:   symbolItemKind(s.Kind),
			Detail: s.Kind.label(),
		})
	}
	items = append(items, propertyItemsFor(path, blockStack(textUpToPosition(text, pos)))...)
	if isGotoTargetPosition(linePrefix) {
		items = append(items, gotoTargetItems(path, text, pos)...)
	}
	return items
}

func methodItems(path string, s symbol) []completionItem {
	items := make([]completionItem, 0, len(s.Methods))
	for _, m := range s.Methods {
		item := completionItem{
			Label:      m,
			Kind:       kindMethod,
			Detail:     s.Kind.label() + " method",
			InsertText: m + "(",
		}
		// Replace the generic "<kind> method" detail with the real
		// signature, and attach its parameter doc, whenever one is known
		// (every native op, collection method, and declared-construct
		// method — see signatures.go).
		if sg, ok := signatureForMethod(path, s, m); ok {
			item.Detail = sg.Label
			if sg.Doc != "" {
				item.Documentation = &markupContent{Kind: "markdown", Value: sg.Doc}
			}
		}
		items = append(items, item)
	}
	return items
}

func symbolItemKind(k symbolKind) int {
	switch k {
	case symAgent, symRouter, symTool, symMemory:
		return kindClass
	case symPrompt, symPipeline, symExtension:
		return kindProperty
	case symNative:
		return kindModule
	case symType, symEnum:
		return kindKeyword
	default:
		return kindText
	}
}

// textBeforePosition returns the text of line pos.Line up to (not
// including) character pos.Character — the slice completion logic matches
// trigger patterns like "name." against.
func textBeforePosition(text string, pos position) string {
	lines := strings.Split(text, "\n")
	if pos.Line < 0 || pos.Line >= len(lines) {
		return ""
	}
	line := lines[pos.Line]
	// Character is a UTF-16 code-unit offset per the LSP spec; MHL source is
	// expected to be ASCII/BMP-only in practice, so treating it as a byte
	// offset here is a deliberate simplification, not a spec-correct decode.
	if pos.Character < 0 {
		return ""
	}
	if pos.Character > len(line) {
		return line
	}
	return line[:pos.Character]
}
