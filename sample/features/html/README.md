# html

The `html` native namespace: parsing and querying HTML with
[`golang.org/x/net/html`](https://pkg.go.dev/golang.org/x/net/html) under the hood. There is
no dedicated "element" value type — `html.parse()` returns the same plain object/array shapes
`json.parse()` already does (`map[string]any`/`[]any`/`string`), so every existing value method
(`.get()`, `.find()`, `.filter()`, ...) already works on the result.

Every node has the same four keys regardless of kind:

- an element: `tag` is the (lower-cased) tag name, `attrs` is `{name: value, ...}`, `text` is
  `""`, `children` is an array of nodes.
- a text node: `tag` is `"#text"`, `attrs` is `{}`, `text` is its content, `children` is `[]`.

`html.parse()`'s root is always a synthetic `{tag: "#fragment", ...}` node, even when the input
parses to exactly one top-level element — a single, predictable return shape regardless of
input. Comments never appear as a node, and neither does a text node that's only
indentation/newlines between tags.

- [html_parse_builds_a_node_tree.mh](html_parse_builds_a_node_tree.mh) — the
  `{tag, attrs, text, children}` shape every node carries
- [html_parse_is_fragment_mode_no_implicit_wrapping.mh](html_parse_is_fragment_mode_no_implicit_wrapping.mh)
  — parsing is fragment-mode (as if inserted into a `<body>`): no implicit
  `<html>`/`<head>`/`<body>` wraps a loose snippet
- [html_parse_drops_comments_and_whitespace_text.mh](html_parse_drops_comments_and_whitespace_text.mh)
  — comments and whitespace-only text nodes never appear in `children`
- [html_get_element_finds_first_match_by_tag.mh](html_get_element_finds_first_match_by_tag.mh)
  — `html.get_element(node, tag?, attrs?)`, depth-first, first match or `null`
- [html_get_element_matches_class_by_token.mh](html_get_element_matches_class_by_token.mh) —
  `attrs: {class: "..."}` matches by space-separated token (like the DOM's `classList`), every
  other attribute by exact value; `html.get_elements(...)` returns every match
- [html_get_element_by_id.mh](html_get_element_by_id.mh) — `html.get_element_by_id(node, id)`,
  shorthand for `get_element(node, attrs: {id: id})`
- [html_get_attribute_never_raises.mh](html_get_attribute_never_raises.mh) —
  `html.get_attribute(element, name, default?)` never raises, even when `element` is `null`
  (a `get_element()` miss passed straight through)
- [html_get_text_concatenates_descendant_text.mh](html_get_text_concatenates_descendant_text.mh)
  — `html.get_text(node)`, the `textContent` equivalent
- [html_to_html_serializes_a_node_back_to_a_string.mh](html_to_html_serializes_a_node_back_to_a_string.mh)
  — `html.to_html(node)`, the inverse of `html.parse()`
