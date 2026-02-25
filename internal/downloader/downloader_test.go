package downloader

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"wget/internal/errcode"
	"wget/internal/httpclient"
)

func TestDownload_SuccessfulDownload(t *testing.T) {
	payload := []byte("hello-world-download")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(payload)
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	target := filepath.Join(tmpDir, "file.bin")

	d := New(server.Client())
	res, err := d.Download(context.Background(), Request{
		URL:         server.URL + "/file.bin",
		OutputPath:  target,
		Timeout:     2 * time.Second,
		MaxRetries:  3,
		BackoffBase: 1 * time.Millisecond,
		BackoffMax:  5 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Download returned error: %v", err)
	}

	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("failed reading output: %v", err)
	}
	if string(got) != string(payload) {
		t.Fatalf("downloaded content mismatch: got %q want %q", string(got), string(payload))
	}
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", res.StatusCode)
	}
	if res.BytesWritten != int64(len(payload)) {
		t.Fatalf("expected bytes written %d, got %d", len(payload), res.BytesWritten)
	}
	if _, err := os.Stat(target + ".part"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected .part file to be removed, stat err=%v", err)
	}
}

func TestDownload_404Handling(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	target := filepath.Join(t.TempDir(), "missing.bin")
	d := New(server.Client())
	_, err := d.Download(context.Background(), Request{
		URL:         server.URL + "/missing.bin",
		OutputPath:  target,
		Timeout:     2 * time.Second,
		MaxRetries:  3,
		BackoffBase: 1 * time.Millisecond,
		BackoffMax:  5 * time.Millisecond,
	})
	if err == nil {
		t.Fatal("expected error for 404 status")
	}

	var statusErr *HTTPStatusError
	if !errors.As(err, &statusErr) {
		t.Fatalf("expected HTTPStatusError, got %T: %v", err, err)
	}
	if statusErr.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", statusErr.Code)
	}
}

func TestDownload_InvalidURL(t *testing.T) {
	d := New(httpclient.New(httpclient.Config{Timeout: 2 * time.Second}))
	_, err := d.Download(context.Background(), Request{
		URL:        "://bad-url",
		OutputPath: filepath.Join(t.TempDir(), "bad.bin"),
	})
	if err == nil {
		t.Fatal("expected invalid URL error")
	}

	var invalidErr *InvalidURLError
	if !errors.As(err, &invalidErr) {
		t.Fatalf("expected InvalidURLError, got %T: %v", err, err)
	}
}

func TestDownload_NetworkInterruptionResume(t *testing.T) {
	payload := []byte(strings.Repeat("abcdef0123456789", 4096))
	cut := len(payload) / 3
	var requestCount int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)

		rangeHeader := r.Header.Get("Range")
		if rangeHeader != "" {
			start := parseRangeStart(t, rangeHeader)
			if start < 0 || start >= len(payload) {
				w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
				return
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Header().Set("Accept-Ranges", "bytes")
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, len(payload)-1, len(payload)))
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write(payload[start:])
			return
		}

		// First response simulates an interrupted network stream after sending partial bytes.
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Fatalf("response writer does not support hijacking")
		}
		conn, rw, err := hj.Hijack()
		if err != nil {
			t.Fatalf("hijack failed: %v", err)
		}
		defer conn.Close()

		_, _ = rw.WriteString("HTTP/1.1 200 OK\r\n")
		_, _ = rw.WriteString("Content-Type: application/octet-stream\r\n")
		_, _ = rw.WriteString(fmt.Sprintf("Content-Length: %d\r\n", len(payload)))
		_, _ = rw.WriteString("Connection: close\r\n")
		_, _ = rw.WriteString("\r\n")
		_, _ = rw.Write(payload[:cut])
		_ = rw.Flush()
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	target := filepath.Join(tmpDir, "resume.bin")
	d := New(server.Client())
	d.sleep = func(time.Duration) {}

	res, err := d.Download(context.Background(), Request{
		URL:         server.URL + "/resume.bin",
		OutputPath:  target,
		Timeout:     2 * time.Second,
		MaxRetries:  3,
		BackoffBase: 1 * time.Millisecond,
		BackoffMax:  1 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Download returned error: %v", err)
	}
	if atomic.LoadInt32(&requestCount) < 2 {
		t.Fatalf("expected at least 2 requests due to retry, got %d", requestCount)
	}
	if !res.Resumed {
		t.Fatal("expected Resumed=true after interrupted transfer")
	}

	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("failed reading output: %v", err)
	}
	if len(got) != len(payload) || string(got) != string(payload) {
		t.Fatalf("resumed content mismatch: got %d bytes want %d", len(got), len(payload))
	}
}

func parseRangeStart(t *testing.T, header string) int {
	t.Helper()
	if !strings.HasPrefix(header, "bytes=") || !strings.HasSuffix(header, "-") {
		t.Fatalf("unexpected Range header format: %q", header)
	}
	raw := strings.TrimSuffix(strings.TrimPrefix(header, "bytes="), "-")
	n, err := strconv.Atoi(raw)
	if err != nil {
		t.Fatalf("invalid range start %q: %v", raw, err)
	}
	return n
}

func TestDownload_TimeoutHandling(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "slow")
	}))
	defer server.Close()

	target := filepath.Join(t.TempDir(), "slow.bin")
	d := New(server.Client())
	d.sleep = func(time.Duration) {}

	_, err := d.Download(context.Background(), Request{
		URL:         server.URL + "/slow.bin",
		OutputPath:  target,
		Timeout:     20 * time.Millisecond,
		MaxRetries:  0,
		BackoffBase: 1 * time.Millisecond,
		BackoffMax:  1 * time.Millisecond,
	})
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "timeout") && !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected timeout-related error, got %v", err)
	}
}

func TestDownload_RetryExhaustionIncludesAttemptMetadata(t *testing.T) {
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	target := filepath.Join(t.TempDir(), "retry.bin")
	d := New(server.Client())
	d.sleep = func(time.Duration) {}

	_, err := d.Download(context.Background(), Request{
		URL:         server.URL + "/retry.bin",
		OutputPath:  target,
		Timeout:     500 * time.Millisecond,
		MaxRetries:  2,
		BackoffBase: 1 * time.Millisecond,
		BackoffMax:  2 * time.Millisecond,
	})
	if err == nil {
		t.Fatal("expected retry-exhaustion error")
	}

	var dErr *DownloadError
	if !errors.As(err, &dErr) {
		t.Fatalf("expected DownloadError, got %T: %v", err, err)
	}
	if !dErr.RetriesExhausted {
		t.Fatal("expected RetriesExhausted=true")
	}
	if dErr.Code != errcode.CodeHTTP5XX {
		t.Fatalf("unexpected terminal code: got %s want %s", dErr.Code, errcode.CodeHTTP5XX)
	}
	if len(dErr.Attempts) != 3 {
		t.Fatalf("expected 3 attempts, got %d", len(dErr.Attempts))
	}
	for i, attempt := range dErr.Attempts {
		if attempt.Attempt != i+1 {
			t.Fatalf("attempt numbering mismatch at index %d: %#v", i, attempt)
		}
		if attempt.ErrorCode != errcode.CodeHTTP5XX {
			t.Fatalf("attempt code mismatch at index %d: %#v", i, attempt)
		}
		if attempt.StatusCode != http.StatusServiceUnavailable {
			t.Fatalf("attempt status mismatch at index %d: %#v", i, attempt)
		}
	}
	if atomic.LoadInt32(&calls) != 3 {
		t.Fatalf("expected exactly 3 total calls, got %d", calls)
	}
}

func TestDownload_RateLimiterApplied(t *testing.T) {
	payload := []byte(strings.Repeat("x", 512*1024))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(payload)
	}))
	defer server.Close()

	limiter := &countingLimiter{}
	target := filepath.Join(t.TempDir(), "limited.bin")
	d := New(server.Client())

	_, err := d.Download(context.Background(), Request{
		URL:         server.URL + "/limited.bin",
		OutputPath:  target,
		Timeout:     2 * time.Second,
		MaxRetries:  0,
		BackoffBase: 1 * time.Millisecond,
		BackoffMax:  1 * time.Millisecond,
		Limiter:     limiter,
	})
	if err != nil {
		t.Fatalf("Download returned error: %v", err)
	}
	if limiter.calls == 0 {
		t.Fatal("expected limiter WaitN to be called")
	}
	if limiter.totalBytes < len(payload) {
		t.Fatalf("expected limiter byte accounting >= payload bytes, got %d want >= %d", limiter.totalBytes, len(payload))
	}
}

type countingLimiter struct {
	calls      int
	totalBytes int
}

func (l *countingLimiter) WaitN(_ context.Context, n int) error {
	l.calls++
	l.totalBytes += n
	return nil
}
