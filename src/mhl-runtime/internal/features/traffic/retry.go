// Package traffic contains agent request resilience controls.
package traffic

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"time"
)

// DefaultMaxDelay caps a single backoff wait when a Retrier declares no
// MaxDelay of its own — an exponential series with a large MaxAttempts must
// not grow an unbounded sleep.
const DefaultMaxDelay = 30 * time.Second

// permanentError marks a failure that must never be retried, whatever the
// RetryOn policy says (a 400/401/404, a schema-invalid response). An adapter
// wraps its error in Permanent(...) to say "asking again will not help".
type permanentError struct{ err error }

func (p permanentError) Error() string { return p.err.Error() }
func (p permanentError) Unwrap() error { return p.err }

// Permanent marks err as non-retryable. Retrier.Execute returns it as-is
// (unwrapped one level so the caller sees the original) without another
// attempt. Permanent(nil) is nil.
func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return permanentError{err}
}

// IsPermanent reports whether err (or anything it wraps) was marked Permanent.
func IsPermanent(err error) bool {
	var p permanentError
	return errors.As(err, &p)
}

// Result is the value returned by an attempted request together with its
// attempt count.
type Result struct {
	Value    any
	Attempts int
}

// Retrier retries configured transient failures with exponential backoff and
// full jitter. A zero Retrier runs its function exactly once.
type Retrier struct {
	MaxAttempts int
	Delay       time.Duration
	// MaxDelay caps one backoff wait; DefaultMaxDelay when zero.
	MaxDelay time.Duration
	RetryOn  []string
	// Sleep, when set, replaces the context-aware wait — tests pass a
	// recorder here. It does not observe cancellation; production paths leave
	// it nil so Execute's ctx governs the wait.
	Sleep func(time.Duration)
	// Rand, when set, supplies the jitter fraction in [0,1); defaults to a
	// package rand source. Tests set it to a constant for deterministic waits.
	Rand func() float64
}

// Execute invokes fn until it succeeds, the retry policy is exhausted, ctx is
// done, or fn returns a Permanent error. The backoff wait between attempts is
// cancelled by ctx, so a run-level cancel or deadline aborts a retry that is
// only sleeping. A nil ctx is treated as context.Background().
func (r Retrier) Execute(ctx context.Context, fn func() (Result, error)) (Result, error) {
	if fn == nil {
		return Result{}, fmt.Errorf("traffic: retry function is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	max := r.MaxAttempts
	if max <= 0 {
		max = 1
	}
	delay := r.Delay
	if delay <= 0 {
		delay = time.Second
	}
	maxDelay := r.MaxDelay
	if maxDelay <= 0 {
		maxDelay = DefaultMaxDelay
	}

	var last Result
	var err error
	for attempt := 1; attempt <= max; attempt++ {
		if ctxErr := ctx.Err(); ctxErr != nil {
			if attempt == 1 {
				return Result{}, ctxErr
			}
			return last, err
		}
		last, err = fn()
		last.Attempts = attempt
		if err == nil {
			return last, nil
		}
		if attempt == max || !r.shouldRetry(ctx, err) {
			return last, unwrapPermanent(err)
		}
		if waitErr := r.wait(ctx, r.backoff(delay, maxDelay, attempt)); waitErr != nil {
			// ctx ended during the backoff — surface the last real failure,
			// not the cancellation, so the caller sees why it was retrying.
			return last, err
		}
	}
	return last, unwrapPermanent(err)
}

// backoff is delay * 2^(attempt-1), clamped to maxDelay, with full jitter
// (a uniform pick in [0, clamped]).
func (r Retrier) backoff(delay, maxDelay time.Duration, attempt int) time.Duration {
	d := delay
	for i := 1; i < attempt; i++ {
		d <<= 1
		if d >= maxDelay || d <= 0 {
			d = maxDelay
			break
		}
	}
	frac := rand.Float64()
	if r.Rand != nil {
		frac = r.Rand()
	}
	return time.Duration(frac * float64(d))
}

// wait sleeps for d, honouring ctx. A test-supplied Sleep short-circuits it
// (and never observes cancellation).
func (r Retrier) wait(ctx context.Context, d time.Duration) error {
	if r.Sleep != nil {
		r.Sleep(d)
		return nil
	}
	if d <= 0 {
		return ctx.Err()
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

func unwrapPermanent(err error) error {
	var p permanentError
	if errors.As(err, &p) {
		return p.err
	}
	return err
}

// shouldRetry decides whether err warrants another attempt. A cancelled or
// deadline-exceeded context, and any Permanent error, are never retried
// whatever RetryOn says. With RetryOn empty, every other error is retried
// (the historical default). With RetryOn set, the error message must match a
// listed condition (substring, or a numeric status code).
func (r Retrier) shouldRetry(ctx context.Context, err error) bool {
	if err == nil {
		return false
	}
	if IsPermanent(err) {
		return false
	}
	if ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	if len(r.RetryOn) == 0 {
		return true
	}
	message := strings.ToLower(err.Error())
	for _, condition := range r.RetryOn {
		condition = strings.ToLower(strings.TrimSpace(condition))
		if condition == "" {
			continue
		}
		if message == condition || strings.Contains(message, condition) {
			return true
		}
		if code, parseErr := strconv.Atoi(condition); parseErr == nil && strings.Contains(message, strconv.Itoa(code)) {
			return true
		}
	}
	return false
}
