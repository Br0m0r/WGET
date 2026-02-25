package concurrency

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func BenchmarkManager_ConcurrencyEfficiency(b *testing.B) {
	for _, workers := range []int{1, 5, 10, 25, 50} {
		workers := workers
		b.Run(fmt.Sprintf("workers_%d", workers), func(b *testing.B) {
			d := &fakeDownloader{delay: 2 * time.Millisecond}
			mgr, err := NewManager(d, Config{Workers: workers})
			if err != nil {
				b.Fatalf("NewManager returned error: %v", err)
			}
			jobs := makeJobs(100)
			b.SetBytes(int64(len(jobs) * 1024))

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, err := mgr.Run(context.Background(), jobs)
				if err != nil {
					b.Fatalf("Run returned error: %v", err)
				}
			}
		})
	}
}
