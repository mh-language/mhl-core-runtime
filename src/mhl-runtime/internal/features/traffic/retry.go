// Package traffic contains agent request resilience controls.
package traffic

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Result is the value returned by an attempted request together with its
// attempt count.
type Result struct {
	Value    any
	Attempts int
}

// Retrier retries configured transient failures with exponential backoff.
type Retrier struct {
	MaxAttempts int
	Delay       time.Duration
	RetryOn     []string
	Sleep       func(time.Duration)
}

// Execute invokes fn until it succeeds or the retry policy is exhausted.
func (r Retrier) Execute(fn func() (Result, error)) (Result, error) {
	if fn == nil {
		return Result{}, fmt.Errorf("traffic: retry function is nil")
	}
	max := r.MaxAttempts
	if max <= 0 {
		max = 1
	}
	delay := r.Delay
	if delay <= 0 {
		delay = time.Second
	}
	sleep := r.Sleep
	if sleep == nil {
		sleep = time.Sleep
	}
	var last Result
	var err error
	for attempt := 1; attempt <= max; attempt++ {
		last, err = fn()
		last.Attempts = attempt
		if err == nil {
			return last, nil
		}
		if attempt == max || !r.shouldRetry(err) {
			return last, err
		}
		sleep(delay << (attempt - 1))
	}
	return last, err
}

func (r Retrier) shouldRetry(err error) bool {
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
