package lsp

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
	"github.com/mh-language/mhl-core-runtime/internal/lang/parser"
)

// selfCompletionAt returns the completion list for `self.` at pos, or nil
// when the cursor isn't inside a tool method or a pipeline/workflow step —
// the only two places `self` means anything (interpreter.evalCtx.selfTool /
// pipelineName). It's checked before the generic memberAccessRe path in
// completionAt because "self" is never a real top-level symbol
// documentSymbols would find.
//
// Inside a tool: offers the tool's own other method names (sibling
// self.method() calls) — this half of `self.` has existed since before
// `partial`, but completion for it never did; fixed here as the same class
// of gap.
//
// Inside a pipeline/workflow step: offers every declared `input`/`var`/
// `mem` name of that pipeline — and, when the declaration is `partial`,
// every sibling .mh file's matching `partial` fragment too. This is the
// actual point of the feature: a `partial` pipeline's shared state is
// otherwise invisible from any one fragment file (a dev has to go read
// every other fragment to discover `current_artifact`/`pending_data`/...
// exist at all) — typing `self.` surfaces all of it regardless of which
// file declared what.
//
// text is repaired before parsing (see repairForParse): the moment
// someone has typed "self." and not yet chosen a member is, on its own, a
// dangling-`.` parse error for the *whole* file — the same reason every
// other completion path in this package (documentSymbols et al.) carries
// its own regex fallback. Completion only needs the file's declaration
// *structure*, not a valid expression at the cursor, so a harmless
// placeholder identifier there is enough to keep the real parser in play
// instead of falling back to a second, hand-maintained extractor.
func selfCompletionAt(path, text string, pos position) []completionItem {
	repaired := repairForParse(text, pos)
	stack := blockStack(textUpToPosition(repaired, pos))
	for i := len(stack) - 1; i >= 0; i-- {
		switch stack[i].Kind {
		case blockTool:
			for _, s := range documentSymbols(path, repaired) {
				if s.Kind == symTool && s.Name == stack[i].Name {
					return methodItems(path, s)
				}
			}
			return nil
		case blockPipeline, blockLoopPipeline:
			return pipelineScopeItems(path, repaired, stack[i].Name)
		}
	}
	return nil
}

// repairForParse inserts a harmless placeholder identifier at pos so a
// buffer that's mid-edit right at the cursor still parses as a whole. Only
// ever needed for the file being completed in — a sibling fragment read
// off disk (partialSiblingScopeItems) is never mid-edit from this session's
// point of view and is parsed as-is.
func repairForParse(text string, pos position) string {
	lines := strings.Split(text, "\n")
	if pos.Line < 0 || pos.Line >= len(lines) {
		return text
	}
	line := lines[pos.Line]
	c := pos.Character
	if c < 0 {
		c = 0
	}
	if c > len(line) {
		c = len(line)
	}
	lines[pos.Line] = line[:c] + "__mhl_self_completion__" + line[c:]
	return strings.Join(lines, "\n")
}

// pipelineScopeItems collects a self.<name> completion item for every
// input/var/mem declared at the top level of pipelineName: text's own
// (already-repaired) declaration of it, plus — only when that declaration
// is `partial` — every sibling .mh file in the same directory declaring
// another `partial` fragment of the same name and kind, mirroring
// definition.go's siblings()/"flat same-directory" resolution for the same
// reason (a fragment's own `import`s don't need to be parsed to find its
// siblings; they realistically already sit next to it).
func pipelineScopeItems(path, text, pipelineName string) []completionItem {
	prog, err := parser.Parse(text)
	if err != nil {
		return nil
	}
	var target *ast.Pipeline
	for _, decl := range prog.Decls {
		if decl.Pipeline != nil && decl.Pipeline.Name == pipelineName {
			target = decl.Pipeline
			break
		}
	}
	if target == nil {
		return nil
	}
	items := pipelineMemberItems(target)
	if target.Partial {
		items = append(items, partialSiblingScopeItems(path, target)...)
	}
	return dedupeCompletionItems(items)
}

// partialSiblingScopeItems scans every other .mh file in target's directory
// for a `partial` fragment sharing its Name and Kind, returning their own
// input/var/mem items too.
func partialSiblingScopeItems(path string, target *ast.Pipeline) []completionItem {
	dir := filepath.Dir(path)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var items []completionItem
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
		sp, err := parser.Parse(string(src))
		if err != nil {
			continue
		}
		for _, decl := range sp.Decls {
			p := decl.Pipeline
			if p != nil && p.Partial && p.Name == target.Name && p.Kind == target.Kind {
				items = append(items, pipelineMemberItems(p)...)
			}
		}
	}
	return items
}

// pipelineMemberItems returns a self.<name> completion item for each
// input/var/mem declared directly in p's own Body (no cross-file merging —
// that's pipelineScopeItems' job).
func pipelineMemberItems(p *ast.Pipeline) []completionItem {
	var items []completionItem
	for _, m := range p.Body {
		switch {
		case m.Input != nil:
			items = append(items, completionItem{Label: m.Input.Name, Kind: kindVariable, Detail: "input"})
		case m.Var != nil:
			items = append(items, completionItem{Label: m.Var.Name, Kind: kindVariable, Detail: "var"})
		case m.Mem != nil:
			items = append(items, completionItem{Label: m.Mem.Name, Kind: kindVariable, Detail: "mem"})
		}
	}
	return items
}

// dedupeCompletionItems drops a later item whose Label repeats an earlier
// one — the same diamond-fragment concern dedupeSymbols exists for, just
// for this file's own completion-item slice instead of the symbol table.
func dedupeCompletionItems(items []completionItem) []completionItem {
	seen := make(map[string]bool, len(items))
	out := make([]completionItem, 0, len(items))
	for _, it := range items {
		if seen[it.Label] {
			continue
		}
		seen[it.Label] = true
		out = append(out, it)
	}
	return out
}
