package nativeops

import (
	"bytes"
	"fmt"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/htmlindex"
	"golang.org/x/text/encoding/ianaindex"
	"golang.org/x/text/encoding/unicode"
)

// .mh strings are always UTF-8 inside the runtime. The `encoding` argument
// of fs.read/fs.write/fs.append/cmd.exec names the charset of the bytes on
// the *outside* — on disk or on a subprocess's stdout/stderr — so legacy
// files (a Windows-1252 export, a UTF-16 file PowerShell 5's `>` wrote) are
// transcoded at the boundary instead of reaching an agent's stdin as invalid
// UTF-8 that a strict CLI (e.g. Devin) refuses.
//
// Accepted names, case-insensitive:
//   - "utf-8"/"utf8": strict — a leading BOM is dropped, invalid UTF-8 raises.
//   - "utf-8-bom"/"utf-8-sig": as "utf-8" on read; writes a leading BOM.
//   - "utf-16": BOM-driven, little-endian when there is none; writes LE+BOM.
//   - "utf-16le"/"utf-16be": fixed byte order; a leading BOM is dropped on read.
//   - "auto" (read only): BOM sniff (UTF-8/UTF-16LE/UTF-16BE), else UTF-8 when
//     the bytes are valid UTF-8, else Windows-1252.
//   - any WHATWG label ("latin1", "iso-8859-1", "windows-1252", "shift_jis",
//     "gbk", "koi8-r", ...) or IANA name ("ibm850", ...).
//
// No encoding (the empty string) keeps the historical behavior: the bytes
// pass through unchanged, valid UTF-8 or not.

const utf8BOM = "\xEF\xBB\xBF"

// Decode converts raw bytes in the named encoding to a UTF-8 string.
func Decode(raw []byte, name string) (string, error) {
	key := normalizeEncoding(name)
	switch key {
	case "":
		return string(raw), nil
	case "utf-8", "utf-8-bom":
		raw = bytes.TrimPrefix(raw, []byte(utf8BOM))
		if !utf8.Valid(raw) {
			return "", fmt.Errorf("content is not valid UTF-8 (pass the file's real encoding, e.g. encoding: \"windows-1252\", or \"auto\")")
		}
		return string(raw), nil
	case "auto":
		return decodeAuto(raw)
	}
	enc, err := lookupEncoding(key)
	if err != nil {
		return "", err
	}
	out, err := enc.NewDecoder().Bytes(raw)
	if err != nil {
		return "", fmt.Errorf("decoding as %s: %w", name, err)
	}
	return strings.TrimPrefix(string(out), utf8BOM), nil
}

// Encode converts a UTF-8 string to bytes in the named encoding. bom asks
// for the encoding's byte-order mark when it has one by default
// ("utf-8-bom", "utf-16"); Append passes false when the file already has
// content, so a mark is never written mid-file. A character the target
// charset cannot represent raises rather than being silently replaced.
func Encode(s, name string, bom bool) ([]byte, error) {
	key := normalizeEncoding(name)
	switch key {
	case "", "utf-8":
		return []byte(s), nil
	case "utf-8-bom":
		if bom {
			return []byte(utf8BOM + s), nil
		}
		return []byte(s), nil
	case "auto":
		return nil, fmt.Errorf(`encoding "auto" is only valid when reading`)
	case "utf-16":
		if bom {
			return unicode.UTF16(unicode.LittleEndian, unicode.UseBOM).NewEncoder().Bytes([]byte(s))
		}
		key = "utf-16le"
	}
	enc, err := lookupEncoding(key)
	if err != nil {
		return nil, err
	}
	out, err := enc.NewEncoder().Bytes([]byte(s))
	if err != nil {
		return nil, fmt.Errorf("encoding as %s: %w", name, err)
	}
	return out, nil
}

// ValidateEncoding reports whether name is an encoding Decode/Encode accept,
// so a caller can reject a typo before touching the filesystem.
func ValidateEncoding(name string) error {
	switch key := normalizeEncoding(name); key {
	case "", "utf-8", "utf-8-bom", "utf-16", "auto":
		return nil
	default:
		_, err := lookupEncoding(key)
		return err
	}
}

func decodeAuto(raw []byte) (string, error) {
	switch {
	case bytes.HasPrefix(raw, []byte(utf8BOM)):
		return Decode(raw, "utf-8")
	case bytes.HasPrefix(raw, []byte{0xFF, 0xFE}):
		return Decode(raw, "utf-16le")
	case bytes.HasPrefix(raw, []byte{0xFE, 0xFF}):
		return Decode(raw, "utf-16be")
	case utf8.Valid(raw):
		return string(raw), nil
	default:
		return Decode(raw, "windows-1252")
	}
}

func normalizeEncoding(name string) string {
	key := strings.ToLower(strings.TrimSpace(name))
	switch key {
	case "utf8":
		return "utf-8"
	case "utf-8-sig", "utf8-bom", "utf8bom", "utf-8bom":
		return "utf-8-bom"
	case "utf16":
		return "utf-16"
	}
	return key
}

func lookupEncoding(key string) (encoding.Encoding, error) {
	switch key {
	case "utf-16":
		return unicode.UTF16(unicode.LittleEndian, unicode.UseBOM), nil
	case "utf-16le", "utf16le":
		return unicode.UTF16(unicode.LittleEndian, unicode.IgnoreBOM), nil
	case "utf-16be", "utf16be":
		return unicode.UTF16(unicode.BigEndian, unicode.IgnoreBOM), nil
	case "latin1", "iso-8859-1", "iso8859-1":
		// WHATWG maps these labels to Windows-1252; keep true ISO-8859-1
		// so a round trip never turns 0x80–0x9F into different characters.
		return charmap.ISO8859_1, nil
	}
	if enc, err := htmlindex.Get(key); err == nil && enc != nil {
		return enc, nil
	}
	if enc, err := ianaindex.IANA.Encoding(key); err == nil && enc != nil {
		return enc, nil
	}
	return nil, fmt.Errorf("unsupported encoding %q", key)
}

// DecodeOutput transcodes a cmd.exec result's "stdout"/"stderr" from
// encodingName to UTF-8 in place — for a subprocess that writes in a legacy
// code page (e.g. a Windows console tool emitting "ibm850"). "" is a no-op.
func DecodeOutput(result map[string]any, encodingName string) error {
	if encodingName == "" {
		return nil
	}
	for _, key := range []string{"stdout", "stderr"} {
		raw, ok := result[key].(string)
		if !ok {
			continue
		}
		text, err := Decode([]byte(raw), encodingName)
		if err != nil {
			return fmt.Errorf("%s: %w", key, err)
		}
		result[key] = text
	}
	return nil
}
