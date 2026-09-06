package traffic_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mh-language/mhl-core-runtime/internal/features/traffic"
)

func TestRetrierRetriesTransientErrorsWithBackoff(t *testing.T) {
	var calls int
	var waits []time.Duration
	r := traffic.Retrier{
		MaxAttempts: 3,
		Delay:       time.Millisecond,
		RetryOn:     []string{"500"},
		Sleep:       func(d time.Duration) { waits = append(waits, d) },
		Rand:        func() float64 { return 1 }, // no jitter shrink, so waits are the full backoff
	}
	result, err := r.Execute(context.Background(), func() (traffic.Result, error) {
		calls++
		if calls < 3 {
			return traffic.Result{}, errors.New("HTTP 500")
		}
		return traffic.Result{Value: "ok"}, nil
	})
	if err != nil || result.Value != "ok" || calls != 3 || len(waits) != 2 || waits[1] != 2*time.Millisecond {
		t.Fatalf("result=%+v err=%v calls=%d waits=%v", result, err, calls, waits)
	}
}

func TestRetrierDoesNotRetryUnconfiguredError(t *testing.T) {
	calls := 0
	_, err := (traffic.Retrier{MaxAttempts: 3, RetryOn: []string{"timeout"}}).
		Execute(context.Background(), func() (traffic.Result, error) {
			calls++
			return traffic.Result{}, errors.New("bad request")
		})
	if err == nil || calls != 1 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
}

// A Permanent error is returned as-is (unwrapped) without a second attempt,
// even when RetryOn is empty (retry-everything).
func TestRetrierNeverRetriesPermanent(t *testing.T) {
	calls := 0
	sentinel := errors.New("400 invalid schema")
	_, err := (traffic.Retrier{MaxAttempts: 5}).
		Execute(context.Background(), func() (traffic.Result, error) {
			calls++
			return traffic.Result{}, traffic.Permanent(sentinel)
		})
	if calls != 1 {
		t.Fatalf("permanent error retried: calls=%d", calls)
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("want the original error unwrapped, got %v", err)
	}
	if traffic.IsPermanent(err) {
		t.Fatalf("Execute should unwrap the Permanent marker before returning")
	}
}

// A context cancelled during the backoff wait aborts the retry loop instead
// of sleeping on; the caller gets the last real failure, not the cancel.
func TestRetrierBackoffIsCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	start := time.Now()
	_, err := (traffic.Retrier{MaxAttempts: 4, Delay: time.Hour}).
		Execute(ctx, func() (traffic.Result, error) {
			calls++
			if calls == 1 {
				go func() { time.Sleep(20 * time.Millisecond); cancel() }()
			}
			return traffic.Result{}, errors.New("HTTP 503")
		})
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("backoff was not cancelled: waited %v", elapsed)
	}
	if calls != 1 {
		t.Fatalf("expected the loop to stop after the cancel during backoff, calls=%d", calls)
	}
	if err == nil || err.Error() != "HTTP 503" {
		t.Fatalf("want the last real failure surfaced, got %v", err)
	}
}

// An already-cancelled context short-circuits before the first attempt.
func TestRetrierRespectsPreCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls := 0
	_, err := (traffic.Retrier{MaxAttempts: 3}).
		Execute(ctx, func() (traffic.Result, error) { calls++; return traffic.Result{Value: "x"}, nil })
	if calls != 0 || !errors.Is(err, context.Canceled) {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
}

// A deadline-exceeded failure is not retried even under retry-everything.
func TestRetrierDoesNotRetryContextDeadline(t *testing.T) {
	calls := 0
	_, err := (traffic.Retrier{MaxAttempts: 3}).
		Execute(context.Background(), func() (traffic.Result, error) {
			calls++
			return traffic.Result{}, context.DeadlineExceeded
		})
	if calls != 1 || err == nil {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
}

// backoff never exceeds MaxDelay however large the attempt number.
func TestRetrierClampsBackoffToMaxDelay(t *testing.T) {
	var waits []time.Duration
	r := traffic.Retrier{
		MaxAttempts: 8,
		Delay:       time.Second,
		MaxDelay:    4 * time.Second,
		Sleep:       func(d time.Duration) { waits = append(waits, d) },
		Rand:        func() float64 { return 1 },
	}
	_, _ = r.Execute(context.Background(), func() (traffic.Result, error) {
		return traffic.Result{}, errors.New("boom")
	})
	for i, w := range waits {
		if w > 4*time.Second {
			t.Fatalf("wait %d = %v exceeds MaxDelay", i, w)
		}
	}
}

// The MaxDelay cap applies from the very first wait — a Delay larger than
// MaxDelay must not produce one giant sleep before the doubling series kicks
// in. (R7 in PLANO.md.)
func TestRetrierClampsFirstWaitWhenDelayExceedsMaxDelay(t *testing.T) {
	for _, tc := range []struct {
		name            string
		delay, maxDelay time.Duration
	}{
		{"delay far above max", 2 * time.Hour, 30 * time.Second},
		{"delay equal to max", 30 * time.Second, 30 * time.Second},
		{"delay below max", 5 * time.Second, 30 * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var waits []time.Duration
			r := traffic.Retrier{
				MaxAttempts: 5,
				Delay:       tc.delay,
				MaxDelay:    tc.maxDelay,
				Sleep:       func(d time.Duration) { waits = append(waits, d) },
				Rand:        func() float64 { return 1 }, // full backoff, no jitter shrink
			}
			_, _ = r.Execute(context.Background(), func() (traffic.Result, error) {
				return traffic.Result{}, errors.New("boom")
			})
			if len(waits) == 0 {
				t.Fatal("no backoff waits recorded")
			}
			for i, w := range waits {
				if w > tc.maxDelay {
					t.Fatalf("wait %d = %v exceeds MaxDelay %v", i, w, tc.maxDelay)
				}
			}
			// The first wait is capped, not skipped, when Delay >= MaxDelay.
			if tc.delay >= tc.maxDelay && waits[0] != tc.maxDelay {
				t.Fatalf("first wait = %v, want the cap %v", waits[0], tc.maxDelay)
			}
		})
	}
}
