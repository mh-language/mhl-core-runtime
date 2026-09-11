package nativeops_test

import (
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/features/nativeops"
)

func TestParseHTMLBuildsAPlainNodeTree(t *testing.T) {
	doc, err := nativeops.ParseHTML(`<div id="card" class="box">Hello</div>`)
	if err != nil {
		t.Fatalf("ParseHTML: %v", err)
	}
	if doc["tag"] != "#fragment" {
		t.Fatalf("root tag = %v, want #fragment", doc["tag"])
	}
	children := doc["children"].([]any)
	if len(children) != 1 {
		t.Fatalf("root has %d children, want 1", len(children))
	}
	div := children[0].(map[string]any)
	if div["tag"] != "div" {
		t.Fatalf("div tag = %v, want div", div["tag"])
	}
	attrs := div["attrs"].(map[string]any)
	if attrs["id"] != "card" || attrs["class"] != "box" {
		t.Fatalf("div attrs = %#v, want id=card class=box", attrs)
	}
	divChildren := div["children"].([]any)
	if len(divChildren) != 1 {
		t.Fatalf("div has %d children, want 1", len(divChildren))
	}
	text := divChildren[0].(map[string]any)
	if text["tag"] != "#text" || text["text"] != "Hello" {
		t.Fatalf("text node = %#v, want #text \"Hello\"", text)
	}
}

func TestParseHTMLIsFragmentModeNotFullDocument(t *testing.T) {
	doc, err := nativeops.ParseHTML(`<p>one</p><p>two</p>`)
	if err != nil {
		t.Fatalf("ParseHTML: %v", err)
	}
	children := doc["children"].([]any)
	if len(children) != 2 {
		t.Fatalf("root has %d children, want 2 (no implicit html/head/body wrapper)", len(children))
	}
	if nativeops.GetElement(doc, nativeops.HTMLMatch{Tag: "html"}) != nil {
		t.Fatal("found an <html> node — fragment parsing must not wrap the input")
	}
	if nativeops.GetElement(doc, nativeops.HTMLMatch{Tag: "body"}) != nil {
		t.Fatal("found a <body> node — fragment parsing must not wrap the input")
	}
}

func TestParseHTMLDropsCommentsAndWhitespaceOnlyText(t *testing.T) {
	doc, err := nativeops.ParseHTML("<ul>\n  <!-- note -->\n  <li>one</li>\n</ul>")
	if err != nil {
		t.Fatalf("ParseHTML: %v", err)
	}
	ul := nativeops.GetElement(doc, nativeops.HTMLMatch{Tag: "ul"})
	if ul == nil {
		t.Fatal("expected to find <ul>")
	}
	children := ul["children"].([]any)
	if len(children) != 1 {
		t.Fatalf("<ul> has %d children, want exactly 1 (<li>, no comment or whitespace text)", len(children))
	}
	if children[0].(map[string]any)["tag"] != "li" {
		t.Fatalf("only child = %#v, want <li>", children[0])
	}
}

func TestGetElementFindsFirstMatchDepthFirst(t *testing.T) {
	doc, _ := nativeops.ParseHTML(`<div><span>a</span><span>b</span></div>`)
	span := nativeops.GetElement(doc, nativeops.HTMLMatch{Tag: "span"})
	if span == nil {
		t.Fatal("expected a match")
	}
	if got := nativeops.GetText(span); got != "a" {
		t.Fatalf("GetText = %q, want %q (first match)", got, "a")
	}
	if nativeops.GetElement(doc, nativeops.HTMLMatch{Tag: "table"}) != nil {
		t.Fatal("expected no match for an absent tag")
	}
}

func TestGetElementClassMatchesByToken(t *testing.T) {
	doc, _ := nativeops.ParseHTML(`<div class="card featured">x</div><div class="card">y</div>`)

	featured := nativeops.GetElement(doc, nativeops.HTMLMatch{Attrs: map[string]string{"class": "featured"}})
	if featured == nil || nativeops.GetText(featured) != "x" {
		t.Fatalf("expected the token-matched element with text %q, got %#v", "x", featured)
	}

	all := nativeops.GetElements(doc, nativeops.HTMLMatch{Attrs: map[string]string{"class": "card"}})
	if len(all) != 2 {
		t.Fatalf("GetElements matched %d elements, want 2 (exact-string equality would have missed \"card featured\")", len(all))
	}
}

func TestGetElementNonClassAttrsMatchExactly(t *testing.T) {
	doc, _ := nativeops.ParseHTML(`<a href="/a">x</a><a href="/a/b">y</a>`)
	el := nativeops.GetElement(doc, nativeops.HTMLMatch{Attrs: map[string]string{"href": "/a"}})
	if el == nil || nativeops.GetText(el) != "x" {
		t.Fatalf("expected exact href match, got %#v", el)
	}
}

func TestGetAttributeNeverRaisesOnAbsenceOrNilElement(t *testing.T) {
	doc, _ := nativeops.ParseHTML(`<a href="/x">link</a>`)
	link := nativeops.GetElement(doc, nativeops.HTMLMatch{Tag: "a"})

	if got := nativeops.GetAttribute(link, "href", nil); got != "/x" {
		t.Fatalf("GetAttribute(href) = %v, want /x", got)
	}
	if got := nativeops.GetAttribute(link, "target", "_self"); got != "_self" {
		t.Fatalf("GetAttribute(target, default) = %v, want _self (attribute absent)", got)
	}
	if got := nativeops.GetAttribute(nil, "href", "fallback"); got != "fallback" {
		t.Fatalf("GetAttribute(nil element, default) = %v, want fallback", got)
	}
}

func TestGetTextConcatenatesDescendantsWithoutSeparator(t *testing.T) {
	doc, _ := nativeops.ParseHTML(`<p>Hello <b>bold</b> world</p>`)
	p := nativeops.GetElement(doc, nativeops.HTMLMatch{Tag: "p"})
	if got := nativeops.GetText(p); got != "Hello bold world" {
		t.Fatalf("GetText = %q, want %q", got, "Hello bold world")
	}
}

func TestToHTMLRoundTripsAnElement(t *testing.T) {
	doc, _ := nativeops.ParseHTML(`<div>Hello</div>`)
	div := nativeops.GetElement(doc, nativeops.HTMLMatch{Tag: "div"})
	got, err := nativeops.ToHTML(div)
	if err != nil {
		t.Fatalf("ToHTML: %v", err)
	}
	if got != "<div>Hello</div>" {
		t.Fatalf("ToHTML = %q, want %q", got, "<div>Hello</div>")
	}
}

func TestToHTMLOnAFragmentRendersEveryChild(t *testing.T) {
	doc, _ := nativeops.ParseHTML(`<p>a</p><p>b</p>`)
	got, err := nativeops.ToHTML(doc)
	if err != nil {
		t.Fatalf("ToHTML: %v", err)
	}
	if got != "<p>a</p><p>b</p>" {
		t.Fatalf("ToHTML = %q, want %q", got, "<p>a</p><p>b</p>")
	}
}
