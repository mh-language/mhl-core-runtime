package nativeops

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// UUIDv4 returns a random (version 4) UUID as its canonical 36-character
// lowercase string, e.g. "9b1deb4d-3b7d-4bad-9bdd-2b0d7b3dcb6d" (RFC 9562
// §5.4). All 122 non-fixed bits come from crypto/rand; an entropy failure
// is returned as an error rather than papered over, so a caller step fails
// the same way a bad `fail()` would.
func UUIDv4() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("uuid.v4: reading random bytes: %w", err)
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // RFC 9562 variant
	return format(b), nil
}

// UUIDv7 returns a time-ordered (version 7) UUID as its canonical
// 36-character lowercase string (RFC 9562 §5.7): a 48-bit big-endian
// Unix-epoch millisecond timestamp followed by 74 bits from crypto/rand
// (with the version and variant bits overwritten). Two v7 values minted in
// the same process sort in creation order down to the millisecond; within
// one millisecond the random tail decides. An entropy failure is returned
// as an error.
func UUIDv7() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[6:]); err != nil {
		return "", fmt.Errorf("uuid.v7: reading random bytes: %w", err)
	}
	ms := uint64(time.Now().UnixMilli())
	b[0] = byte(ms >> 40)
	b[1] = byte(ms >> 32)
	b[2] = byte(ms >> 24)
	b[3] = byte(ms >> 16)
	b[4] = byte(ms >> 8)
	b[5] = byte(ms)
	b[6] = (b[6] & 0x0f) | 0x70 // version 7
	b[8] = (b[8] & 0x3f) | 0x80 // RFC 9562 variant
	return format(b), nil
}

// format renders 16 bytes as the canonical 8-4-4-4-12 lowercase hex form.
func format(b [16]byte) string {
	var buf [36]byte
	hex.Encode(buf[0:8], b[0:4])
	buf[8] = '-'
	hex.Encode(buf[9:13], b[4:6])
	buf[13] = '-'
	hex.Encode(buf[14:18], b[6:8])
	buf[18] = '-'
	hex.Encode(buf[19:23], b[8:10])
	buf[23] = '-'
	hex.Encode(buf[24:36], b[10:16])
	return string(buf[:])
}
