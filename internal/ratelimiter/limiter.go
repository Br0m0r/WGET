package ratelimiter

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"time"
)

type sleepFunc func(context.Context, time.Duration) error

// Config configures token-bucket behavior.
type Config struct {
	BytesPerSec int64
	BurstBytes  int64
	Now         func() time.Time
	Sleep       sleepFunc
}

// Limiter implements a token-bucket rate limiter.
type Limiter struct {
	mu         sync.Mutex
	rate       float64
	burst      float64
	tokens     float64
	lastRefill time.Time
	now        func() time.Time
	sleep      sleepFunc
	unlimited  bool
}

// New constructs a limiter. Burst defaults to one second worth of tokens.
func New(cfg Config) (*Limiter, error) {
	if cfg.BytesPerSec < 0 {
		return nil, fmt.Errorf("bytes per second cannot be negative")
	}

	nowFn := cfg.Now
	if nowFn == nil {
		nowFn = time.Now
	}

	sleepFn := cfg.Sleep
	if sleepFn == nil {
		sleepFn = sleepWithContext
	}

	if cfg.BytesPerSec == 0 {
		return &Limiter{
			now:       nowFn,
			sleep:     sleepFn,
			unlimited: true,
		}, nil
	}

	burst := cfg.BurstBytes
	if burst <= 0 {
		burst = cfg.BytesPerSec
	}

	now := nowFn()
	return &Limiter{
		rate:       float64(cfg.BytesPerSec),
		burst:      float64(burst),
		tokens:     float64(burst),
		lastRefill: now,
		now:        nowFn,
		sleep:      sleepFn,
	}, nil
}

// WaitN blocks until n bytes are permitted by the limiter.
func (l *Limiter) WaitN(ctx context.Context, n int) error {
	if n <= 0 || l.unlimited {
		return nil
	}
	if ctx == nil {
		return errors.New("context is required")
	}

	remaining := float64(n)
	for remaining > 0 {
		chunk := math.Min(remaining, l.burst)
		if err := l.waitChunk(ctx, chunk); err != nil {
			return err
		}
		remaining -= chunk
	}
	return nil
}

func (l *Limiter) waitChunk(ctx context.Context, chunk float64) error {
	for {
		l.mu.Lock()
		now := l.now()
		l.refillLocked(now)

		if l.tokens >= chunk {
			l.tokens -= chunk
			l.mu.Unlock()
			return nil
		}

		deficit := chunk - l.tokens
		waitSeconds := deficit / l.rate
		wait := time.Duration(waitSeconds * float64(time.Second))
		if wait < time.Microsecond {
			wait = time.Microsecond
		}
		l.mu.Unlock()

		if err := l.sleep(ctx, wait); err != nil {
			return err
		}
	}
}

func (l *Limiter) refillLocked(now time.Time) {
	if now.Before(l.lastRefill) {
		return
	}

	elapsed := now.Sub(l.lastRefill).Seconds()
	if elapsed <= 0 {
		return
	}

	l.tokens += elapsed * l.rate
	if l.tokens > l.burst {
		l.tokens = l.burst
	}
	l.lastRefill = now
}

func sleepWithContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
