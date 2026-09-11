package cli_test

import (
	"strings"
	"testing"
)

// TestHTMLGetElementMissIsALanguageNull is a regression test for a
// typed-nil-in-interface bug: nativeops.GetElement's Go return type is the
// concrete map[string]any, so a naive `return nativeops.GetElement(...),
// nil` from nativeOpCall wrapped a "no match" result in a non-nil `any`
// (type map[string]any, value nil) — is_null()/not_null() compare against
// the untyped nil literal (test.go's `args[0] == nil`) and both failed
// against that value, even though it still *formatted* as "null". A miss
// must be indistinguishable from any other language-level null.
func TestHTMLGetElementMissIsALanguageNull(t *testing.T) {
	out, err := run(t, wrapStep(`
        var doc = html.parse("<div>x</div>")
        log(html.get_element(doc, tag: "table") == null)
        log(html.get_element(doc, tag: "div") != null)
        log(html.get_element_by_id(doc, "nope") == null)
    `))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "true\ntrue\ntrue\n") {
		t.Errorf("expected all three comparisons to log true, got: %s", out)
	}
}

func TestHTMLParseGetElementGetAttributeGetTextToHTML(t *testing.T) {
	// The <div>'s attrs deliberately hold just one attribute: html.to_html()
	// stores attrs as an object, not an ordered list, so with more than one
	// attribute the serialized order isn't guaranteed (see the ToHTML doc
	// comment/spec's own note on this).
	out, err := run(t, wrapStep(`
        var doc = html.parse("<div class=\"card featured\"><b>Hi</b> there</div>")
        var card = html.get_element(doc, attrs: { "class": "card" })
        log(html.get_attribute(card, "class"))
        log(html.get_attribute(card, "missing", "fallback"))
        log(html.get_text(card))
        log(html.to_html(card))
    `))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, want := range []string{"card featured\n", "fallback\n", "Hi there\n", "<div class=\"card featured\"><b>Hi</b> there</div>\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected output to contain %q, got: %s", want, out)
		}
	}
}

