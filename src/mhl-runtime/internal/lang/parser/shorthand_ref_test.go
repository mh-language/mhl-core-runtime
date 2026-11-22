package parser_test

import (
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/engine/runtime"
	"github.com/mh-language/mhl-core-runtime/internal/lang/parser"
)

// `{base.x}` / `{self.x}` is `{x: base.x}` / `{x: self.x}` — the key is the
// member's name — and hashes like the long form.
func TestShorthandFieldThroughBaseOrSelf(t *testing.T) {
	short := "partial workflow W {\n    var a = 1\n    entry step S { log({base.a, self.a, b: 2}) }\n}\n"
	long := "partial workflow W {\n    var a = 1\n    entry step S { log({a: base.a, a: self.a, b: 2}) }\n}\n"
	ps, err := parser.Parse(short)
	if err != nil {
		t.Fatal(err)
	}
	pl, err := parser.Parse(long)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.DefinitionDigest(ps, "W") != runtime.DefinitionDigest(pl, "W") {
		t.Fatal("{base.a} must hash like {a: base.a}")
	}
}
