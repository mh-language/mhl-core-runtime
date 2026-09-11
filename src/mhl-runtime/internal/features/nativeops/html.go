package nativeops

import (
	"fmt"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// ParseHTML parses text as an HTML *fragment* — as if it had been inserted
// into a <body>, not a standalone document — so a plain snippet like
// "<div>x</div>" round-trips as itself instead of being wrapped in an
// implicit <html>/<head>/<body> the way the raw HTML5 parsing algorithm
// would. The result is MHL's own value kinds only (map[string]any/
// []any/string), the same "parsed structure as plain values" contract
// Parse (json.go) already follows — there is no separate "element handle"
// type, an element is just an object a caller can `.get("attrs")` etc. on
// like any other.
//
// Every returned node has the same four keys — "tag", "attrs", "text",
// "children" — regardless of kind, so field access never has to branch on
// what kind of node it might be:
//   - an element: tag is the (lower-cased) tag name, attrs is
//     {name: value, ...}, text is "".
//   - a text node: tag is "#text", attrs is {}, text is its content.
//
// The root is always a single synthetic {tag: "#fragment", ...} node, even
// when text parses to exactly one top-level element — a consistent return
// shape regardless of input beats unwrapping single-child input specially.
// Comments are dropped entirely (never appear as a node), and so are
// whitespace-only text nodes (pure indentation/newlines between tags) —
// both would just be noise for the read/query use case this exists for.
// x/net/html's HTML5 algorithm recovers from malformed markup rather than
// rejecting it, so parse errors in practice never happen from a string
// input; the error return exists for the same reason json.Parse's does.
func ParseHTML(text string) (map[string]any, error) {
	context := &html.Node{Type: html.ElementNode, Data: "body", DataAtom: atom.Body}
	nodes, err := html.ParseFragment(strings.NewReader(text), context)
	if err != nil {
		return nil, fmt.Errorf("html.parse: %w", err)
	}
	children := make([]any, 0, len(nodes))
	for _, n := range nodes {
		if child, ok := convertHTMLNode(n); ok {
			children = append(children, child)
		}
	}
	return map[string]any{
		"tag":      "#fragment",
		"attrs":    map[string]any{},
		"text":     "",
		"children": children,
	}, nil
}

// convertHTMLNode converts one *html.Node — and, for an element, its whole
// subtree — into the plain {tag, attrs, text, children} shape ParseHTML
// documents. ok is false for a node kind that is dropped outright (a
// comment; a whitespace-only text node) rather than represented as some
// placeholder value.
func convertHTMLNode(n *html.Node) (map[string]any, bool) {
	switch n.Type {
	case html.ElementNode:
		attrs := make(map[string]any, len(n.Attr))
		for _, a := range n.Attr {
			attrs[a.Key] = a.Val
		}
		children := make([]any, 0)
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if child, ok := convertHTMLNode(c); ok {
				children = append(children, child)
			}
		}
		return map[string]any{
			"tag":      n.Data,
			"attrs":    attrs,
			"text":     "",
			"children": children,
		}, true
	case html.TextNode:
		if strings.TrimSpace(n.Data) == "" {
			return nil, false
		}
		return map[string]any{
			"tag":      "#text",
			"attrs":    map[string]any{},
			"text":     n.Data,
			"children": []any{},
		}, true
	default:
		// Comments, the doctype, and any other non-content node kind are
		// dropped — never represented in the returned tree.
		return nil, false
	}
}

// HTMLMatch is the (tag, attrs) filter GetElement/GetElements search a
// node's subtree for. A zero Tag matches any tag; a nil/empty Attrs
// matches regardless of attributes.
type HTMLMatch struct {
	Tag   string
	Attrs map[string]string
}

// GetElement returns the first node in node's subtree — node itself
// included — that matches m, visited depth-first/pre-order (a node before
// its children, children in document order). nil means no match.
func GetElement(node map[string]any, m HTMLMatch) map[string]any {
	if htmlNodeMatches(node, m) {
		return node
	}
	for _, child := range htmlChildren(node) {
		if found := GetElement(child, m); found != nil {
			return found
		}
	}
	return nil
}

// GetElements returns every node in node's subtree — node itself included
// — that matches m, in the same depth-first/pre-order GetElement searches.
func GetElements(node map[string]any, m HTMLMatch) []any {
	out := make([]any, 0)
	if htmlNodeMatches(node, m) {
		out = append(out, node)
	}
	for _, child := range htmlChildren(node) {
		out = append(out, GetElements(child, m)...)
	}
	return out
}

// GetAttribute reads name out of element's attrs, returning def when
// element isn't a node at all (e.g. a GetElement miss's nil result was
// passed straight through), it has no attrs, or the attribute is absent —
// mirroring the object.get() value method's own never-raise-on-a-missing-
// key contract instead of adding a new failure mode of its own.
func GetAttribute(element map[string]any, name string, def any) any {
	attrs, ok := element["attrs"].(map[string]any)
	if !ok {
		return def
	}
	if v, present := attrs[name]; present {
		return v
	}
	return def
}

// GetText concatenates the text content of every #text descendant of node
// (node itself included, if it is one) in document order — the DOM
// textContent equivalent. There is no separator inserted between sibling
// text runs, matching textContent's own behavior.
func GetText(node map[string]any) string {
	var b strings.Builder
	collectHTMLText(node, &b)
	return b.String()
}

func collectHTMLText(node map[string]any, b *strings.Builder) {
	if tag, _ := node["tag"].(string); tag == "#text" {
		if text, ok := node["text"].(string); ok {
			b.WriteString(text)
		}
		return
	}
	for _, child := range htmlChildren(node) {
		collectHTMLText(child, b)
	}
}

// ToHTML serializes node back to an HTML string — the inverse of
// ParseHTML. A "#fragment" root serializes each of its children in order
// (a fragment itself is never one real tag); any other node serializes as
// itself, subtree included. Attribute order is not preserved across a
// parse→ToHTML round trip, since ParseHTML stores attrs as a plain object
// keyed by name rather than an ordered list.
func ToHTML(node map[string]any) (string, error) {
	var b strings.Builder
	tag, _ := node["tag"].(string)
	if tag == "#fragment" {
		for _, child := range htmlChildren(node) {
			if err := renderHTMLNode(child, &b); err != nil {
				return "", err
			}
		}
		return b.String(), nil
	}
	if err := renderHTMLNode(node, &b); err != nil {
		return "", err
	}
	return b.String(), nil
}

func renderHTMLNode(node map[string]any, b *strings.Builder) error {
	n, err := buildHTMLNode(node)
	if err != nil {
		return err
	}
	if err := html.Render(b, n); err != nil {
		return fmt.Errorf("html.to_html: %w", err)
	}
	return nil
}

// buildHTMLNode is ToHTML's inverse of convertHTMLNode: it rebuilds a real
// *html.Node (and, for an element, its subtree) from the plain
// {tag, attrs, text, children} shape, so html.Render can serialize it.
func buildHTMLNode(node map[string]any) (*html.Node, error) {
	tag, _ := node["tag"].(string)
	switch tag {
	case "":
		return nil, fmt.Errorf("html.to_html: node has no tag")
	case "#fragment":
		return nil, fmt.Errorf("html.to_html: a #fragment node has no single tag to render — pass it to html.to_html() directly rather than nesting it")
	case "#text":
		text, _ := node["text"].(string)
		return &html.Node{Type: html.TextNode, Data: text}, nil
	}
	n := &html.Node{Type: html.ElementNode, Data: tag, DataAtom: atom.Lookup([]byte(tag))}
	if attrs, ok := node["attrs"].(map[string]any); ok {
		for k, v := range attrs {
			val, _ := v.(string)
			n.Attr = append(n.Attr, html.Attribute{Key: k, Val: val})
		}
	}
	for _, child := range htmlChildren(node) {
		cn, err := buildHTMLNode(child)
		if err != nil {
			return nil, err
		}
		n.AppendChild(cn)
	}
	return n, nil
}

// htmlChildren reads node's "children" as a []map[string]any, skipping any
// entry that isn't one — never the case for a tree ParseHTML itself
// produced, but node may just as well be a plain object a .mh program
// built or edited by hand.
func htmlChildren(node map[string]any) []map[string]any {
	raw, _ := node["children"].([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, c := range raw {
		if child, ok := c.(map[string]any); ok {
			out = append(out, child)
		}
	}
	return out
}

// htmlNodeMatches reports whether node satisfies m: its tag (when m.Tag is
// non-empty) and every one of m.Attrs. Every attribute other than "class"
// is matched by exact string equality; "class" is matched by token — every
// space-separated word in m.Attrs["class"] must appear among node's own
// space-separated class tokens, in any order, alongside others — mirroring
// how HTML/CSS itself treats `class` as a set, not an opaque string, so
// `attrs: {class: "card"}` finds `class="card featured"` the way a reader
// would expect.
func htmlNodeMatches(node map[string]any, m HTMLMatch) bool {
	if m.Tag != "" {
		tag, _ := node["tag"].(string)
		if tag != m.Tag {
			return false
		}
	}
	if len(m.Attrs) == 0 {
		return true
	}
	attrs, _ := node["attrs"].(map[string]any)
	for name, want := range m.Attrs {
		got, ok := attrs[name].(string)
		if !ok {
			return false
		}
		if name == "class" {
			if !hasAllClassTokens(got, want) {
				return false
			}
			continue
		}
		if got != want {
			return false
		}
	}
	return true
}

// hasAllClassTokens reports whether every space-separated token in want
// appears among actual's own space-separated tokens.
func hasAllClassTokens(actual, want string) bool {
	actualTokens := strings.Fields(actual)
	for _, w := range strings.Fields(want) {
		found := false
		for _, a := range actualTokens {
			if a == w {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
