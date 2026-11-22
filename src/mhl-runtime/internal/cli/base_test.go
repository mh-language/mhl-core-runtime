package cli_test

import (
	"encoding/json"
	"strings"
	"testing"
)

// `base.name` reads and writes the base fragment's state exactly like
// `self.name`, in an expression, an assignment and a `${...}` span; a
// variable that is itself named `base` keeps meaning the variable.
func TestBaseRefReadsAndWritesTheBaseFragmentState(t *testing.T) {
	run := stageInputRun(t, map[string]string{
		"pipeline.mh": `
import "use.mh"

partial workflow W {
    input dir: string
    var folder = ""
    var trail = []
    entry step Load {
        self.folder = dir + "/x"
        goto Use
    }
}
`,
		"use.mh": `
partial workflow W stage Use {
    step Read {
        base.trail = base.trail.append("${base.folder}")
        base.folder = base.folder + "!"
    }
    step Local {
        var base = {n: 1}
        self.trail = self.trail.append("local ${base.n}")
    }
}
`,
	})
	res := gateRun(t, run, "--input", "dir=/p")
	if res["ok"] != true {
		t.Fatalf("run = %v", res)
	}
	vars := res["vars"].(map[string]any)
	trail, _ := json.Marshal(vars["trail"])
	if string(trail) != `["/p/x","local 1"]` || vars["folder"] != "/p/x!" {
		t.Fatalf("vars = %v", vars)
	}
}

// `base.{f} = expr` assigns several of the base's vars from one object, and
// lint rejects a field the base does not declare.
func TestBaseDestructure(t *testing.T) {
	fragments := map[string]string{
		"pipeline.mh": `
import "use.mh"

partial workflow W {
    var plan = ""
    var price = 0
    entry step Load { goto Use }
}
`,
		"use.mh": `
partial workflow W stage Use {
    step Pick {
        base.{plan, cost: price} = {plan: "pro", cost: 30}
    }
}
`,
	}
	res := gateRun(t, stageInputRun(t, fragments))
	vars, _ := res["vars"].(map[string]any)
	if res["ok"] != true || vars["plan"] != "pro" || vars["price"] != 30.0 {
		t.Fatalf("run = %v", res)
	}

	fragments["use.mh"] = strings.Replace(fragments["use.mh"], "cost: price", "cost: nope", 1)
	out, err := stageInputRun(t, fragments)("--dry-run")
	if err == nil || !strings.Contains(out, `base.nope: the base fragment`) {
		t.Fatalf("lint = %v\n%s", err, out)
	}
}
