package nativeops

import (
	"strings"
	"time"
)

// friendlyLayoutTokens maps human-readable date/time tokens (the
// moment.js/ICU convention: YYYY/MM/DD/HH/mm/ss) to Go's reference-time
// layout equivalents, so a caller can write time.format(v, "dd/MM/yyyy")
// instead of learning Go's "Mon Jan 2 15:04:05 MST 2006" mnemonic. Ordered
// longest-pattern-first so the greedy scan in friendlyLayoutToGo matches
// "yyyy" before "yy", etc.
//
// Case carries meaning where two units would otherwise collide: M/MM is
// always month, m/mm is always minute; H/HH is always 24-hour, h/hh is
// always 12-hour. Day and year have no such collision, so both cases mean
// the same thing. This is deliberately the same split moment.js/day.js use,
// since it's the convention most authors coming from another language
// already know.
//
// A layout built entirely of digits/punctuation (e.g. mhl's own default,
// Go's "2006-01-02") contains none of these letters and passes through
// unchanged. A raw Go layout that spells out a month/weekday *name*
// (e.g. "Jan 2, 2006", "Monday") is NOT supported here — its letters
// collide with these tokens (e.g. the "a" in "Jan" would be read as the
// am/pm token) — pass such a layout through fs/log formatting yourself
// instead of via time.format's layout argument.
var friendlyLayoutTokens = []struct {
	pattern  string
	goLayout string
}{
	{"yyyy", "2006"},
	{"YYYY", "2006"},
	{"yy", "06"},
	{"YY", "06"},
	{"MM", "01"},
	{"dd", "02"},
	{"DD", "02"},
	{"HH", "15"},
	{"hh", "03"},
	{"mm", "04"},
	{"ss", "05"},
	{"M", "1"},
	{"d", "2"},
	{"D", "2"},
	{"h", "3"},
	{"m", "4"},
	{"s", "5"},
	{"A", "PM"},
	{"a", "pm"},
}

// friendlyLayoutToGo translates layout's friendly tokens (see
// friendlyLayoutTokens) into a Go reference-time layout, passing through
// any character that isn't part of a recognized token (separators like
// "/", "-", ":", " ", "T") unchanged. An empty layout means time.RFC3339.
func friendlyLayoutToGo(layout string) string {
	if layout == "" {
		return time.RFC3339
	}
	var b strings.Builder
	for i := 0; i < len(layout); {
		matched := false
		for _, tok := range friendlyLayoutTokens {
			if strings.HasPrefix(layout[i:], tok.pattern) {
				b.WriteString(tok.goLayout)
				i += len(tok.pattern)
				matched = true
				break
			}
		}
		if !matched {
			b.WriteByte(layout[i])
			i++
		}
	}
	return b.String()
}
