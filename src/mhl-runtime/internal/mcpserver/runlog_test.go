package mcpserver

import (
	"strings"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/features/auth"
)

// drainAll polls read from a cursor to exhaustion, returning every fragment
// concatenated and the fragments themselves. It stops when the cursor stops
// advancing.
func drainAll(r *ringLog, from int64) (joined string, frags []string) {
	var b strings.Builder
	cur := from
	for {
		text, next, _ := r.read(cur)
		if text != "" {
			b.WriteString(text)
			frags = append(frags, text)
		}
		if next <= cur {
			break
		}
		cur = next
	}
	return b.String(), frags
}

// TestAssessmentLogFragmentsMustNotLeak is the review probe, kept as a
// permanent regression: a secret split across two writes must not be
// reconstructable by concatenating two successive run/logs reads.
func TestAssessmentLogFragmentsMustNotLeak(t *testing.T) {
	secret := "SYNTHETIC_LOG_REVIEW_TOKEN"
	auth.Register(secret)

	r := newRingLog()
	cut := 10
	r.Write([]byte(secret[:cut]))
	a, next, _ := r.read(0)
	r.Write([]byte(secret[cut:]))
	b, _, _ := r.read(next)

	if strings.Contains(a+b, secret) {
		t.Fatalf("two successive run/logs reads reconstruct the registered secret\n a=%q\n b=%q", a, b)
	}
}

// TestRingLogSecretSplitAtEveryBoundary writes a line containing a secret one
// byte at a time, polling after each write. No concatenation of the fragments
// returned so far may contain the secret, at any point in the stream.
func TestRingLogSecretSplitAtEveryBoundary(t *testing.T) {
	secret := "R3_STREAMING_SECRET_VALUE_abcdef"
	auth.Register(secret)
	full := "before " + secret + " after\n"

	r := newRingLog()
	var seen strings.Builder
	cur := int64(0)
	for i := 0; i < len(full); i++ {
		r.Write([]byte{full[i]})
		text, next, _ := r.read(cur)
		seen.WriteString(text)
		cur = next
		if strings.Contains(seen.String(), secret) {
			t.Fatalf("secret leaked after writing %d/%d bytes:\n%q", i+1, len(full), seen.String())
		}
	}

	// Once sealed, the whole non-secret frame must have been delivered, with the
	// secret masked exactly once.
	r.Seal()
	tail, _ := drainAll(r, cur)
	out := seen.String() + tail
	if strings.Contains(out, secret) {
		t.Fatalf("secret leaked in the sealed drain: %q", out)
	}
	if !strings.Contains(out, "before ") || !strings.Contains(out, " after") {
		t.Fatalf("non-secret content lost: %q", out)
	}
	if strings.Count(out, "[REDACTED]") != 1 {
		t.Fatalf("want the secret masked exactly once, got %d in %q", strings.Count(out, "[REDACTED]"), out)
	}
}

// TestRingLogCursorLandsMidSecret checks the case where a caller's returned
// cursor would fall inside a secret: the cursor must be walked back so the
// next read re-covers the whole secret with left context.
func TestRingLogCursorLandsMidSecret(t *testing.T) {
	secret := "R3_MIDCURSOR_SECRET_0123456789"
	auth.Register(secret)

	r := newRingLog()
	head := strings.Repeat("x", 200)
	r.Write([]byte(head + secret + strings.Repeat("y", 200)))
	r.Seal()

	joined, _ := drainAll(r, 0)
	if strings.Contains(joined, secret) {
		t.Fatalf("secret survived a full drain: %q", joined)
	}
	if !strings.Contains(joined, "[REDACTED]") {
		t.Fatalf("secret was never masked: %q", joined)
	}
	if got := strings.Count(joined, "x") + strings.Count(joined, "y"); got != 400 {
		t.Fatalf("non-secret padding lost: got %d of 400", got)
	}

	// Re-read from a cursor deliberately inside the secret's byte range: still
	// no leak of the secret's tail.
	mid := int64(len(head) + len(secret)/2)
	text, _, _ := r.read(mid)
	if strings.Contains(text, secret[len(secret)/2:]) {
		t.Fatalf("read from mid-secret cursor leaked the tail: %q", text)
	}
}

// TestRingLogTruncationDoesNotSplitSecret drives the buffer past ringLogMax
// with a secret straddling the drop point. The retained buffer must not begin
// in the middle of the secret, and no drain may leak it.
func TestRingLogTruncationDoesNotSplitSecret(t *testing.T) {
	secret := "R3_TRUNCATION_BOUNDARY_SECRET_9f9f9f"
	auth.Register(secret)

	r := newRingLog()
	// Fill to just under the cap, then write the secret so it will straddle the
	// drop boundary once more bytes push the buffer over ringLogMax.
	r.Write([]byte(strings.Repeat("a", ringLogMax-len(secret)/2)))
	r.Write([]byte(secret))
	r.Write([]byte(strings.Repeat("b", ringLogMax))) // forces the drop
	r.Seal()

	joined, _ := drainAll(r, 0)
	if strings.Contains(joined, secret) {
		t.Fatalf("secret leaked after truncation: len=%d", len(joined))
	}
}

// TestRingLogSealReleasesHeldTail verifies the live/hold-back behaviour: while
// unsealed the last MaxLen bytes are withheld; Seal releases them.
func TestRingLogSealReleasesHeldTail(t *testing.T) {
	secret := "R3_TAIL_HOLDBACK_SECRET_zzzz"
	auth.Register(secret)
	margin := auth.MaxLen()

	r := newRingLog()
	body := strings.Repeat("m", margin*3) + "END"
	r.Write([]byte(body))

	live, _ := drainAll(r, 0)
	if len(live) >= len(body) {
		t.Fatalf("live read returned the whole body (%d) — tail not held back", len(live))
	}

	r.Seal()
	sealed, _ := drainAll(r, 0)
	if !strings.HasSuffix(sealed, "END") {
		t.Fatalf("sealed drain missing the tail: ...%q", tailOf(sealed, 8))
	}
	if strings.Contains(sealed, secret) {
		t.Fatalf("unexpected secret in %q", sealed)
	}
}

func tailOf(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

// TestRingLogConcurrentWriteRead runs parallel writers (as parallel steps do)
// against a poller and asserts, under -race, that no read ever surfaces the
// registered secret.
func TestRingLogConcurrentWriteRead(t *testing.T) {
	secret := "R3_CONCURRENT_SECRET_deadbeef"
	auth.Register(secret)

	r := newRingLog()
	chunk := "line " + secret + " end "
	done := make(chan struct{})

	go func() {
		for i := 0; i < 400; i++ {
			for j := 0; j < len(chunk); j += 3 {
				end := j + 3
				if end > len(chunk) {
					end = len(chunk)
				}
				r.Write([]byte(chunk[j:end]))
			}
		}
		close(done)
	}()

	cur := int64(0)
	poll := func() {
		text, next, _ := r.read(cur)
		if strings.Contains(text, secret) {
			t.Errorf("concurrent read leaked the secret")
		}
		cur = next
	}
	for {
		select {
		case <-done:
			r.Seal()
			poll()
			return
		default:
			poll()
		}
	}
}
