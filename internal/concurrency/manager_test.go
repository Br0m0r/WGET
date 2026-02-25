package concurrency

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	dl "wget/internal/downloader"
	"wget/internal/errcode"
)

func TestManager_MultipleSimultaneousDownloads(t *testing.T) {
	d := &fakeDownloader{delay: 60 * time.Millisecond}
	mgr, err := NewManager(d, Config{Workers: 3})
	if err != nil {
		t.Fatalf("NewManager returned error: %v", err)
	}

	jobs := makeJobs(9)
	start := time.Now()
	summary, err := mgr.Run(context.Background(), jobs)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if summary.Total != 9 || summary.Succeeded != 9 || summary.Failed != 0 {
		t.Fatalf("unexpected summary: %#v", summary)
	}
	if d.maxActive.Load() < 2 {
		t.Fatalf("expected parallel execution, max active=%d", d.maxActive.Load())
	}
	serialTime := 9 * 60 * time.Millisecond
	if elapsed >= serialTime {
		t.Fatalf("expected parallel speedup, elapsed=%s serial=%s", elapsed, serialTime)
	}
}

func TestManager_AggregatesErrorsAndContinues(t *testing.T) {
	d := &fakeDownloader{
		delay:      10 * time.Millisecond,
		failJobIDs: map[int]bool{2: true, 5: true},
	}
	mgr, err := NewManager(d, Config{Workers: 4})
	if err != nil {
		t.Fatalf("NewManager returned error: %v", err)
	}

	jobs := makeJobs(8)
	summary, runErr := mgr.Run(context.Background(), jobs)
	if runErr == nil {
		t.Fatal("expected aggregate error")
	}
	var aggErr *AggregateError
	if !errors.As(runErr, &aggErr) {
		t.Fatalf("expected AggregateError, got %T", runErr)
	}
	if len(aggErr.Failures) != 2 {
		t.Fatalf("expected 2 failures, got %d", len(aggErr.Failures))
	}
	for _, f := range aggErr.Failures {
		if f.ErrorCode == "" {
			t.Fatalf("expected failure error code to be populated, got %#v", f)
		}
	}

	if summary.Total != 8 || summary.Succeeded != 6 || summary.Failed != 2 {
		t.Fatalf("unexpected summary counts: %#v", summary)
	}
	if d.calls.Load() != 8 {
		t.Fatalf("expected all jobs attempted, got %d", d.calls.Load())
	}
}

func TestManager_AggregatesDownloadErrorMetadata(t *testing.T) {
	d := &fakeDownloader{
		failErrByID: map[int]error{
			2: &dl.DownloadError{
				Code: errcode.CodeNetworkTime,
				URL:  "https://example.com/file",
				Attempts: []dl.AttemptMetadata{
					{
						Attempt:      1,
						StatusCode:   503,
						Backoff:      500 * time.Millisecond,
						AttemptBytes: 128,
						PartialBytes: 128,
						Retryable:    true,
						ErrorCode:    errcode.CodeHTTP5XX,
						Error:        "unexpected HTTP status 503",
					},
				},
				RetriesExhausted: true,
				Cause:            errors.New("request timeout"),
			},
		},
	}
	mgr, err := NewManager(d, Config{Workers: 2, CaptureResults: true})
	if err != nil {
		t.Fatalf("NewManager returned error: %v", err)
	}

	jobs := makeJobs(3)
	summary, runErr := mgr.Run(context.Background(), jobs)
	if runErr == nil {
		t.Fatal("expected aggregate error")
	}
	if summary.Failed != 1 {
		t.Fatalf("expected one failure, got %#v", summary)
	}
	if len(summary.Results) != 3 {
		t.Fatalf("expected all results captured, got %d", len(summary.Results))
	}
	var failed JobResult
	found := false
	for _, r := range summary.Results {
		if r.Err != nil {
			failed = r
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected a failed result entry")
	}
	if failed.ErrorCode != errcode.CodeNetworkTime {
		t.Fatalf("unexpected error code: got %s want %s", failed.ErrorCode, errcode.CodeNetworkTime)
	}
	if len(failed.Attempts) != 1 {
		t.Fatalf("expected attempts to be propagated, got %#v", failed.Attempts)
	}
}

func TestManager_RaceSafetyHighConcurrency(t *testing.T) {
	d := &fakeDownloader{delay: 1 * time.Millisecond}
	mgr, err := NewManager(d, Config{Workers: 50})
	if err != nil {
		t.Fatalf("NewManager returned error: %v", err)
	}

	jobs := makeJobs(500)
	summary, runErr := mgr.Run(context.Background(), jobs)
	if runErr != nil {
		t.Fatalf("Run returned error: %v", runErr)
	}
	if summary.Succeeded != 500 || summary.Failed != 0 {
		t.Fatalf("unexpected summary: %#v", summary)
	}
}

func TestNewManager_Validation(t *testing.T) {
	_, err := NewManager(nil, Config{Workers: 1})
	if err == nil {
		t.Fatal("expected nil downloader validation error")
	}

	_, err = NewManager(&fakeDownloader{}, Config{Workers: 51})
	if err == nil {
		t.Fatal("expected workers upper bound error")
	}
}

type fakeDownloader struct {
	delay       time.Duration
	failJobIDs  map[int]bool
	failErrByID map[int]error

	calls     atomic.Int64
	active    atomic.Int64
	maxActive atomic.Int64
	mu        sync.Mutex
}

func (f *fakeDownloader) Download(ctx context.Context, req dl.Request) (dl.Result, error) {
	f.calls.Add(1)
	cur := f.active.Add(1)
	updateMax(&f.maxActive, cur)
	defer f.active.Add(-1)

	if f.delay > 0 {
		select {
		case <-ctx.Done():
			return dl.Result{}, ctx.Err()
		case <-time.After(f.delay):
		}
	}

	var id int
	if _, err := fmt.Sscanf(req.OutputPath, "out-%d", &id); err != nil {
		id = -1
	}
	if f.failJobIDs != nil && f.failJobIDs[id] {
		return dl.Result{}, errors.New("simulated download failure")
	}
	if f.failErrByID != nil {
		if err, ok := f.failErrByID[id]; ok {
			return dl.Result{}, err
		}
	}

	return dl.Result{BytesWritten: 1024}, nil
}

func updateMax(target *atomic.Int64, value int64) {
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

func makeJobs(n int) []Job {
	out := make([]Job, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, Job{
			ID: i,
			Request: dl.Request{
				URL:        "https://example.com/file",
				OutputPath: fmt.Sprintf("out-%d", i),
			},
		})
	}
	return out
}
