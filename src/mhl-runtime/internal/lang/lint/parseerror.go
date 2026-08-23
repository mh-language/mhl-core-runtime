package lint

import (
	"errors"

	"github.com/alecthomas/participle/v2"
)

// findingFromParseError converts a parser.Parse error into a Finding. When
// the error carries participle position information (the common case), the
// finding points at the exact line/column; otherwise it falls back to line 0
// with the raw error text.
func findingFromParseError(file string, err error) Finding {
	var perr participle.Error
	if errors.As(err, &perr) {
		pos := perr.Position()
		return Finding{File: file, Line: pos.Line, Column: pos.Column, Message: perr.Message()}
	}
	return Finding{File: file, Message: err.Error()}
}
