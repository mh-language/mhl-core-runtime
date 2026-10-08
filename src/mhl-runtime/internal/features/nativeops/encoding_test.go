package nativeops_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/features/nativeops"
)

func TestDecode(t *testing.T) {
	cases := []struct {
		name     string
		raw      []byte
		encoding string
		want     string
		wantErr  string
	}{
		{"default passes bytes through", []byte("ol\xe1"), "", "ol\xe1", ""},
		{"utf-8 strips BOM", []byte("\xef\xbb\xbfolá"), "utf-8", "olá", ""},
		{"utf-8 rejects invalid bytes", []byte("ol\xe1"), "UTF8", "", "not valid UTF-8"},
		{"windows-1252", []byte("a\xe7\xe3o \x80"), "windows-1252", "ação €", ""},
		{"latin1 is true ISO-8859-1", []byte("\xe9\x80"), "latin1", "é\u0080", ""},
		{"utf-16 with LE BOM", []byte{0xFF, 0xFE, 'o', 0, 'l', 0, 0xE1, 0}, "utf-16", "olá", ""},
		{"utf-16 with BE BOM", []byte{0xFE, 0xFF, 0, 'o', 0, 'k'}, "utf-16", "ok", ""},
		{"utf-16le drops BOM", []byte{0xFF, 0xFE, 'o', 0, 'k', 0}, "utf-16le", "ok", ""},
		{"utf-16be", []byte{0, 'o', 0, 'k'}, "utf-16be", "ok", ""},
		{"ibm850 via IANA", []byte{0x87}, "IBM850", "ç", ""},
		{"auto: UTF-16LE BOM", []byte{0xFF, 0xFE, 'o', 0, 'k', 0}, "auto", "ok", ""},
		{"auto: UTF-8 BOM", []byte("\xef\xbb\xbfok"), "auto", "ok", ""},
		{"auto: valid UTF-8", []byte("ação"), "auto", "ação", ""},
		{"auto: falls back to windows-1252", []byte("a\xe7\xe3o"), "auto", "ação", ""},
		{"unknown", []byte("x"), "klingon", "", `unsupported encoding "klingon"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := nativeops.Decode(tc.raw, tc.encoding)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want containing %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestEncode(t *testing.T) {
	cases := []struct {
		name     string
		encoding string
		bom      bool
		want     []byte
		wantErr  string
	}{
		{"default", "", true, []byte("ação"), ""},
		{"utf-8-bom", "utf-8-sig", true, []byte("\xef\xbb\xbfação"), ""},
		{"utf-8-bom without bom", "utf-8-bom", false, []byte("ação"), ""},
		{"windows-1252", "cp1252", true, []byte("a\xe7\xe3o"), ""},
		{"utf-16 writes LE BOM", "utf-16", true, []byte{0xFF, 0xFE, 'a', 0, 0xE7, 0, 0xE3, 0, 'o', 0}, ""},
		{"utf-16 append has no BOM", "utf-16", false, []byte{'a', 0, 0xE7, 0, 0xE3, 0, 'o', 0}, ""},
		{"auto is read-only", "auto", true, nil, "only valid when reading"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := nativeops.Encode("ação", tc.encoding, tc.bom)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want containing %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, tc.want) {
				t.Fatalf("got % x, want % x", got, tc.want)
			}
		})
	}
}

func TestEncodeRejectsUnrepresentable(t *testing.T) {
	if _, err := nativeops.Encode("emoji 🙂", "windows-1252", true); err == nil {
		t.Fatal("expected an error for a rune windows-1252 cannot represent")
	}
}

func TestEncodedRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.txt")
	if _, err := nativeops.WriteEncoded(path, "linha 1 ç\n", "utf-16"); err != nil {
		t.Fatal(err)
	}
	if _, err := nativeops.AppendEncoded(path, "linha 2 ã\n", "utf-16"); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	if n := bytes.Count(raw, []byte{0xFF, 0xFE}); n != 1 {
		t.Fatalf("expected exactly one BOM, found %d in % x", n, raw)
	}
	got, err := nativeops.ReadEncoded(path, "utf-16")
	if err != nil {
		t.Fatal(err)
	}
	if got != "linha 1 ç\nlinha 2 ã\n" {
		t.Fatalf("got %q", got)
	}
}

func TestReadEncodedRejectsUnknownBeforeReading(t *testing.T) {
	_, err := nativeops.ReadEncoded(filepath.Join(t.TempDir(), "missing"), "nope")
	if err == nil || !strings.Contains(err.Error(), "unsupported encoding") {
		t.Fatalf("err = %v", err)
	}
}
