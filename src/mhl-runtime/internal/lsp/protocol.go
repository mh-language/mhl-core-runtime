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

type referenceParams struct {
	TextDocument versionedTextDocumentIdentifier `json:"textDocument"`
	Position     position                        `json:"position"`
	Context      referenceContext                `json:"context"`
}

type referenceContext struct {
	IncludeDeclaration bool `json:"includeDeclaration"`
}

type codeLensParams struct {
	TextDocument versionedTextDocumentIdentifier `json:"textDocument"`
}

type publishDiagnosticsParams struct {
	URI         string       `json:"uri"`
	Diagnostics []diagnostic `json:"diagnostics"`
}

// location is LSP's Location: a document URI plus the range within it to
// reveal. It is the payload of a textDocument/definition response (returned
// singly or as a list).
type location struct {
	URI   string `json:"uri"`
	Range rangeT `json:"range"`
}

// completionItemKind mirrors the subset of LSP's CompletionItemKind enum
// this server uses.
const (
	kindText     = 1
	kindMethod   = 2
	kindFunction = 3
	kindField    = 5
	kindVariable = 6
	kindClass    = 7
	kindModule   = 9
	kindProperty = 10
	kindKeyword  = 14
	kindSnippet  = 15
)

// insertTextFormatSnippet marks a completionItem's InsertText as LSP snippet
// syntax ($1, ${2:default}, $0 final cursor) rather than plain text — see
// declarationSnippets in completion.go. Omitted (zero value) means
// PlainText, the LSP default when the field is absent.
const insertTextFormatSnippet = 2

type completionItem struct {
	Label            string         `json:"label"`
	Kind             int            `json:"kind"`
	Detail           string         `json:"detail,omitempty"`
	Documentation    *markupContent `json:"documentation,omitempty"`
	InsertText       string         `json:"insertText,omitempty"`
	InsertTextFormat int            `json:"insertTextFormat,omitempty"`
	SortText         string         `json:"sortText,omitempty"`
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
	DefinitionProvider    bool                 `json:"definitionProvider"`
	ReferencesProvider    bool                 `json:"referencesProvider"`
	CodeLensProvider      codeLensOptions      `json:"codeLensProvider"`
	HoverProvider         bool                 `json:"hoverProvider"`
}

type codeLensOptions struct {
	ResolveProvider bool `json:"resolveProvider"`
}

type command struct {
	Title     string `json:"title"`
	Command   string `json:"command"`
	Arguments []any  `json:"arguments,omitempty"`
}

type codeLens struct {
	Range   rangeT  `json:"range"`
	Command command `json:"command"`
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
	Label         string         `json:"label"`
	Documentation *markupContent `json:"documentation,omitempty"`
}

// hover is LSP's Hover response: the explanation shown for whatever's under
// the cursor (hover.go). Omits the optional "range" field — the client
// highlights nothing extra, same minimalism as signatureHelp's own response.
type hover struct {
	Contents markupContent `json:"contents"`
}

type initializeResult struct {
	Capabilities serverCapabilities `json:"capabilities"`
	ServerInfo   serverInfo         `json:"serverInfo"`
}

// initializeParams is the slice of "initialize"'s request params this server
// reads: the client's project boundary, used to scope find-references/
// codeLens file scans instead of guessing one from ancestor directory
// markers (see references.go's referenceRoot). rootUri is preferred when
// present (deprecated by the spec but still what most clients send
// alongside workspaceFolders); workspaceFolders[0] is the fallback.
type initializeParams struct {
	RootURI          *string            `json:"rootUri"`
	WorkspaceFolders []workspaceFolder  `json:"workspaceFolders"`
	Capabilities     clientCapabilities `json:"capabilities"`
}

// clientCapabilities is the one leaf this server reads out of "initialize"'s
// (large) capabilities tree: whether the client can render a snippet
// InsertText (tabstops, placeholders) rather than insert it as literal text.
// Absent/false on an unfamiliar client falls back to plain keyword
// completion — see declarationSnippets in completion.go and its use in
// server.go's "textDocument/completion" handler.
type clientCapabilities struct {
	TextDocument struct {
		Completion struct {
			CompletionItem struct {
				SnippetSupport bool `json:"snippetSupport"`
			} `json:"completionItem"`
		} `json:"completion"`
	} `json:"textDocument"`
}

type workspaceFolder struct {
	URI string `json:"uri"`
}

type serverInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}
