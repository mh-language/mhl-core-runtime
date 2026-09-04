package nativeops

import (
	"context"
	"fmt"
	"time"
)

// parseRFC3339 parses s as the UTC RFC3339 timestamp every Time* function
// here reads and produces (TimeNow's own output, and anything TimeParse
// already normalized) — the shared boundary Format/Add/Diff/Compare all
// start from.
func parseRFC3339(s string) (time.Time, error) {
	return time.Parse(time.RFC3339, s)
}

// TimeNow returns the current instant as a UTC RFC3339 string — the same
// convention internal/features/memory/jsonlog.go's "ts" field already uses,
// so a value written by the memory logger and one produced here are
// interchangeable.
func TimeNow() string {
	return time.Now().UTC().Format(time.RFC3339)
}

// TimeParse parses text with layout and reformats the result as UTC
// RFC3339 — the normalization step lets a caller ingest a timestamp in
// whatever format an external source hands it and store/compare it in
// mhl's single canonical shape from then on. layout accepts either a
// friendly token pattern (e.g. "dd/MM/yyyy", see friendlyLayoutToGo) or a
// raw Go reference-time layout; empty means time.RFC3339.
func TimeParse(text, layout string) (string, error) {
	t, err := time.Parse(friendlyLayoutToGo(layout), text)
	if err != nil {
		return "", fmt.Errorf("time.parse %q: %w", text, err)
	}
	return t.UTC().Format(time.RFC3339), nil
}

// TimeFormat parses value (UTC RFC3339) and renders it with layout — a
// friendly token pattern (e.g. "dd/MM/yyyy") or a raw Go reference-time
// layout; empty means time.RFC3339, i.e. the identity transform. See
// friendlyLayoutToGo for the supported tokens.
func TimeFormat(value, layout string) (string, error) {
	t, err := parseRFC3339(value)
	if err != nil {
		return "", fmt.Errorf("time.format %q: %w", value, err)
	}
	return t.Format(friendlyLayoutToGo(layout)), nil
}

// TimeAdd parses value (UTC RFC3339), adds d, and reformats the result as
// UTC RFC3339. d comes from an mhl duration literal (e.g. "7d"), which the
// language's lexer only ever produces unsigned, so this can only move value
// forward in time, never back.
func TimeAdd(value string, d time.Duration) (string, error) {
	t, err := parseRFC3339(value)
	if err != nil {
		return "", fmt.Errorf("time.add %q: %w", value, err)
	}
	return t.Add(d).UTC().Format(time.RFC3339), nil
}

// TimeSleep blocks for d, then returns nil — the deliberate-delay primitive
// (polling backoff, rate-pacing a loop, spacing retries). It is
// cancellation-aware: if ctx is cancelled first — a run-level cancel, or the
// enclosing step's `timeout` firing — it stops waiting immediately and
// returns ctx.Err(), so the step fails like any other interrupted blocking
// call rather than the sleep swallowing the signal. A non-positive d is a
// no-op (mhl duration literals are always unsigned; a computed value can
// still be zero or negative).
func TimeSleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// TimeDiff returns the number of seconds between a and b (a minus b), both
// UTC RFC3339. mhl has a single number type (float64) and no first-class
// duration value, so the result is plain seconds rather than a richer
// duration object — a caller divides by 60/3600/86400 itself if it needs
// minutes/hours/days.
func TimeDiff(a, b string) (float64, error) {
	ta, err := parseRFC3339(a)
	if err != nil {
		return 0, fmt.Errorf("time.diff %q: %w", a, err)
	}
	tb, err := parseRFC3339(b)
	if err != nil {
		return 0, fmt.Errorf("time.diff %q: %w", b, err)
	}
	return ta.Sub(tb).Seconds(), nil
}

// TimeCompare orders two UTC RFC3339 timestamps, returning -1 if a is
// before b, 1 if a is after b, 0 if equal. mhl's comparison operators
// (<, <=, >, >=) only accept number operands, so this is the only way to
// order two datetime strings from within a .mh program.
func TimeCompare(a, b string) (float64, error) {
	ta, err := parseRFC3339(a)
	if err != nil {
		return 0, fmt.Errorf("time.compare %q: %w", a, err)
	}
	tb, err := parseRFC3339(b)
	if err != nil {
		return 0, fmt.Errorf("time.compare %q: %w", b, err)
	}
	switch {
	case ta.Before(tb):
		return -1, nil
	case ta.After(tb):
		return 1, nil
	default:
		return 0, nil
	}
}
