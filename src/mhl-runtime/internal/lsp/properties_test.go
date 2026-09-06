package lsp

import (
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
)

// The editor's pipeline-body completion is derived from the shared
// ast.PipelineBodyProperties allow-list — every entry must show up (a
// LoopOnly one only under `loop`), and nothing extra.
func TestPipelinePropertyItemsMatchTheSharedAllowList(t *testing.T) {
	seen := map[string]bool{}
	for _, it := range pipelinePropertyItems {
		seen[it.Label] = true
	}
	loopSeen := map[string]bool{}
	for _, it := range loopPipelineExtraPropertyItems {
		loopSeen[it.Label] = true
	}

	want := map[string]bool{}
	for _, p := range ast.PipelineBodyProperties {
		want[p.Name] = true
		if p.LoopOnly {
			if !loopSeen[p.Name] {
				t.Errorf("LoopOnly property %q missing from loopPipelineExtraPropertyItems", p.Name)
			}
			if seen[p.Name] {
				t.Errorf("LoopOnly property %q must not be in the base list", p.Name)
			}
		} else if !seen[p.Name] {
			t.Errorf("property %q missing from pipelinePropertyItems", p.Name)
		}
	}
	for label := range seen {
		if !want[label] {
			t.Errorf("pipelinePropertyItems offers %q which is not in ast.PipelineBodyProperties", label)
		}
	}
	for label := range loopSeen {
		if !want[label] {
			t.Errorf("loopPipelineExtraPropertyItems offers %q which is not in ast.PipelineBodyProperties", label)
		}
	}
}
