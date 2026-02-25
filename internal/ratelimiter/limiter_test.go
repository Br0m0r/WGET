package ratelimiter

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestLimiter_RateEnforcementAccuracy(t *testing.T) {
	fc := newFakeClock(time.Unix(1000, 0))
	limiter, err := New(Config{
		BytesPerSec: 1024,
		BurstBytes:  1024,
		Now:         fc.Now,
		Sleep:       fc.Sleep,
	})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	start := fc.Now()
	if err := limiter.WaitN(context.Background(), 2048); err != nil {
		t.Fatalf("WaitN returned error: %v", err)
	}
	elapsed := fc.Now().Sub(start)
	if elapsed != time.Second {
		t.Fatalf("expected 1s elapsed for 2048 bytes with 1024 burst/rate, got %s", elapsed)
	}
}

func TestLimiter_BurstHandling(t *testing.T) {
	fc := newFakeClock(time.Unix(2000, 0))
	limiter, err := New(Config{
		BytesPerSec: 1024,
		BurstBytes:  2048,
		Now:         fc.Now,
		Sleep:       fc.Sleep,
	})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	start := fc.Now()
	if err := limiter.WaitN(context.Background(), 2048); err != nil {
		t.Fatalf("WaitN returned error: %v", err)
	}
	if fc.Now().Sub(start) != 0 {
		t.Fatalf("expected first 2048-byte burst to be immediate, got elapsed %s", fc.Now().Sub(start))
	}

	if err := limiter.WaitN(context.Background(), 1024); err != nil {
		t.Fatalf("WaitN returned error: %v", err)
	}
	if fc.Now().Sub(start) != time.Second {
		t.Fatalf("expected total elapsed 1s after additional 1024 bytes, got %s", fc.Now().Sub(start))
	}
}

func TestLimiter_DefaultBurstIsOneSecondWorth(t *testing.T) {
	fc := newFakeClock(time.Unix(3000, 0))
	limiter, err := New(Config{
		BytesPerSec: 4096,
		BurstBytes:  0,
		Now:         fc.Now,
		Sleep:       fc.Sleep,
	})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	start := fc.Now()
	if err := limiter.WaitN(context.Background(), 4096); err != nil {
		t.Fatalf("WaitN returned error: %v", err)
	}
	if fc.Now().Sub(start) != 0 {
		t.Fatalf("expected immediate first second burst, got %s", fc.Now().Sub(start))
	}

	if err := limiter.WaitN(context.Background(), 4096); err != nil {
		t.Fatalf("WaitN returned error: %v", err)
	}
	if fc.Now().Sub(start) != time.Second {
		t.Fatalf("expected 1s elapsed for second burst, got %s", fc.Now().Sub(start))
	}
}

func TestLimiter_ContextCancellation(t *testing.T) {
	fc := newFakeClock(time.Unix(4000, 0))
	limiter, err := New(Config{
		BytesPerSec: 1024,
		BurstBytes:  1,
		Now:         fc.Now,
		Sleep:       fc.Sleep,
	})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = limiter.WaitN(ctx, 1024)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newFakeClock(start time.Time) *fakeClock {
	return &fakeClock{now: start}
}

func (f *fakeClock) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

func (f *fakeClock) Sleep(ctx context.Context, d time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	f.mu.Lock()
	f.now = f.now.Add(d)
	f.mu.Unlock()
	return nil
}
