package traffic_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/yanjustino/mhl-runtime/internal/traffic"
)

func TestLimiterQueuesAndCapsConcurrency(t *testing.T) {
	l := &traffic.Limiter{Concurrency: 2, OnExceeded: "queue"}
	var mu sync.Mutex
	active, maxActive, completed := 0, 0, 0
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := l.Acquire(context.Background()); err != nil {
				t.Error(err)
				return
			}
			defer l.Release()
			mu.Lock()
			active++
			if active > maxActive {
				maxActive = active
			}
			mu.Unlock()
			time.Sleep(time.Millisecond)
			mu.Lock()
			active--
			completed++
			mu.Unlock()
		}()
	}
	wg.Wait()
	if maxActive > 2 || completed != 8 {
		t.Fatalf("max concurrency=%d completed=%d", maxActive, completed)
	}
}
