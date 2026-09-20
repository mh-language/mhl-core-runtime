package lint_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/lang/lint"
)

// TestLintPartialMergesFragmentsAcrossFiles is the clean case: the primary
// file bare-imports a sibling fragment, together they form one valid
// workflow (one entry step, every goto target resolves), and lint reports
// nothing.
func TestLintPartialMergesFragmentsAcrossFiles(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "fragment.mh"), `
partial workflow Discovery {
    step Gate {}
}
`)
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
import "fragment.mh"

partial workflow Discovery {
    entry step Dispatch {
        goto Gate
    }
}
`)
	if findings := lint.File(main); len(findings) != 0 {
		t.Fatalf("expected no findings, got %+v", findings)
	}
}

// TestLintPartialLoneFragmentWithoutEntryIsSilent is the fix for a real
// false positive reported against production code: every fragment file
// used to show a permanent "expected exactly one entry step, found 0" the
// moment it was opened, because the LSP lints each open document on its
// own and a lone fragment (its siblings never pulled in from here)
// legitimately has zero. There's no way to tell that apart from a genuinely
// forgotten `entry` from inside this one file, so it's silent here —
// runtime.FindPipeline still catches a truly missing entry the moment
// anything tries to run it for real.
func TestLintPartialLoneFragmentWithoutEntryIsSilent(t *testing.T) {
	dir := t.TempDir()
	frag := filepath.Join(dir, "fragment.mh")
	write(t, frag, `
partial workflow Discovery {
    step Gate {}
}
`)
	if findings := lint.File(frag); len(findings) != 0 {
		t.Fatalf("expected no findings for a lone fragment, got %+v", findings)
	}
}

// TestLintPartialMergedFragmentsWithoutEntryIsFlagged is
// TestLintPartialLoneFragmentWithoutEntryIsSilent's counterpart: once a
// whole-file `import` actually merges more than one fragment together,
// "nobody marked an entry anywhere" stops being ambiguous and is flagged
// again — this pass has real evidence it's looking at the complete,
// assembled picture.
func TestLintPartialMergedFragmentsWithoutEntryIsFlagged(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "fragment.mh"), `
partial workflow Discovery {
    step Gate {}
}
`)
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
import "fragment.mh"

partial workflow Discovery {
    step Dispatch {
        goto Gate
    }
}
`)
	findings := lint.File(main)
	found := false
	for _, f := range findings {
		if strings.Contains(f.Message, "expected exactly one `entry step`") && strings.Contains(f.Message, "found 0") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a found-0 finding once fragments are actually merged, got %+v", findings)
	}
}

// TestLintPartialLoneFragmentWithTwoEntriesIsStillFlagged: unlike "found
// 0", "found 2" (or more) is unambiguous regardless of how many fragments
// are visible — a fragment declaring two entry steps itself is always
// wrong — so it's still flagged even standalone.
func TestLintPartialLoneFragmentWithTwoEntriesIsStillFlagged(t *testing.T) {
	dir := t.TempDir()
	frag := filepath.Join(dir, "fragment.mh")
	write(t, frag, `
partial workflow Discovery {
    entry step A {}
    entry step B {}
}
`)
	findings := lint.File(frag)
	found := false
	for _, f := range findings {
		if strings.Contains(f.Message, "found 2") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a found-2 finding even for a lone fragment, got %+v", findings)
	}
}

func TestLintPartialTwoEntrySteps(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
partial workflow Discovery {
    entry step A {}
    entry step B {}
}
`)
	findings := lint.File(main)
	if len(findings) != 1 || !strings.Contains(findings[0].Message, "found 2") {
		t.Fatalf("expected a found-2 finding, got %+v", findings)
	}
}

func TestLintEntryOutsidePartialIsRejected(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
workflow Discovery {
    entry step A {}
}
`)
	findings := lint.File(main)
	if len(findings) != 1 || !strings.Contains(findings[0].Message, "isn't `partial`") {
		t.Fatalf("expected an entry-outside-partial finding, got %+v", findings)
	}
}

func TestLintPartialKindMismatch(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "fragment.mh"), `
partial pipeline Discovery {
    step Gate {}
}
`)
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
import "fragment.mh"

partial workflow Discovery {
    entry step Dispatch {}
}
`)
	findings := lint.File(main)
	found := false
	for _, f := range findings {
		if strings.Contains(f.Message, "one fragment declares it") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a kind-mismatch finding, got %+v", findings)
	}
}

func TestLintPartialLoopDeclaredTwice(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "fragment.mh"), `
partial loop workflow Discovery {
    step Gate {}
}
`)
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
import "fragment.mh"

partial loop workflow Discovery {
    entry step Dispatch {}
}
`)
	findings := lint.File(main)
	found := false
	for _, f := range findings {
		if strings.Contains(f.Message, "`loop` is declared on more than one fragment") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a loop-declared-twice finding, got %+v", findings)
	}
}

func TestLintWholeFileImportRejectsBracesRequirement(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "shared.mh"), `
export tool Helper {
    ping() -> "pong"
}
`)
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
import "shared.mh"

pipeline P {
    step S {
        log(Helper.ping())
    }
}
`)
	if findings := lint.File(main); len(findings) != 0 {
		t.Fatalf("expected no findings, got %+v", findings)
	}
}

// TestLintPartialLoneFragmentReferencingSiblingVarIsSilent is the fix for a
// second false-positive class discovered while splitting a real production
// workflow: checkAgentCalls (which drives checkStatements/checkAssignTarget)
// depends on the same whole-body knowledge checkPipelineGoto does. A lone
// fragment assigning to a pipeline-scoped `var` declared only in a sibling
// (unseen) fragment must not be flagged "undefined variable" any more than
// a `goto` reaching an unseen step is flagged as targeting a missing step.
func TestLintPartialLoneFragmentReferencingSiblingVarIsSilent(t *testing.T) {
	dir := t.TempDir()
	frag := filepath.Join(dir, "fragment.mh")
	write(t, frag, `
partial workflow Discovery {
    step Gate {
        pending_data = "set from a var this file never declares"
        goto Done
    }
}
`)
	if findings := lint.File(frag); len(findings) != 0 {
		t.Fatalf("expected no findings for a lone fragment referencing a sibling's var, got %+v", findings)
	}
}

// TestLintPartialAssembledFileStillCatchesRealBugs is
// TestLintPartialLoneFragmentReferencingSiblingVarIsSilent's counterpart:
// once every fragment is actually merged (the file that bare-imports them),
// a genuinely undefined variable and a genuinely missing goto target are
// still real errors — the leniency is scoped to "this file alone can't see
// the whole picture", not a blanket exemption for `partial` pipelines.
func TestLintPartialAssembledFileStillCatchesRealBugs(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "fragment.mh"), `
partial workflow Discovery {
    step Gate {
        nonexistent_var = "boom"
    }
}
`)
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
import "fragment.mh"

partial workflow Discovery {
    entry step Dispatch {
        goto NoSuchStep
    }
}
`)
	findings := lint.File(main)
	var messages []string
	for _, f := range findings {
		messages = append(messages, f.Message)
	}
	foundUndefined := false
	foundBadGoto := false
	for _, m := range messages {
		if strings.Contains(m, `undefined variable "nonexistent_var"`) {
			foundUndefined = true
		}
		if strings.Contains(m, "targets a step that isn't declared in it") {
			foundBadGoto = true
		}
	}
	if !foundUndefined {
		t.Errorf("expected an undefined-variable finding once fragments are merged, got %+v", messages)
	}
	if !foundBadGoto {
		t.Errorf("expected a bad-goto-target finding once fragments are merged, got %+v", messages)
	}
}

// TestLintPartialFallthroughAcrossFragmentBoundary is the regression for a
// real production incident: an empty-bodied convergence step (`Done`) sat
// last in one fragment while a sibling fragment was still merged after it
// (a whole-file `import` always appends after the importer's own
// declarations — see interpreter.resolveImports), so a run that reached
// Done silently continued into the next fragment's first step instead of
// completing — firing an unwanted LLM call downstream in the real incident.
func TestLintPartialFallthroughAcrossFragmentBoundary(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "fragment.mh"), `
partial workflow Discovery {
    step BriefGenerate {
        goto Gate
    }
}
`)
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
import "fragment.mh"

partial workflow Discovery {
    entry step Dispatch {
        goto Gate
    }
    step Gate {
        goto BriefGenerate
    }
    step Done {
    }
}
`)
	findings := lint.File(main)
	found := false
	for _, f := range findings {
		if strings.Contains(f.Message, `step "Done"`) && strings.Contains(f.Message, `fall through into "BriefGenerate"`) {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a fallthrough finding for Done, got %+v", findings)
	}
}

// TestLintPartialFallthroughWithinOneFragmentIsSilent is the false-positive
// fix: a step with no explicit `goto` that isn't the pipeline's last step
// is completely normal *within* one file (mhl's execution model defaults
// to sequential fallthrough everywhere — a real, common pattern this exact
// check regressed against internal/cli's own dry-run tests before being
// narrowed to fragment *boundaries* specifically: only a fragment's own
// *last* member is ever a boundary-check candidate). Dispatch and Gate
// have no `goto` at all but aren't their fragment's last member (Third
// is, and it does divert) — a real merge would have main.mh's own
// declarations first (imports only ever append after the importer's own —
// see interpreter.resolveImports), so this fragment really is
// [Dispatch, Gate, Third].
func TestLintPartialFallthroughWithinOneFragmentIsSilent(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "fragment.mh"), `
partial workflow Discovery {
    step Last {
    }
}
`)
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
import "fragment.mh"

partial workflow Discovery {
    entry step Dispatch {
    }
    step Gate {
    }
    step Third {
        goto Last
    }
}
`)
	if findings := lint.File(main); len(findings) != 0 {
		t.Fatalf("expected no findings for same-fragment fallthrough, got %+v", findings)
	}
}

// TestLintPartialFallthroughLastFragmentIsSilent: the truly-last fragment's
// own steps are never checked as boundary sources — there's nothing after
// them to silently fall into. main.mh's own declarations always merge
// *before* whatever it `import`s (see interpreter.resolveImports), so
// fragment.mh — the only thing main.mh imports — is the one that ends up
// last here, and its step Last (no `goto`) is correctly never flagged.
func TestLintPartialFallthroughLastFragmentIsSilent(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "fragment.mh"), `
partial workflow Discovery {
    step Last {
        // no goto — fine, this is the pipeline's actual last step
    }
}
`)
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
import "fragment.mh"

partial workflow Discovery {
    entry step Dispatch {
        goto Last
    }
}
`)
	if findings := lint.File(main); len(findings) != 0 {
		t.Fatalf("expected no findings, the last fragment's steps are never boundary-checked, got %+v", findings)
	}
}

// TestLintPartialFallthroughNotCheckedForPlainPipeline: a `partial
// pipeline` (not `workflow`) has no `goto` at all — every fragment
// boundary necessarily falls through by design, so it's never flagged.
func TestLintPartialFallthroughNotCheckedForPlainPipeline(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "fragment.mh"), `
partial pipeline Discovery {
    step Second {
    }
}
`)
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
import "fragment.mh"

partial pipeline Discovery {
    entry step First {
    }
}
`)
	if findings := lint.File(main); len(findings) != 0 {
		t.Fatalf("expected no findings for a partial pipeline (not workflow), got %+v", findings)
	}
}

// TestLintPartialFallthroughCompleteCountsAsDiverting: a fragment's last
// step ending in a bare `complete()` call is exactly as safe as one ending
// in `goto`/`fail(...)`/`pause(...)` — it stops the run right there,
// regardless of what fragment is merged in after it.
func TestLintPartialFallthroughCompleteCountsAsDiverting(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "fragment.mh"), `
partial workflow Discovery {
    step NeverChecked {
    }
}
`)
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
import "fragment.mh"

partial workflow Discovery {
    entry step Dispatch {
        complete()
    }
}
`)
	if findings := lint.File(main); len(findings) != 0 {
		t.Fatalf("expected no findings, complete() should count as diverting, got %+v", findings)
	}
}
