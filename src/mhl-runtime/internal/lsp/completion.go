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
// signatureTypeRe matches a pipeline/workflow header's type positions —
// the param's (`workflow W(req: ▮`) and the result's (`workflow W(req: In):
// ▮`, `pipeline W: ▮`) — where the same type vocabulary is offered.
var signatureTypeRe = regexp.MustCompile(`^\s*(?:partial\s+)?(?:loop\s+)?(?:pipeline|workflow)\s+\w+\s*(?:\(\s*\w+\s*:\s*\w*|(?:\([^()]*\))?\s*:\s*\w*)$`)

var typeAnnotationRe = regexp.MustCompile(`\b[A-Za-z_][A-Za-z0-9_]*\s*:\s*[A-Za-z_]*$`)

// declarationSnippet is one keyword's tab-through example body, in LSP
// snippet syntax ($1/${2:default}/$0 for the final cursor stop; same
// numbered placeholder repeated mirrors what's typed across occurrences).
// A literal `$` that must survive as mhl's own `${...}` interpolation syntax
// (e.g. an agent's args: ["-p", "${prompt}"]) is written `\$` so the client
// doesn't mistake it for a tabstop — see the "agent" entry below.
//
// Label overrides the completion item's label (e.g. "agent (full)"); left
// empty, the item is offered under the bare keyword itself. A keyword may
// have more than one variant — declarationSnippets below holds a slice per
// keyword — e.g. a minimal "agent" alongside a "agent (full)" that also
// covers retry/cache/rate_limit/fallback/before/after, since the minimal
// shape alone doesn't hint that any of those exist.
type declarationSnippet struct {
	label  string
	body   string
	detail string
}

// declarationSnippets gives a handful of top-level declaration keywords a
// fill-in-the-placeholders example, in place of just the bare word, so
// `mhl.mh`'s reference example (docs/site/Docs-Reference.dc.html) is one Tab
// away instead of a context switch to the docs. Kept to the constructs whose
// shape isn't obvious from the keyword alone; deliberately not exhaustive —
// see completionAt's general keyword loop for how this is applied.
var declarationSnippets = map[string][]declarationSnippet{
	"memory": {{
		detail: `memory Name { type: "kv"|"json"|"append_log"|"jsonl", path? }`,
		body: "memory ${1:Name} {\n" +
			"\ttype: \"${2:json}\"\n" +
			"\tpath: \"${3:.mhl/state.json}\"\n" +
			"}\n$0",
	}},
	"agent": {
		{
			detail: `agent Name { engine, command, args, ... }`,
			body: "agent ${1:Name} {\n" +
				"\tengine: \"${2:cli/claude-code}\"\n" +
				"\tcommand: \"${3:claude}\"\n" +
				"\targs: [\"-p\", \"\\${prompt}\"]\n" +
				"}\n$0",
		},
		{
			label:  "agent (full)",
			detail: `agent Name { ... retry, cache, rate_limit, fallback, before/after hooks, description }`,
			body: "agent ${1:Name} {\n" +
				"\tengine: \"${2:cli/claude-code}\"\n" +
				"\tcommand: \"${3:claude}\"\n" +
				"\targs: [\"-p\", \"\\${prompt}\"]\n\n" +
				"\tretry: {\n" +
				"\t\tmax_attempts: ${4:3}\n" +
				"\t\tdelay: ${5:2s}\n" +
				"\t\tretry_on: [500, 503, \"timeout\", \"rate_limit\"]\n" +
				"\t}\n" +
				"\tcache: { ttl: ${6:10m}, storage: \"disk\" }\n" +
				"\trate_limit: { requests_per_minute: ${7:20}, concurrency: ${8:2} }\n" +
				"\tfallback: [\n" +
				"\t\tagent {\n" +
				"\t\t\tcommand: \"echo\"\n" +
				"\t\t\targs: [\"fallback:\", \"\\${prompt}\"]\n" +
				"\t\t}\n" +
				"\t]\n\n" +
				"\tbefore: () -> {\n" +
				"\t\treturn { ${9:extra}: ${10:\"\"} }\n" +
				"\t}\n" +
				"\tafter: () -> {\n" +
				"\t\tlog.info(\"response is \" + result.size() + \" chars\")\n" +
				"\t}\n\n" +
				"\tdescription: \"${11:What this agent is for}\"\n" +
				"}\n$0",
		},
	},
	"tool": {{
		detail: `tool Name { method(param: type) -> ... }`,
		body: "tool ${1:Name} {\n" +
			"\t${2:method}(${3:param}: ${4:string}) -> {\n" +
			"\t\treturn ${3:param}\n" +
			"\t}\n" +
			"}\n$0",
	}},
	"prompt": {{
		detail: `prompt Name(param: type) { """ ... """ }`,
		body: "prompt ${1:Name}(${2:param}: ${3:string}) {\n" +
			"\t\"\"\"\n" +
			"\t${4:Instructions for the agent.}\n" +
			"\t\"\"\"\n" +
			"}\n$0",
	}},
	"pipeline": {
		{
			detail: `pipeline Name { step Name { ... } }`,
			body: "pipeline ${1:Name} {\n" +
				"\tstep ${2:First} {\n" +
				"\t\t${0:log.info(\"running\")}\n" +
				"\t}\n" +
				"}",
		},
		{
			label:  "pipeline (full)",
			detail: `pipeline Name { input, context (resume state), checkpoint, output, lifecycle hooks, steps }`,
			body: "pipeline ${1:Name} {\n" +
				"\tinput ${2:target}: string\n\n" +
				"\tcontext: {\n" +
				"\t\tsource: \"latest\"\n" +
				"\t\trequire: false\n" +
				"\t}\n" +
				"\tcheckpoint: { ttl: ${3:30d} }\n\n" +
				"\tvar ${4:result} = null\n\n" +
				"\toutput: {\n" +
				"\t\t${4:result}: ${4:result}\n" +
				"\t}\n\n" +
				"\tsession_start: (session) -> {\n" +
				"\t\tlog.info(\"start \" + session.session_id)\n" +
				"\t}\n" +
				"\tsession_end: (session) -> {\n" +
				"\t\tlog.info(\"end broke=\" + json.stringify(session.broke))\n" +
				"\t}\n" +
				"\tstep_start: (step) -> { log.info(\"-> \" + step.step) }\n" +
				"\tstep_end: (step) -> { log.info(\"<- \" + step.step) }\n" +
				"\tstop_failure: (failure) -> {\n" +
				"\t\tlog.error(failure.step + \": \" + failure.error)\n" +
				"\t}\n\n" +
				"\tstep ${5:First} {\n" +
				"\t\tif (context.vars.keys().contains(\"${4:result}\")) {\n" +
				"\t\t\t${4:result} = context.vars[\"${4:result}\"]\n" +
				"\t\t}\n" +
				"\t\t${6:log.info(\"running\")}\n" +
				"\t}\n\n" +
				"\tstep ${7:Second} {\n" +
				"\t\t$0\n" +
				"\t}\n" +
				"}",
		},
	},
	"workflow": {
		{
			detail: `workflow Name { step Name { ... } } — goto-capable pipeline`,
			body: "workflow ${1:Name} {\n" +
				"\tstep ${2:First} {\n" +
				"\t\t${0:log.info(\"running\")}\n" +
				"\t}\n" +
				"}",
		},
		{
			label:  "workflow (full)",
			detail: `workflow Name { input, checkpoint, output, lifecycle hooks, goto between steps }`,
			body: "workflow ${1:Name} {\n" +
				"\tinput ${2:environment}: string\n\n" +
				"\tvar ${3:approved} = false\n\n" +
				"\tcheckpoint: { ttl: ${4:30d} }\n\n" +
				"\toutput: {\n" +
				"\t\t${3:approved}: ${3:approved}\n" +
				"\t}\n\n" +
				"\tsession_start: (session) -> {\n" +
				"\t\tlog.info(\"start \" + session.session_id)\n" +
				"\t}\n" +
				"\tsession_end: (session) -> {\n" +
				"\t\tlog.info(\"end broke=\" + json.stringify(session.broke))\n" +
				"\t}\n" +
				"\tstep_start: (step) -> { log.info(\"-> \" + step.step) }\n" +
				"\tstep_end: (step) -> { log.info(\"<- \" + step.step) }\n" +
				"\tstop_failure: (failure) -> {\n" +
				"\t\tlog.error(failure.step + \": \" + failure.error)\n" +
				"\t}\n\n" +
				"\tstep ${5:Validate} {\n" +
				"\t\t${6:var result = cmd.exec([\"go\", \"test\", \"./...\"], timeout: 2m)}\n" +
				"\t\tif (result.exit_code != 0) fail(result.stderr)\n" +
				"\t\t${3:approved} = true\n" +
				"\t}\n\n" +
				"\tstep ${7:Publish} {\n" +
				"\t\tif (!${3:approved}) goto ${5:Validate}\n" +
				"\t\tlog.info(\"publishing to \" + ${2:environment})\n" +
				"\t\t$0\n" +
				"\t}\n" +
				"}",
		},
	},
	"router": {{
		detail: `router Name { agents: [...], select: (prompt) -> ... }`,
		body: "router ${1:Name} {\n" +
			"\tagents: [${2:AgentA}, ${3:AgentB}]\n\n" +
			"\tselect: (prompt) -> {\n" +
			"\t\treturn nameof(${2:AgentA})\n" +
			"\t}\n" +
			"}\n$0",
	}},
	"extension": {{
		detail: `extension mcp Name { transport, command/url, ... }`,
		body: "extension mcp ${1:Name} {\n" +
			"\ttransport: \"${2:stdio}\"\n" +
			"\tcommand: \"${3:npx}\"\n" +
			"\targs: [${4:\"-y\", \"@modelcontextprotocol/server-filesystem\", \".\"}]\n" +
			"}\n$0",
	}},
	"enum": {{
		detail: `enum Name { Variant, ... }`,
		body:   "enum ${1:Status} { ${2:Draft}, ${3:Published}, ${4:Archived} }\n$0",
	}},
	"test": {{
		detail: `test Name { describe name { assertions... } }`,
		body: "test ${1:Name} {\n" +
			"\tdescribe ${2:behavior} {\n" +
			"\t\t${0:are_equal(1, 1)}\n" +
			"\t}\n" +
			"}",
	}},
}

// keywordSet is `keywords` as a membership set — used by plainTextItems to
// tell a bare-keyword snippet item (label == the keyword itself, e.g.
// "memory") from a richer named variant (e.g. "agent (full)") whose label
// isn't valid mhl syntax to fall back to inserting as plain text.
var keywordSet = func() map[string]bool {
	m := make(map[string]bool, len(keywords))
	for _, kw := range keywords {
		m[kw] = true
	}
	return m
}()

// plainTextItems downgrades every snippet-format item back to a bare keyword
// insertion, for a client whose "initialize" capabilities didn't claim
// snippetSupport — sending it InsertTextFormat=2 anyway would risk it
// inserting the literal "${1:Name}" placeholder syntax as text instead of
// rendering tabstops. A named variant like "agent (full)" has no plain-text
// fallback worth offering (its label isn't itself valid mhl to insert), so
// it's dropped rather than downgraded — the bare "agent" item covers that
// case already.
func plainTextItems(items []completionItem) []completionItem {
	out := items[:0]
	for _, it := range items {
		if it.InsertTextFormat == insertTextFormatSnippet {
			if !keywordSet[it.Label] {
				continue
			}
			it.Kind = kindKeyword
			it.InsertText = ""
			it.InsertTextFormat = 0
		}
		out = append(out, it)
	}
	return out
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
		if items, ok := paramTypeCompletionAt(text, pos, target); ok {
			return items
		}
		if items, ok := pipelineParamCompletionAt(text, pos, target); ok {
			return items
		}
		for _, s := range documentSymbols(path, text) {
			if s.Name == target {
				return methodItems(path, text, s)
			}
		}
		return nil
	}

	if signatureTypeRe.MatchString(linePrefix) || (typeAnnotationRe.MatchString(linePrefix) && isTypeAnnotationPosition(linePrefix, text, pos)) {
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
		variants, ok := declarationSnippets[kw]
		if !ok {
			items = append(items, completionItem{Label: kw, Kind: kindKeyword})
			continue
		}
		for _, sn := range variants {
			label := sn.label
			if label == "" {
				label = kw
			}
			items = append(items, completionItem{
				Label:            label,
				Kind:             kindSnippet,
				Detail:           sn.detail,
				InsertText:       sn.body,
				InsertTextFormat: insertTextFormatSnippet,
			})
		}
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

func methodItems(path, text string, s symbol) []completionItem {
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
		if sg, ok := signatureForMethod(path, text, s, m); ok {
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
