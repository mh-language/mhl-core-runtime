package lsp

// This file holds the small slice of the LSP type system this server
// actually speaks — not a general-purpose protocol library. Field sets are
// trimmed to what we read or write.

type position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

type rangeT struct {
	Start position `json:"start"`
	End   position `json:"end"`
}

type diagnostic struct {
	Range    rangeT `json:"range"`
	Severity int    `json:"severity"` // 1=Error 2=Warning 3=Info 4=Hint
	Source   string `json:"source"`
	Message  string `json:"message"`
}

type textDocumentItem struct {
	URI  string `json:"uri"`
	Text string `json:"text"`
}

type versionedTextDocumentIdentifier struct {
	URI string `json:"uri"`
}

type didOpenParams struct {
	TextDocument textDocumentItem `json:"textDocument"`
}

type contentChange struct {
	Text string `json:"text"`
}

type didChangeParams struct {
	TextDocument   versionedTextDocumentIdentifier `json:"textDocument"`
	ContentChanges []contentChange                 `json:"contentChanges"`
}

type didCloseParams struct {
	TextDocument versionedTextDocumentIdentifier `json:"textDocument"`
}

type textDocumentPositionParams struct {
	TextDocument versionedTextDocumentIdentifier `json:"textDocument"`
	Position     position                        `json:"position"`
}

type publishDiagnosticsParams struct {
	URI         string       `json:"uri"`
	Diagnostics []diagnostic `json:"diagnostics"`
}

// completionItemKind mirrors the subset of LSP's CompletionItemKind enum
// this server uses.
const (
	kindText     = 1
	kindMethod   = 2
	kindFunction = 3
	kindClass    = 7
	kindModule   = 9
	kindProperty = 10
	kindKeyword  = 14
)

type completionItem struct {
	Label         string         `json:"label"`
	Kind          int            `json:"kind"`
	Detail        string         `json:"detail,omitempty"`
	Documentation *markupContent `json:"documentation,omitempty"`
	InsertText    string         `json:"insertText,omitempty"`
	SortText      string         `json:"sortText,omitempty"`
}

// markupContent is LSP's MarkupContent: a string plus how to render it
// ("markdown" or "plaintext"). Used for a completion item's hover doc and a
// signature's explanation text.
type markupContent struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

type serverCapabilities struct {
	TextDocumentSync      int                  `json:"textDocumentSync"` // 1=Full
	CompletionProvider    completionOptions    `json:"completionProvider"`
	SignatureHelpProvider signatureHelpOptions `json:"signatureHelpProvider"`
}

type completionOptions struct {
	TriggerCharacters []string `json:"triggerCharacters"`
}

type signatureHelpOptions struct {
	TriggerCharacters   []string `json:"triggerCharacters"`
	RetriggerCharacters []string `json:"retriggerCharacters"`
}

// signatureHelp and its nested types are LSP's SignatureHelp response: the
// list of overloads that could apply at the cursor, which one is active, and
// which parameter within it the cursor currently sits on.
type signatureHelp struct {
	Signatures      []signatureInformation `json:"signatures"`
	ActiveSignature int                    `json:"activeSignature"`
	ActiveParameter int                    `json:"activeParameter"`
}

type signatureInformation struct {
	Label         string                 `json:"label"`
	Documentation *markupContent         `json:"documentation,omitempty"`
	Parameters    []parameterInformation `json:"parameters"`
}

type parameterInformation struct {
	Label string `json:"label"`
}

type initializeResult struct {
	Capabilities serverCapabilities `json:"capabilities"`
	ServerInfo   serverInfo         `json:"serverInfo"`
}

type serverInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}
