package parser

import "testing"

func TestParseExtensionProvider(t *testing.T) {
	tests := []struct {
		src          string
		wantKind     string
		wantName     string
		wantProvider string
	}{
		{`extension data Cache { url: "x" }`, "data", "Cache", ""},
		{`extension data Cache from "mhl.data.redis" { url: "x" }`, "data", "Cache", "mhl.data.redis"},
		{`extension state S from "mhl.state.sqlite" {}`, "state", "S", "mhl.state.sqlite"},
	}
	for _, tt := range tests {
		prog, err := Parse(tt.src)
		if err != nil {
			t.Fatalf("%s: parse: %v", tt.src, err)
		}
		if len(prog.Decls) != 1 || prog.Decls[0].Extension == nil {
			t.Fatalf("%s: expected one extension declaration", tt.src)
		}
		ext := prog.Decls[0].Extension
		if ext.Kind != tt.wantKind || ext.Name != tt.wantName || ext.Provider != tt.wantProvider {
			t.Fatalf("%s: got (%q, %q, %q), want (%q, %q, %q)", tt.src,
				ext.Kind, ext.Name, ext.Provider, tt.wantKind, tt.wantName, tt.wantProvider)
		}
	}
}
