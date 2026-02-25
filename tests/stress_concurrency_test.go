//go:build stress
// +build stress

package tests

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"wget/internal/concurrency"
	"wget/internal/downloader"
)

func TestStress_ConcurrencyManagerHighLoad(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	fake := &stressDownloader{delay: 1 * time.Millisecond}
	mgr, err := concurrency.NewManager(fake, concurrency.Config{Workers: 50})
	if err != nil {
		t.Fatalf("NewManager returned error: %v", err)
	}

	jobs := makeStressJobs(2000)
	summary, runErr := mgr.Run(ctx, jobs)
	if runErr != nil {
		t.Fatalf("Run returned error: %v", runErr)
	}
	if summary.Succeeded != len(jobs) || summary.Failed != 0 {
		t.Fatalf("unexpected summary: %#v", summary)
	}
	if fake.maxActive.Load() < 20 {
		t.Fatalf("expected substantial parallelism, maxActive=%d", fake.maxActive.Load())
	}
}

func TestStress_ConcurrencyManagerContinuesWhenSomeFail(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	fake := &stressDownloader{
		delay:     500 * time.Microsecond,
		failEvery: 25,
	}
	mgr, err := concurrency.NewManager(fake, concurrency.Config{
		Workers:        50,
		CaptureResults: true,
	})
	if err != nil {
		t.Fatalf("NewManager returned error: %v", err)
	}

	jobs := makeStressJobs(2500)
	summary, runErr := mgr.Run(ctx, jobs)
	if runErr == nil {
		t.Fatal("expected aggregate error")
	}

	var aggErr *concurrency.AggregateError
	if !errors.As(runErr, &aggErr) {
		t.Fatalf("expected AggregateError, got %T", runErr)
	}

	expectedFailures := len(jobs) / fake.failEvery
	if summary.Failed != expectedFailures {
		t.Fatalf("failed count mismatch: got %d want %d", summary.Failed, expectedFailures)
	}
	if summary.Succeeded+summary.Failed != len(jobs) {
		t.Fatalf("expected all jobs processed: %#v", summary)
	}
	if len(summary.Results) != len(jobs) {
		t.Fatalf("expected captured results for all jobs, got %d", len(summary.Results))
	}
}

type stressDownloader struct {
	delay     time.Duration
	failEvery int

	active    atomic.Int64
	maxActive atomic.Int64
}

func (s *stressDownloader) Download(ctx context.Context, req downloader.Request) (downloader.Result, error) {
	cur := s.active.Add(1)
	updateStressMax(&s.maxActive, cur)
	defer s.active.Add(-1)

	if s.delay > 0 {
		select {
		case <-ctx.Done():
			return downloader.Result{}, ctx.Err()
		case <-time.After(s.delay):
		}
	}

	if s.failEvery > 0 {
		var id int
		_, _ = fmt.Sscanf(filepath.Base(req.OutputPath), "job-%d.bin", &id)
		if id > 0 && id%s.failEvery == 0 {
			return downloader.Result{}, errors.New("simulated stress failure")
		}
	}

	return downloader.Result{BytesWritten: 1024}, nil
}

func makeStressJobs(count int) []concurrency.Job {
	jobs := make([]concurrency.Job, 0, count)
	for i := 1; i <= count; i++ {
		jobs = append(jobs, concurrency.Job{
			ID: i,
			Request: downloader.Request{
				URL:        "https://example.com/file.bin",
				OutputPath: fmt.Sprintf("job-%d.bin", i),
			},
		})
	}
	return jobs
}

func updateStressMax(target *atomic.Int64, value int64) {
	for {
		prev := target.Load()
		if value <= prev {
			return
		}
		if target.CompareAndSwap(prev, value) {
			return
		}
	}
}
