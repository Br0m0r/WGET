package concurrency

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	dl "wget/internal/downloader"
	"wget/internal/errcode"
)

const (
	defaultWorkers = 10
	maxWorkers     = 50
)

// Downloader is the download engine dependency used by the manager.
type Downloader interface {
	Download(ctx context.Context, req dl.Request) (dl.Result, error)
}

// Config controls worker-pool behavior.
type Config struct {
	Workers        int
	CaptureResults bool
}

// Job is one queued download request.
type Job struct {
	ID      int
	Request dl.Request
}

// JobResult captures the outcome of one job.
type JobResult struct {
	Job       Job
	Result    dl.Result
	Err       error
	ErrorCode string
	Attempts  []dl.AttemptMetadata
	StartedAt time.Time
	EndedAt   time.Time
}

// Summary is the aggregate outcome for a Run.
type Summary struct {
	Total        int
	Succeeded    int
	Failed       int
	BytesWritten int64
	StartedAt    time.Time
	EndedAt      time.Time
	Results      []JobResult
}

// AggregateError contains all failed jobs.
type AggregateError struct {
	Failures []JobResult
}

func (e *AggregateError) Error() string {
	if len(e.Failures) == 0 {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d download(s) failed:", len(e.Failures))
	for _, f := range e.Failures {
		fmt.Fprintf(&b, " [job=%d url=%s code=%s err=%v]", f.Job.ID, f.Job.Request.URL, f.ErrorCode, f.Err)
	}
	return b.String()
}

// Manager executes download jobs with bounded concurrency.
type Manager struct {
	downloader     Downloader
	workers        int
	captureResults bool
}

// NewManager constructs a worker-pool manager.
func NewManager(downloader Downloader, cfg Config) (*Manager, error) {
	if downloader == nil {
		return nil, errors.New("downloader is required")
	}

	workers := cfg.Workers
	if workers <= 0 {
		workers = defaultWorkers
	}
	if workers > maxWorkers {
		return nil, fmt.Errorf("workers cannot exceed %d", maxWorkers)
	}

	return &Manager{
		downloader:     downloader,
		workers:        workers,
		captureResults: cfg.CaptureResults,
	}, nil
}

// Run processes all jobs and aggregates failures.
// It continues through individual job failures and returns an AggregateError at the end if any failed.
func (m *Manager) Run(ctx context.Context, jobs []Job) (Summary, error) {
	start := time.Now()
	summary := Summary{
		Total:     len(jobs),
		StartedAt: start,
		Results:   make([]JobResult, 0, minInt(len(jobs), m.workers*2)),
	}
	if len(jobs) == 0 {
		summary.EndedAt = time.Now()
		return summary, nil
	}

	jobCh := make(chan Job)
	resultCh := make(chan JobResult, minInt(len(jobs), m.workers*2))

	var wg sync.WaitGroup
	for i := 0; i < m.workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobCh {
				started := time.Now()
				res, err := m.downloader.Download(ctx, job.Request)
				code := ""
				attempts := []dl.AttemptMetadata(nil)
				if err != nil {
					code = errcode.Of(err)
					var dlErr *dl.DownloadError
					if errors.As(err, &dlErr) {
						attempts = append(attempts, dlErr.Attempts...)
					}
				}
				resultCh <- JobResult{
					Job:       job,
					Result:    res,
					Err:       err,
					ErrorCode: code,
					Attempts:  attempts,
					StartedAt: started,
					EndedAt:   time.Now(),
				}
			}
		}()
	}

	go func() {
		defer close(jobCh)
		for _, job := range jobs {
			select {
			case <-ctx.Done():
				return
			case jobCh <- job:
			}
		}
	}()

	go func() {
		wg.Wait()
		close(resultCh)
	}()

	failures := make([]JobResult, 0)
	for r := range resultCh {
		if m.captureResults || r.Err != nil {
			summary.Results = append(summary.Results, r)
		}
		summary.BytesWritten += r.Result.BytesWritten
		if r.Err != nil {
			summary.Failed++
			failures = append(failures, r)
			continue
		}
		summary.Succeeded++
	}
	summary.EndedAt = time.Now()

	if len(failures) > 0 {
		return summary, &AggregateError{Failures: failures}
	}
	if err := ctx.Err(); err != nil {
		return summary, err
	}
	return summary, nil
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
