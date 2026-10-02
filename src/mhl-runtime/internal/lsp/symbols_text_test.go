package lsp

import (
	"reflect"
	"testing"
)

// toolMethodsFromText finds a method whether its block body follows `->` or
// sits directly after the signature, and never mistakes a control statement
// inside a body (`if (x) {`, `while (x) {`) for a method.
func TestToolMethodsFromTextArrowOptionalBeforeBlock(t *testing.T) {
	src := `
tool T {
    read(p: string): string -> fs.read(p)
    count(items) {
        var n = 0
        while (n < items.size()) {
            n = n + 1
        }
        if (n > 0) {
            return n
        }
        return 0
    }
    internal helper(x): number {
        return x
    }
    old(x) -> {
        return x
    }
}
`
	methods, internal := toolMethodsFromText(src, "T")
	if want := []string{"read", "count", "helper", "old"}; !reflect.DeepEqual(methods, want) {
		t.Fatalf("methods = %v, want %v", methods, want)
	}
	if !internal["helper"] || len(internal) != 1 {
		t.Fatalf("internal = %v, want only helper", internal)
	}
}
