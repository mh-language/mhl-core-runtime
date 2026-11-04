// Package pack reads an encrypted workflows pack — the `DMPK` v2 artifact a
// host ships its .mh programs and prompts in, so that `mhl serve mcp
// --workflows-pack` can load them without the plaintext ever touching the
// disk (docs/MHL-WORKFLOW-PACK-SPEC.md).
//
// Layout (integers little-endian):
//
//	header:  "DMPK" | version u8 = 2 | 3 zero bytes | count u32
//	entry i: nonce [12] | ct_len u32 | ct [ct_len]
//
// Each ct is AES-256-GCM (ciphertext || 16-byte tag) with the entry index as
// a u32 AAD, over: path_len u16 | path | content. The path is encrypted with
// the content, and the AAD pins every entry to its position, so reordering,
// dropping or splicing entries fails authentication. Any failure rejects the
// whole pack.
package pack

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"unicode/utf8"
)

const (
	magic   = "DMPK"
	version = 2
	// KeySize is the AES-256 key length a pack is sealed with.
	KeySize = 32

	headerLen  = 12
	nonceLen   = 12
	tagLen     = 16
	maxPathLen = 1<<16 - 1

	// MaxEntries and MaxEntryLen bound a pack before anything is allocated
	// from its header, so a tampered count or length cannot drive a huge
	// allocation.
	MaxEntries  = 100_000
	MaxEntryLen = 16 << 20
)

// File is one decrypted entry: a slash-separated path relative to the pack
// root, and the file's bytes untransformed.
type File struct {
	Path    string
	Content []byte
}

// Open decrypts raw with key and returns its files in pack order. Nothing is
// written anywhere. Errors never carry plaintext or crypto detail.
func Open(raw, key []byte) ([]File, error) {
	aead, err := newAEAD(key)
	if err != nil {
		return nil, err
	}
	if len(raw) < headerLen || string(raw[:4]) != magic {
		return nil, errors.New("pack: not a workflows pack (bad magic)")
	}
	if raw[4] != version {
		return nil, fmt.Errorf("pack: unsupported version %d (want %d)", raw[4], version)
	}
	count := binary.LittleEndian.Uint32(raw[8:headerLen])
	if count > MaxEntries {
		return nil, fmt.Errorf("pack: %d entries exceeds the limit of %d", count, MaxEntries)
	}

	out := make([]File, 0, count)
	seen := make(map[string]bool, count)
	aad := make([]byte, 4)
	p := headerLen
	for i := range int(count) {
		if len(raw)-p < nonceLen+4 {
			return nil, fmt.Errorf("pack: truncated at entry %d", i)
		}
		nonce := raw[p : p+nonceLen]
		p += nonceLen
		ctLen := binary.LittleEndian.Uint32(raw[p : p+4])
		p += 4
		if ctLen > MaxEntryLen {
			return nil, fmt.Errorf("pack: entry %d exceeds the %d byte limit", i, MaxEntryLen)
		}
		if uint64(len(raw)-p) < uint64(ctLen) {
			return nil, fmt.Errorf("pack: truncated at entry %d", i)
		}
		binary.LittleEndian.PutUint32(aad, uint32(i))
		plain, err := aead.Open(nil, nonce, raw[p:p+int(ctLen)], aad)
		if err != nil {
			// Deliberately not wrapped: a wrong key and a tampered byte look
			// the same from here, and neither needs more detail.
			return nil, fmt.Errorf("pack: entry %d does not authenticate (wrong key or tampered pack)", i)
		}
		p += int(ctLen)
		if len(plain) < 2 {
			return nil, fmt.Errorf("pack: entry %d has no path header", i)
		}
		n := int(binary.LittleEndian.Uint16(plain[:2]))
		if 2+n > len(plain) {
			return nil, fmt.Errorf("pack: entry %d has a truncated path", i)
		}
		name := string(plain[2 : 2+n])
		if err := validPath(name); err != nil {
			return nil, fmt.Errorf("pack: entry %d: %w", i, err)
		}
		if seen[name] {
			return nil, fmt.Errorf("pack: entry %d repeats path %q", i, name)
		}
		seen[name] = true
		out = append(out, File{Path: name, Content: plain[2+n:]})
	}
	if p != len(raw) {
		return nil, fmt.Errorf("pack: %d trailing bytes after the last entry", len(raw)-p)
	}
	return out, nil
}

// Seal is Open's inverse: it encrypts files, in the order given, under key
// with a fresh random nonce per entry. The host's build normally produces the
// pack; Seal exists so tests (and tooling) can build one.
func Seal(files []File, key []byte) ([]byte, error) {
	aead, err := newAEAD(key)
	if err != nil {
		return nil, err
	}
	if len(files) > MaxEntries {
		return nil, fmt.Errorf("pack: %d entries exceeds the limit of %d", len(files), MaxEntries)
	}
	out := make([]byte, headerLen, headerLen+len(files)*(nonceLen+4+tagLen))
	copy(out, magic)
	out[4] = version
	binary.LittleEndian.PutUint32(out[8:], uint32(len(files)))
	aad := make([]byte, 4)
	for i, f := range files {
		if err := validPath(f.Path); err != nil {
			return nil, err
		}
		if len(f.Path) > maxPathLen {
			return nil, fmt.Errorf("pack: path %q is too long", f.Path)
		}
		plain := make([]byte, 2, 2+len(f.Path)+len(f.Content))
		binary.LittleEndian.PutUint16(plain, uint16(len(f.Path)))
		plain = append(append(plain, f.Path...), f.Content...)
		if len(plain)+tagLen > MaxEntryLen {
			return nil, fmt.Errorf("pack: %q exceeds the %d byte limit", f.Path, MaxEntryLen)
		}
		nonce := make([]byte, nonceLen)
		if _, err := rand.Read(nonce); err != nil {
			return nil, err
		}
		binary.LittleEndian.PutUint32(aad, uint32(i))
		ct := aead.Seal(nil, nonce, plain, aad)
		out = append(out, nonce...)
		out = binary.LittleEndian.AppendUint32(out, uint32(len(ct)))
		out = append(out, ct...)
	}
	return out, nil
}

func newAEAD(key []byte) (cipher.AEAD, error) {
	if len(key) != KeySize {
		return nil, fmt.Errorf("pack: key must be %d bytes, got %d", KeySize, len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// validPath accepts exactly the clean, relative, slash-separated paths a pack
// may carry: fs.ValidPath (no empty, "." or ".." element, no leading or
// trailing slash) minus the root itself, valid UTF-8, no backslash or NUL.
func validPath(p string) error {
	switch {
	case p == "" || p == ".":
		return errors.New("empty path")
	case !utf8.ValidString(p):
		return errors.New("path is not valid UTF-8")
	case strings.ContainsAny(p, "\\\x00"):
		return fmt.Errorf("path %q contains a backslash or NUL", p)
	case !fs.ValidPath(p):
		return fmt.Errorf("path %q is not a clean relative path", p)
	}
	return nil
}
