package pack

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"strings"
	"testing"
	"testing/fstest"
)

// The interoperability vector from docs/MHL-WORKFLOW-PACK-SPEC.md §5 — a
// golden pack produced by the host's own writer. If it stops opening, the
// format has drifted from the producer.
const (
	vectorKey = "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"
	vectorHex = "444d504b0200000002000000000102030405060708090a0b340000005102a174b78e9d72f9" +
		"24faa4c6860a06dcbff3519d5532144f0897ee7b056fc5216bd3f6588d72e2218516c3915" +
		"0cd1721084ab60c0d0e0f10111213141516173700000086fe1ab01e049937184b2fb0ffff" +
		"55c834648880820e15dae9cd9790908ead9e5c85054891b12ca5dbec022b119fc25e7f257" +
		"e3d1553aa"
)

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestOpenInteropVector(t *testing.T) {
	raw := mustHex(t, vectorHex)
	if len(raw) != 151 {
		t.Fatalf("vector is %d bytes, want 151", len(raw))
	}
	files, err := Open(raw, mustHex(t, vectorKey))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	want := []File{
		{Path: "work_item/work_item.mh", Content: []byte("workflow {}\n")},
		{Path: "shared/agents/agents.prompt.md", Content: []byte("prompt\n")},
	}
	if len(files) != len(want) {
		t.Fatalf("got %d files, want %d", len(files), len(want))
	}
	for i := range want {
		if files[i].Path != want[i].Path || !bytes.Equal(files[i].Content, want[i].Content) {
			t.Errorf("entry %d = %q %q, want %q %q", i, files[i].Path, files[i].Content, want[i].Path, want[i].Content)
		}
	}
}

// Every single-byte change to the vector — header, nonce, length or
// ciphertext — must reject the whole pack.
func TestOpenRejectsAnyFlippedByte(t *testing.T) {
	raw := mustHex(t, vectorHex)
	key := mustHex(t, vectorKey)
	for i := range raw {
		// The three pad bytes are ignored by design (the reader does not
		// check them), so flipping one is not tampering with anything.
		if i >= 5 && i < 8 {
			continue
		}
		mod := bytes.Clone(raw)
		mod[i] ^= 0x01
		if _, err := Open(mod, key); err == nil {
			t.Errorf("byte %d flipped: Open succeeded", i)
		}
	}
}

func TestOpenRejectsTruncationAndTrailingBytes(t *testing.T) {
	raw := mustHex(t, vectorHex)
	key := mustHex(t, vectorKey)
	for n := range len(raw) {
		if _, err := Open(raw[:n], key); err == nil {
			t.Errorf("truncated to %d bytes: Open succeeded", n)
		}
	}
	if _, err := Open(append(bytes.Clone(raw), 0), key); err == nil || !strings.Contains(err.Error(), "trailing") {
		t.Errorf("trailing byte: err = %v", err)
	}
}

func TestOpenRejectsBadKeys(t *testing.T) {
	raw := mustHex(t, vectorHex)
	wrong := mustHex(t, vectorKey)
	wrong[0] ^= 0xff
	for name, key := range map[string][]byte{"wrong": wrong, "short": wrong[:16], "empty": nil} {
		if _, err := Open(raw, key); err == nil {
			t.Errorf("%s key: Open succeeded", name)
		}
	}
}

// A wrong key's error says only that the entry did not authenticate.
func TestOpenErrorCarriesNoPlaintext(t *testing.T) {
	key := mustHex(t, vectorKey)
	raw, err := Seal([]File{{Path: "secret/plan.prompt.md", Content: []byte("the secret prompt")}}, key)
	if err != nil {
		t.Fatal(err)
	}
	key[0] ^= 0xff
	_, err = Open(raw, key)
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Errorf("err = %v", err)
	}
}

func TestOpenRejectsHeaders(t *testing.T) {
	key := mustHex(t, vectorKey)
	raw := mustHex(t, vectorHex)

	badMagic := bytes.Clone(raw)
	copy(badMagic, "XXXX")
	badVersion := bytes.Clone(raw)
	badVersion[4] = 1
	tooMany := bytes.Clone(raw[:headerLen])
	binary.LittleEndian.PutUint32(tooMany[8:], MaxEntries+1)
	hugeEntry := bytes.Clone(raw[:headerLen+nonceLen+4])
	binary.LittleEndian.PutUint32(hugeEntry[headerLen+nonceLen:], MaxEntryLen+1)

	for name, c := range map[string]struct {
		raw  []byte
		want string
	}{
		"magic":   {badMagic, "bad magic"},
		"version": {badVersion, "version 1"},
		"count":   {tooMany, "exceeds the limit"},
		"ct_len":  {hugeEntry, "byte limit"},
	} {
		if _, err := Open(c.raw, key); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want it to mention %q", name, err, c.want)
		}
	}
}

// sealRaw builds a pack whose plaintext entries are given verbatim, so a test
// can produce paths Seal itself refuses to write.
func sealRaw(t *testing.T, key []byte, paths ...string) []byte {
	t.Helper()
	aead, err := newAEAD(key)
	if err != nil {
		t.Fatal(err)
	}
	out := []byte(magic)
	out = append(out, version, 0, 0, 0)
	out = binary.LittleEndian.AppendUint32(out, uint32(len(paths)))
	for i, p := range paths {
		plain := binary.LittleEndian.AppendUint16(nil, uint16(len(p)))
		plain = append(append(plain, p...), "x"...)
		nonce := make([]byte, nonceLen)
		nonce[0] = byte(i)
		ct := aead.Seal(nil, nonce, plain, binary.LittleEndian.AppendUint32(nil, uint32(i)))
		out = append(out, nonce...)
		out = binary.LittleEndian.AppendUint32(out, uint32(len(ct)))
		out = append(out, ct...)
	}
	return out
}

func TestOpenRejectsBadPaths(t *testing.T) {
	key := mustHex(t, vectorKey)
	for _, p := range []string{"", ".", "/abs.mh", "../up.mh", "a/../b.mh", "a/./b.mh", "a//b.mh", "a/", `a\b.mh`, "a\x00.mh", "\xff.mh"} {
		if _, err := Open(sealRaw(t, key, p), key); err == nil {
			t.Errorf("path %q: Open succeeded", p)
		}
	}
	if _, err := Open(sealRaw(t, key, "a.mh", "a.mh"), key); err == nil || !strings.Contains(err.Error(), "repeats") {
		t.Errorf("repeated path: err = %v", err)
	}
}

func TestSealRoundTrip(t *testing.T) {
	key := mustHex(t, vectorKey)
	in := []File{
		{Path: "a/main.mh", Content: []byte("workflow Main { step S { } }\n")},
		{Path: "a/p.prompt.md", Content: []byte("hi ${name}\n")},
		{Path: "empty.json", Content: nil},
	}
	raw, err := Seal(in, key)
	if err != nil {
		t.Fatal(err)
	}
	out, err := Open(raw, key)
	if err != nil {
		t.Fatal(err)
	}
	for i := range in {
		if out[i].Path != in[i].Path || !bytes.Equal(out[i].Content, in[i].Content) {
			t.Errorf("entry %d = %+v, want %+v", i, out[i], in[i])
		}
	}
	if _, err := Seal([]File{{Path: "../x"}}, key); err == nil {
		t.Error("Seal accepted an unclean path")
	}
}

func TestFS(t *testing.T) {
	fsys, err := NewFS([]File{
		{Path: "shared/agents/agents.mh", Content: []byte("agent A {}")},
		{Path: "shared/agents/p.prompt.md", Content: []byte("p")},
		{Path: "shared/core.mh", Content: []byte("c")},
		{Path: "top.mh", Content: []byte("t")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := fstest.TestFS(fsys, "shared/agents/agents.mh", "shared/agents/p.prompt.md", "shared/core.mh", "top.mh"); err != nil {
		t.Fatal(err)
	}
	if _, err := NewFS([]File{{Path: "a", Content: nil}, {Path: "a/b", Content: nil}}); err == nil {
		t.Error("NewFS accepted a path that is both a file and a directory")
	}
}
