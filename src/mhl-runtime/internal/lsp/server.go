package lsp

import (
	"encoding/json"
	"io"
	"net/url"
	"strings"
)

// server holds the open-document store and I/O for one LSP session. mhl lsp
// runs exactly one of these over stdio for the lifetime of the editor
// process (internal/cli.runLSP).
type server struct {
	rd   *reader
	wr   *writer
	docs map[string]string // URI -> current full text (didOpen/didChange keep this in sync)
}

// Serve runs the LSP message loop over in/out until the client sends
// "exit" (LSP's own termination sequence, distinct from EOF) or the stream
// closes. It returns nil on either clean ending; a non-nil error means the
// stream itself broke (e.g. malformed framing).
func Serve(in io.Reader, out io.Writer) error {
	s := &server{
		rd:   newReader(in),
		wr:   newWriter(out),
		docs: map[string]string{},
	}
	for {
		msg, err := s.rd.next()
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		if msg.Method == "exit" {
			return nil
		}
		s.handle(msg)
	}
}

func (s *server) handle(msg *rpcMessage) {
	switch msg.Method {
	case "initialize":
		s.wr.respond(msg.ID, initializeResult{
			Capabilities: serverCapabilities{
				TextDocumentSync: 1,
				CompletionProvider: completionOptions{
					TriggerCharacters: []string{"."},
				},
				SignatureHelpProvider: signatureHelpOptions{
					TriggerCharacters:   []string{"(", ","},
					RetriggerCharacters: []string{","},
				},
				DefinitionProvider: true,
			},
			ServerInfo: serverInfo{Name: "mhl-lsp", Version: "0.1.0"},
		})
	case "initialized":
		// no-op: nothing to do once the client confirms initialization
	case "shutdown":
		s.wr.respond(msg.ID, nil)
	case "textDocument/didOpen":
		var p didOpenParams
		if json.Unmarshal(msg.Params, &p) == nil {
			s.docs[p.TextDocument.URI] = p.TextDocument.Text
			s.publishDiagnostics(p.TextDocument.URI)
		}
	case "textDocument/didChange":
		var p didChangeParams
		if json.Unmarshal(msg.Params, &p) == nil && len(p.ContentChanges) > 0 {
			// Full-document sync only (see serverCapabilities.TextDocumentSync
			// = 1): the last change entry always carries the entire new text.
			s.docs[p.TextDocument.URI] = p.ContentChanges[len(p.ContentChanges)-1].Text
			s.publishDiagnostics(p.TextDocument.URI)
		}
	case "textDocument/didClose":
		var p didCloseParams
		if json.Unmarshal(msg.Params, &p) == nil {
			delete(s.docs, p.TextDocument.URI)
			s.wr.notify("textDocument/publishDiagnostics", publishDiagnosticsParams{URI: p.TextDocument.URI, Diagnostics: []diagnostic{}})
		}
	case "textDocument/completion":
		var p textDocumentPositionParams
		if json.Unmarshal(msg.Params, &p) != nil {
			s.wr.respond(msg.ID, []completionItem{})
			return
		}
		text := s.docs[p.TextDocument.URI]
		items := completionAt(uriToPath(p.TextDocument.URI), text, p.Position)
		s.wr.respond(msg.ID, items)
	case "textDocument/signatureHelp":
		var p textDocumentPositionParams
		if json.Unmarshal(msg.Params, &p) != nil {
			s.wr.respond(msg.ID, nil)
			return
		}
		text := s.docs[p.TextDocument.URI]
		s.wr.respond(msg.ID, signatureHelpAt(uriToPath(p.TextDocument.URI), text, p.Position))
	case "textDocument/definition":
		var p textDocumentPositionParams
		if json.Unmarshal(msg.Params, &p) != nil {
			s.wr.respond(msg.ID, nil)
			return
		}
		text := s.docs[p.TextDocument.URI]
		locs := definitionAt(uriToPath(p.TextDocument.URI), text, p.Position)
		if len(locs) == 0 {
			// LSP: null is the "no definition found" response.
			s.wr.respond(msg.ID, nil)
			return
		}
		s.wr.respond(msg.ID, locs)
	default:
		if msg.ID != nil {
			s.wr.respondError(msg.ID, -32601, "method not found: "+msg.Method)
		}
	}
}

func (s *server) publishDiagnostics(uri string) {
	text := s.docs[uri]
	diags := diagnosticsFor(uriToPath(uri), text)
	s.wr.notify("textDocument/publishDiagnostics", publishDiagnosticsParams{URI: uri, Diagnostics: diags})
}

// uriToPath converts a "file://..." document URI into a plain filesystem
// path. lint.Source only uses this path to resolve relative import
// targets and to label findings, so a best-effort decode (falling back to
// stripping the scheme verbatim on a malformed URI) is good enough — it
// never needs to be a real file handle.
//
// A Windows client sends drive-letter URIs like "file:///c%3A/Users/..."
// (VS Code, and the LSP spec's own examples, always do); url.Parse decodes
// that to a Path of "/c:/Users/..." — a leading slash ahead of the drive
// letter that isn't a valid Windows path, so every filepath.*/os.* call
// downstream (import/prompt-source resolution, go-to-definition) silently
// fails to find the file. Strip that leading slash when what follows looks
// like a drive letter; a genuine Unix path never matches "/<letter>:/...",
// so this is safe on every platform, not just under GOOS=windows.
func uriToPath(uri string) string {
	u, err := url.Parse(uri)
	if err != nil || u.Scheme != "file" {
		return strings.TrimPrefix(uri, "file://")
	}
	p := u.Path
	if len(p) >= 3 && p[0] == '/' && p[2] == ':' && isASCIILetter(p[1]) {
		p = p[1:]
	}
	return p
}

func isASCIILetter(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}
