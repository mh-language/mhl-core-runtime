package traffic

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Limiter queues callers behind a concurrency cap and a requests-per-minute
// token window. Waiting is cancellable through the supplied context.
type Limiter struct {
	RequestsPerMinute int
	Concurrency       int
	OnExceeded        string

	mu          sync.Mutex
	windowStart time.Time
	used        int
	semaphore   chan struct{}
}

// Acquire reserves capacity for one request. Release must be called exactly
// once after the request finishes.
func (l *Limiter) Acquire(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if l == nil {
		return fmt.Errorf("traffic: nil limiter")
	}
	if l.Concurrency > 0 {
		l.mu.Lock()
		if l.semaphore == nil {
			l.semaphore = make(chan struct{}, l.Concurrency)
		}
		sem := l.semaphore
		l.mu.Unlock()
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if l.RequestsPerMinute > 0 {
		for {
			l.mu.Lock()
			now := time.Now()
			if l.windowStart.IsZero() || now.Sub(l.windowStart) >= time.Minute {
				l.windowStart, l.used = now, 0
			}
			if l.used < l.RequestsPerMinute {
				l.used++
				l.mu.Unlock()
				return nil
			}
			wait := time.Until(l.windowStart.Add(time.Minute))
			l.mu.Unlock()
			timer := time.NewTimer(wait)
			select {
			case <-timer.C:
			case <-ctx.Done():
				if !timer.Stop() {
					<-timer.C
				}
				l.Release()
				return ctx.Err()
			}
		}
	}
	return nil
}

// Release returns a concurrency slot. It is safe for limiters without a cap.
func (l *Limiter) Release() {
	if l == nil {
		return
	}
	l.mu.Lock()
	sem := l.semaphore
	l.mu.Unlock()
	if sem != nil {
		<-sem
	}
}
