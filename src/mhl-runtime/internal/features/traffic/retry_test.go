package traffic_test

import (
	"errors"
	"testing"
	"time"

	"github.com/mh-language/mhl-core-runtime/internal/features/traffic"
)

func TestRetrierRetriesTransientErrorsWithBackoff(t *testing.T) {
	var calls int
	var waits []time.Duration
	r := traffic.Retrier{MaxAttempts: 3, Delay: time.Millisecond, RetryOn: []string{"500"}, Sleep: func(d time.Duration) { waits = append(waits, d) }}
	result, err := r.Execute(func() (traffic.Result, error) {
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
	_, err := (traffic.Retrier{MaxAttempts: 3, RetryOn: []string{"timeout"}}).Execute(func() (traffic.Result, error) { calls++; return traffic.Result{}, errors.New("bad request") })
	if err == nil || calls != 1 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
}
