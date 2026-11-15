package lsp

import "testing"

// Inside a parallel group's merge, `<param>.` completes the group's branch
// step names; `merge` itself is a keyword completion.
func TestMergeParamCompletionListsBranchSteps(t *testing.T) {
	src, pos := posAtMarker(t, `
workflow W {
    var reply = []
    parallel Fan timeout 30s {
        step Left  { self.reply = ["a"] }
        step Right { self.reply = ["b"] }
        merge (branches) {
            self.reply = branches.§
        }
    }
}
`)
	items := completionAt("main.mh", src, pos)
	for _, step := range []string{"Left", "Right"} {
		if !hasLabel(items, step) {
			t.Errorf("missing branch step %q in completion: %+v", step, items)
		}
	}
	if len(items) != 2 {
		t.Errorf("want only the two branch steps, got %+v", items)
	}

	src, pos = posAtMarker(t, `
workflow W {
    parallel Fan {
        step Left { log("l") }
        me§
    }
}
`)
	if !hasLabel(completionAt("main.mh", src, pos), "merge") {
		t.Errorf("`merge` missing from keyword completion")
	}
}

// Inside a parallel group, `self.` also offers the group's own vars; outside
// it, it does not.
func TestSelfCompletionOffersGroupVarsInsideTheGroup(t *testing.T) {
	src, pos := posAtMarker(t, `
workflow W {
    var reply = []
    parallel Fan {
        var left = ""
        step L {
            self.§
        }
    }
}
`)
	items := completionAt("main.mh", src, pos)
	if !hasLabel(items, "left") || !hasLabel(items, "reply") {
		t.Fatalf("want left and reply, got %+v", items)
	}
	src, pos = posAtMarker(t, `
workflow W {
    var reply = []
    parallel Fan {
        var left = ""
        step L { log("l") }
    }
    step After {
        self.§
    }
}
`)
	if hasLabel(completionAt("main.mh", src, pos), "left") {
		t.Fatalf("a group var offered outside its group")
	}
}
