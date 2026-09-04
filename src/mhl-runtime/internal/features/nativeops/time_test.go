package nativeops_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/mh-language/mhl-core-runtime/internal/features/nativeops"
)

func TestTimeNowReturnsRFC3339UTC(t *testing.T) {
	got := nativeops.TimeNow()
	parsed, err := time.Parse(time.RFC3339, got)
	if err != nil {
		t.Fatalf("TimeNow() = %q, not valid RFC3339: %v", got, err)
	}
	if parsed.Location() != time.UTC {
		t.Errorf("TimeNow() location = %v, want UTC", parsed.Location())
	}
	if d := time.Since(parsed); d < 0 || d > 5*time.Second {
		t.Errorf("TimeNow() = %q, not close to time.Now(): off by %v", got, d)
	}
}

func TestTimeParseDefaultLayoutRoundTrips(t *testing.T) {
	got, err := nativeops.TimeParse("2024-01-15T10:30:00Z", "")
	if err != nil {
		t.Fatalf("TimeParse: %v", err)
	}
	if got != "2024-01-15T10:30:00Z" {
		t.Errorf("got %q, want %q", got, "2024-01-15T10:30:00Z")
	}
}

func TestTimeParseCustomLayout(t *testing.T) {
	got, err := nativeops.TimeParse("2024-01-15", "2006-01-02")
	if err != nil {
		t.Fatalf("TimeParse: %v", err)
	}
	if got != "2024-01-15T00:00:00Z" {
		t.Errorf("got %q, want %q", got, "2024-01-15T00:00:00Z")
	}
}

func TestTimeParseFriendlyDayMonthYearLayout(t *testing.T) {
	got, err := nativeops.TimeParse("15/01/2024", "dd/MM/yyyy")
	if err != nil {
		t.Fatalf("TimeParse: %v", err)
	}
	if got != "2024-01-15T00:00:00Z" {
		t.Errorf("got %q, want %q", got, "2024-01-15T00:00:00Z")
	}
}

func TestTimeParseInvalidTextErrors(t *testing.T) {
	_, err := nativeops.TimeParse("not a date", "")
	if err == nil {
		t.Fatal("expected an error for unparseable text")
	}
	if !strings.Contains(err.Error(), "time.parse") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestTimeFormatCustomLayout(t *testing.T) {
	got, err := nativeops.TimeFormat("2024-01-15T10:30:00Z", "2006-01-02")
	if err != nil {
		t.Fatalf("TimeFormat: %v", err)
	}
	if got != "2024-01-15" {
		t.Errorf("got %q, want %q", got, "2024-01-15")
	}
}

func TestTimeFormatFriendlyDayMonthYearLayout(t *testing.T) {
	got, err := nativeops.TimeFormat("2024-01-15T10:30:00Z", "dd/MM/yyyy")
	if err != nil {
		t.Fatalf("TimeFormat: %v", err)
	}
	if got != "15/01/2024" {
		t.Errorf("got %q, want %q", got, "15/01/2024")
	}
}

func TestTimeFormatFriendlyTimeLayoutDistinguishesMinuteFromMonth(t *testing.T) {
	// Lowercase "mm" is minute, uppercase "MM" is month — the same split
	// moment.js/day.js use, since "m" alone is ambiguous between the two.
	got, err := nativeops.TimeFormat("2024-01-15T10:30:00Z", "HH:mm:ss")
	if err != nil {
		t.Fatalf("TimeFormat: %v", err)
	}
	if got != "10:30:00" {
		t.Errorf("got %q, want %q", got, "10:30:00")
	}
}

func TestTimeFormatFriendlyLayoutMixedWithLiteralText(t *testing.T) {
	got, err := nativeops.TimeFormat("2024-01-15T10:30:00Z", "yyyy-MM-dd HH:mm")
	if err != nil {
		t.Fatalf("TimeFormat: %v", err)
	}
	if got != "2024-01-15 10:30" {
		t.Errorf("got %q, want %q", got, "2024-01-15 10:30")
	}
}

func TestTimeFormatDefaultLayoutIsIdentity(t *testing.T) {
	got, err := nativeops.TimeFormat("2024-01-15T10:30:00Z", "")
	if err != nil {
		t.Fatalf("TimeFormat: %v", err)
	}
	if got != "2024-01-15T10:30:00Z" {
		t.Errorf("got %q, want %q", got, "2024-01-15T10:30:00Z")
	}
}

func TestTimeFormatInvalidValueErrors(t *testing.T) {
	_, err := nativeops.TimeFormat("not a date", "")
	if err == nil {
		t.Fatal("expected an error for an unparseable value")
	}
	if !strings.Contains(err.Error(), "time.format") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestTimeAddPositiveDuration(t *testing.T) {
	got, err := nativeops.TimeAdd("2024-01-15T10:30:00Z", time.Hour)
	if err != nil {
		t.Fatalf("TimeAdd: %v", err)
	}
	if got != "2024-01-15T11:30:00Z" {
		t.Errorf("got %q, want %q", got, "2024-01-15T11:30:00Z")
	}
}

func TestTimeAddInvalidValueErrors(t *testing.T) {
	_, err := nativeops.TimeAdd("not a date", time.Hour)
	if err == nil {
		t.Fatal("expected an error for an unparseable value")
	}
	if !strings.Contains(err.Error(), "time.add") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestTimeAddDiffRoundTrips(t *testing.T) {
	start := "2024-01-15T10:30:00Z"
	end, err := nativeops.TimeAdd(start, time.Hour)
	if err != nil {
		t.Fatalf("TimeAdd: %v", err)
	}
	got, err := nativeops.TimeDiff(end, start)
	if err != nil {
		t.Fatalf("TimeDiff: %v", err)
	}
	if got != 3600 {
		t.Errorf("got %v, want 3600", got)
	}
}

func TestTimeDiffSecondsBetweenTwoTimestamps(t *testing.T) {
	got, err := nativeops.TimeDiff("2024-01-15T11:00:00Z", "2024-01-15T10:00:00Z")
	if err != nil {
		t.Fatalf("TimeDiff: %v", err)
	}
	if got != 3600 {
		t.Errorf("got %v, want 3600", got)
	}
}

func TestTimeDiffNegativeWhenAIsEarlier(t *testing.T) {
	got, err := nativeops.TimeDiff("2024-01-15T10:00:00Z", "2024-01-15T11:00:00Z")
	if err != nil {
		t.Fatalf("TimeDiff: %v", err)
	}
	if got != -3600 {
		t.Errorf("got %v, want -3600", got)
	}
}

func TestTimeDiffInvalidInputErrors(t *testing.T) {
	_, err := nativeops.TimeDiff("not a date", "2024-01-15T10:00:00Z")
	if err == nil {
		t.Fatal("expected an error for an unparseable first argument")
	}
	if !strings.Contains(err.Error(), "time.diff") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestTimeCompareOrdersChronologically(t *testing.T) {
	cases := []struct {
		a, b string
		want float64
	}{
		{"2024-01-15T10:00:00Z", "2024-01-15T11:00:00Z", -1},
		{"2024-01-15T11:00:00Z", "2024-01-15T10:00:00Z", 1},
		{"2024-01-15T10:00:00Z", "2024-01-15T10:00:00Z", 0},
	}
	for _, c := range cases {
		got, err := nativeops.TimeCompare(c.a, c.b)
		if err != nil {
			t.Fatalf("TimeCompare(%q, %q): %v", c.a, c.b, err)
		}
		if got != c.want {
			t.Errorf("TimeCompare(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestTimeCompareInvalidInputErrors(t *testing.T) {
	_, err := nativeops.TimeCompare("2024-01-15T10:00:00Z", "not a date")
	if err == nil {
		t.Fatal("expected an error for an unparseable second argument")
	}
	if !strings.Contains(err.Error(), "time.compare") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestTimeSleepBlocksForDuration(t *testing.T) {
	start := time.Now()
	if err := nativeops.TimeSleep(context.Background(), 40*time.Millisecond); err != nil {
		t.Fatalf("TimeSleep: %v", err)
	}
	if elapsed := time.Since(start); elapsed < 35*time.Millisecond {
		t.Errorf("TimeSleep returned after %v, want >= ~40ms", elapsed)
	}
}

func TestTimeSleepNonPositiveIsNoOp(t *testing.T) {
	start := time.Now()
	for _, d := range []time.Duration{0, -5 * time.Second} {
		if err := nativeops.TimeSleep(context.Background(), d); err != nil {
			t.Fatalf("TimeSleep(%v): %v", d, err)
		}
	}
	if elapsed := time.Since(start); elapsed > 10*time.Millisecond {
		t.Errorf("non-positive TimeSleep took %v, want immediate", elapsed)
	}
}

func TestTimeSleepInterruptedByContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	start := time.Now()
	err := nativeops.TimeSleep(ctx, 5*time.Second)
	if err == nil {
		t.Fatal("expected a context error when cancelled mid-sleep")
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Errorf("TimeSleep did not stop promptly on cancel: waited %v", elapsed)
	}
}
