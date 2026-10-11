package lsp

import (
	"regexp"
	"strings"
)

// providerFromRe matches a line prefix ending inside the id string of an
// `extension <kind> <Name> from "<partial id>` header, capturing the kind and
// the partial id typed so far.
var providerFromRe = regexp.MustCompile(`\bextension\s+(\w+)\s+\w+\s+from\s+"([^"]*)$`)

// providerCompletionAt offers, inside `from "..."`, the id of every locked
// external extension that serves the header's kind (projectProviders). Each
// item's textEdit replaces the whole partial id, since editors split words at
// the dots an id contains. ok is false when the cursor isn't in that
// position; ok with no items means it is, but no locked extension fits.
func providerCompletionAt(path, linePrefix string, pos position) ([]completionItem, bool) {
	m := providerFromRe.FindStringSubmatch(linePrefix)
	if m == nil {
		return nil, false
	}
	kind, partial := m[1], m[2]
	edit := rangeT{
		Start: position{Line: pos.Line, Character: pos.Character - len(partial)},
		End:   pos,
	}
	var items []completionItem
	for _, p := range projectProviders(path, kind) {
		detail := "extension " + kind
		if p.Version != "" {
			detail += " · " + p.Version
		}
		items = append(items, completionItem{
			Label:    p.ID,
			Kind:     kindModule,
			Detail:   detail,
			TextEdit: &textEdit{Range: edit, NewText: p.ID},
		})
	}
	return items, true
}

// justOpenedString reports whether linePrefix ends with a `"` that opens a
// string literal (an odd number of unescaped quotes on the line so far) — the
// `"` trigger character firing somewhere other than `from "`, where no
// completion applies.
func justOpenedString(linePrefix string) bool {
	if !strings.HasSuffix(linePrefix, `"`) {
		return false
	}
	quotes := strings.Count(linePrefix, `"`) - strings.Count(linePrefix, `\"`)
	return quotes%2 == 1
}
