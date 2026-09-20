package lsp

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
	"github.com/mh-language/mhl-core-runtime/internal/lang/parser"
)

// gotoTargetRe matches an in-progress `goto <partial>` at the end of the
// text before the cursor — deliberately not `goto match ...`, whose subject
// expression isn't a step name at all (that shape is excluded by requiring
// no further `(` / other tokens between `goto` and the cursor). A step name
// is never reachable via `self.<name>` (unlike a pipeline var/input/mem,
// `self.` never disambiguates a step: a step name has no local-shadow
// concept a bare `goto Foo` could ever be confused with, since it isn't an
// expression position at all) — this is why goto's own target position, not
// self-completion, is where step-name completion belongs.
var gotoTargetRe = regexp.MustCompile(`\bgoto\s+(?:nameof\(\s*)?\w*$`)

// isGotoTargetPosition reports whether linePrefix ends in an in-progress
// `goto <partial>` (or `goto nameof(<partial>`) — not `goto match`, whose
// subject is an arbitrary expression.
func isGotoTargetPosition(linePrefix string) bool {
	if !gotoTargetRe.MatchString(linePrefix) {
		return false
	}
	return !strings.Contains(linePrefix, "match")
}

// enclosingPipelineName returns the name of the pipeline/workflow directly
// enclosing pos — the innermost blockPipeline/blockLoopPipeline frame on
// blockStack, skipping past any nested block (a step body, an `if`, a
// `parallel` group) in between, since a `goto` reaches every such block. ok
// is false when pos isn't inside a pipeline/workflow body at all.
func enclosingPipelineName(text string, pos position) (name string, ok bool) {
	stack := blockStack(textUpToPosition(text, pos))
	for i := len(stack) - 1; i >= 0; i-- {
		switch stack[i].Kind {
		case blockPipeline, blockLoopPipeline:
			return stack[i].Name, true
		case blockTool, blockAgent, blockRouter, blockExtension:
			// None of these can nest inside, or be nested inside, a
			// pipeline/workflow body — stop looking outward.
			return "", false
		}
	}
	return "", false
}

// gotoTargetItems lists every step name a `goto` at pos could legally name —
// every step (including a `parallel` group's own branch steps) declared in
// the pipeline/workflow directly enclosing pos, plus, when that declaration
// is `partial`, every sibling fragment file's steps too (a goto in one
// fragment routinely targets a step another fragment declares — see
// ast.Pipeline's own doc comment on `partial`). Returns nil when pos isn't
// inside a pipeline/workflow body at all (blockStack's top frame isn't
// blockPipeline/blockLoopPipeline) or that declaration can't be found.
func gotoTargetItems(path, text string, pos position) []completionItem {
	pipelineName, ok := enclosingPipelineName(text, pos)
	if !ok {
		return nil
	}
	prog, err := parser.Parse(repairForParse(text, pos))
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
	items := pipelineStepItems(target)
	if target.Partial {
		items = append(items, partialSiblingStepItems(path, target)...)
	}
	return dedupeCompletionItems(items)
}

// pipelineStepItems returns a completion item for every step p's own Body
// declares directly — a plain `step`, and each branch of a `parallel` group.
func pipelineStepItems(p *ast.Pipeline) []completionItem {
	var items []completionItem
	for _, m := range p.Body {
		switch {
		case m.Step != nil:
			items = append(items, completionItem{Label: m.Step.Name, Kind: kindMethod, Detail: "step"})
		case m.Parallel != nil:
			for _, s := range m.Parallel.Steps {
				items = append(items, completionItem{Label: s.Name, Kind: kindMethod, Detail: "step (parallel " + m.Parallel.Name + ")"})
			}
		}
	}
	return items
}

// partialSiblingStepItems mirrors partialSiblingScopeItems (selfcompletion.go)
// for step names instead of input/var/mem — every other .mh file in target's
// directory that declares a `partial` fragment sharing its Name and Kind.
func partialSiblingStepItems(path string, target *ast.Pipeline) []completionItem {
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
				items = append(items, pipelineStepItems(p)...)
			}
		}
	}
	return items
}

// gotoTargetPrefixRe matches the text immediately before an already-typed
// goto target word — definitionAt's counterpart to gotoTargetRe, which
// matches a still-being-typed one instead.
var gotoTargetPrefixRe = regexp.MustCompile(`\bgoto\s+(?:nameof\(\s*)?$`)

// isGotoTargetWord reports whether the word starting at byte offset start on
// line is a `goto <word>` target (not `goto match`'s subject expression).
func isGotoTargetWord(line string, start int) bool {
	if start > len(line) {
		return false
	}
	return gotoTargetPrefixRe.MatchString(line[:start])
}

// findStepDeclaration resolves stepName to the location of its `step` (or a
// `parallel` group branch step) declaration inside the pipeline/workflow
// named enclosingPipeline — first in path's own buffer, then, only when
// that declaration is `partial`, every sibling fragment file's own body too
// (mirroring gotoTargetItems' own reach, and selfcompletion.go's
// partialSiblingScopeItems before it). ok is false when enclosingPipeline
// can't be found in path's buffer at all.
func findStepDeclaration(path, text, enclosingPipeline, stepName string) (location, bool) {
	prog, err := parser.Parse(text)
	if err != nil {
		return location{}, false
	}
	var target *ast.Pipeline
	for _, decl := range prog.Decls {
		if decl.Pipeline != nil && decl.Pipeline.Name == enclosingPipeline {
			target = decl.Pipeline
			break
		}
	}
	if target == nil {
		return location{}, false
	}
	if loc, ok := stepLocationInText(path, text, target.Pos.Offset, stepName); ok {
		return loc, true
	}
	if !target.Partial {
		return location{}, false
	}
	dir := filepath.Dir(path)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return location{}, false
	}
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
		srcText := string(src)
		sp, err := parser.Parse(srcText)
		if err != nil {
			continue
		}
		for _, decl := range sp.Decls {
			p := decl.Pipeline
			if p != nil && p.Partial && p.Name == target.Name && p.Kind == target.Kind {
				if loc, ok := stepLocationInText(full, srcText, p.Pos.Offset, stepName); ok {
					return loc, true
				}
			}
		}
	}
	return location{}, false
}

// stepLocationInText finds stepName's declaration textually within the
// pipeline/workflow whose header starts at byte offset pipelinePos in
// src — blockBounds (definition.go) locates that declaration's own `{...}`
// body from its header position exactly as findMember already does for a
// tool's body, so a same-named step declared in a different pipeline
// elsewhere in the same file is never mistaken for this one.
func stepLocationInText(path, src string, pipelinePos int, stepName string) (location, bool) {
	bodyStart, bodyEnd, ok := blockBounds(src, pipelinePos)
	if !ok {
		return location{}, false
	}
	re := regexp.MustCompile(`(?m)^[ \t]*(?:entry[ \t]+)?step[ \t]+(` + regexp.QuoteMeta(stepName) + `)\b`)
	m := re.FindStringSubmatchIndex(src[bodyStart:bodyEnd])
	if m == nil {
		return location{}, false
	}
	off := bodyStart + m[2]
	return location{URI: pathToURI(path), Range: identRange(src, off, stepName)}, true
}
