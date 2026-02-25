package ratelimiter

import (
	"context"
	"testing"
)

func BenchmarkLimiterWaitN_CPUOverhead(b *testing.B) {
	limiter, err := New(Config{
		BytesPerSec: 1 << 30,
		BurstBytes:  1 << 30,
	})
	if err != nil {
		b.Fatalf("New returned error: %v", err)
	}

	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := limiter.WaitN(ctx, 1024); err != nil {
			b.Fatalf("WaitN returned error: %v", err)
		}
	}
}

func BenchmarkLimiterWaitN_MemoryOverhead(b *testing.B) {
	limiter, err := New(Config{
		BytesPerSec: 1 << 30,
		BurstBytes:  1 << 30,
	})
	if err != nil {
		b.Fatalf("New returned error: %v", err)
	}

	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := limiter.WaitN(ctx, 1); err != nil {
			b.Fatalf("WaitN returned error: %v", err)
		}
	}
}
