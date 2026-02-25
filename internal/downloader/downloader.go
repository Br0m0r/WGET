package downloader

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"wget/internal/errcode"
)

const (
	defaultTimeout     = 30 * time.Second
	defaultMaxRetries  = 3
	defaultBackoffBase = 500 * time.Millisecond
	defaultBackoffMax  = 5 * time.Second
)

var errRetryableNetwork = errors.New("retryable network error")

var streamCopyBufferPool = sync.Pool{
	New: func() any {
		return make([]byte, 256*1024)
	},
}

func init() {
	streamCopyBufferPool.Put(make([]byte, 256*1024))
}

// Request describes a single download operation.
type Request struct {
	URL         string
	OutputPath  string
	Timeout     time.Duration
	MaxRetries  int
	BackoffBase time.Duration
	BackoffMax  time.Duration
	Limiter     ByteLimiter
	OnResponse  func(statusCode int, contentSize int64)
	OnProgress  func(downloaded int64, total int64)
}

// ByteLimiter gates byte throughput for download copy loops.
type ByteLimiter interface {
	WaitN(ctx context.Context, n int) error
}

// Result captures completed download metadata.
type Result struct {
	StartTime     time.Time
	EndTime       time.Time
	StatusCode    int
	ContentSize   int64
	SavePath      string
	BytesWritten  int64
	Resumed       bool
	AttemptCount  int
	ResponseURL   string
	RetryStatuses []int
}

// AttemptMetadata captures one failed transfer attempt before terminal failure.
type AttemptMetadata struct {
	Attempt      int
	StatusCode   int
	Backoff      time.Duration
	AttemptBytes int64
	PartialBytes int64
	Retryable    bool
	ErrorCode    string
	Error        string
}

// DownloadError contains terminal failure details for a download operation.
type DownloadError struct {
	Code             string
	URL              string
	OutputPath       string
	Attempts         []AttemptMetadata
	RetriesExhausted bool
	Cause            error
}

func (e *DownloadError) Error() string {
	if e == nil {
		return ""
	}
	if e.RetriesExhausted {
		return fmt.Sprintf("download failed after retries exhausted [code=%s url=%s]: %v", e.Code, e.URL, e.Cause)
	}
	return fmt.Sprintf("download failed [code=%s url=%s]: %v", e.Code, e.URL, e.Cause)
}

func (e *DownloadError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func (e *DownloadError) ErrorCode() string {
	if e == nil {
		return ""
	}
	return e.Code
}

// HTTPStatusError reports non-success HTTP responses.
type HTTPStatusError struct {
	Code int
	URL  string
}

func (e *HTTPStatusError) Error() string {
	return fmt.Sprintf("unexpected HTTP status %d for %s", e.Code, e.URL)
}

func (e *HTTPStatusError) ErrorCode() string {
	if e == nil {
		return ""
	}
	return errcode.HTTPStatus(e.Code)
}

// InvalidURLError reports malformed or unsupported URLs.
type InvalidURLError struct {
	URL string
}

func (e *InvalidURLError) Error() string {
	return fmt.Sprintf("invalid URL %q: only absolute http/https URLs are supported", e.URL)
}

func (e *InvalidURLError) ErrorCode() string {
	return errcode.CodeInvalidURL
}

// Downloader handles streaming HTTP downloads with retries and resume support.
type Downloader struct {
	client *http.Client
	rng    *rand.Rand
	sleep  func(time.Duration)
}

// New creates a Downloader instance.
func New(client *http.Client) *Downloader {
	if client == nil {
		client = &http.Client{Timeout: defaultTimeout}
	}
	return &Downloader{
		client: client,
		rng:    rand.New(rand.NewSource(time.Now().UnixNano())),
		sleep:  time.Sleep,
	}
}

// Download fetches a URL and writes it to OutputPath using a .part temporary file.
func (d *Downloader) Download(ctx context.Context, req Request) (Result, error) {
	start := time.Now()
	if err := validateRequest(req); err != nil {
		res := Result{StartTime: start}
		return finalizeWithError(res, wrapDownloadError(req, res, err, nil, false))
	}

	timeout := req.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}

	maxRetries := req.MaxRetries
	if maxRetries < 0 {
		maxRetries = defaultMaxRetries
	}
	backoffBase := req.BackoffBase
	if backoffBase <= 0 {
		backoffBase = defaultBackoffBase
	}
	backoffMax := req.BackoffMax
	if backoffMax <= 0 {
		backoffMax = defaultBackoffMax
	}

	partPath := req.OutputPath + ".part"
	if err := os.MkdirAll(filepath.Dir(req.OutputPath), 0o755); err != nil {
		res := Result{StartTime: start}
		return finalizeWithError(res, wrapDownloadError(req, res, fmt.Errorf("create output directory: %w", err), nil, false))
	}

	offset, err := partSize(partPath)
	if err != nil {
		res := Result{StartTime: start}
		return finalizeWithError(res, wrapDownloadError(req, res, fmt.Errorf("inspect partial file: %w", err), nil, false))
	}

	res := Result{
		StartTime:    start,
		SavePath:     req.OutputPath,
		Resumed:      offset > 0,
		ResponseURL:  req.URL,
		BytesWritten: offset,
	}

	attempts := make([]AttemptMetadata, 0, maxRetries+1)
	attempt := 0
	for {
		attempt++
		res.AttemptCount = attempt
		if offset > 0 {
			res.Resumed = true
		}

		attemptCtx, cancel := context.WithTimeout(ctx, timeout)
		statusCode, bytesWritten, contentSize, newOffset, retryableErr, err := d.fetchOnce(
			attemptCtx,
			req.URL,
			partPath,
			offset,
			req.Limiter,
			req.OnResponse,
			req.OnProgress,
		)
		cancel()

		if statusCode != 0 {
			res.StatusCode = statusCode
		}
		if bytesWritten > 0 {
			res.BytesWritten += bytesWritten
		}
		if contentSize >= 0 {
			res.ContentSize = contentSize
		}
		offset = newOffset

		if err == nil {
			if err := promotePartFile(partPath, req.OutputPath); err != nil {
				return finalizeWithError(res, wrapDownloadError(req, res, fmt.Errorf("finalize output file: %w", err), nil, false))
			}
			res.EndTime = time.Now()
			return res, nil
		}

		var statusErr *HTTPStatusError
		if errors.As(err, &statusErr) {
			res.RetryStatuses = append(res.RetryStatuses, statusErr.Code)
		}

		retryable := shouldRetry(err) || errors.Is(retryableErr, errRetryableNetwork)
		errCode := classifyAttemptCode(statusCode, err, retryableErr)
		wait := time.Duration(0)
		if retryable && attempt <= maxRetries {
			wait = backoffWithJitter(d.rng, backoffBase, backoffMax, attempt-1)
		}
		attempts = append(attempts, AttemptMetadata{
			Attempt:      attempt,
			StatusCode:   statusCode,
			Backoff:      wait,
			AttemptBytes: bytesWritten,
			PartialBytes: res.BytesWritten,
			Retryable:    retryable,
			ErrorCode:    errCode,
			Error:        err.Error(),
		})
		if !retryable || attempt > maxRetries {
			return finalizeWithError(res, wrapDownloadError(req, res, err, attempts, retryable && attempt > maxRetries))
		}

		if retryableErr != nil && errors.Is(retryableErr, errRetryableNetwork) {
			// Keep the part file for resume if this is a network interruption.
		}
		if wait > 0 {
			d.sleep(wait)
		}
	}
}

func (d *Downloader) fetchOnce(
	ctx context.Context,
	rawURL, partPath string,
	offset int64,
	limiter ByteLimiter,
	onResponse func(statusCode int, contentSize int64),
	onProgress func(downloaded int64, total int64),
) (statusCode int, bytesWritten int64, contentSize int64, newOffset int64, retryableErr error, err error) {
	contentSize = -1
	newOffset = offset

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return 0, 0, -1, offset, nil, &InvalidURLError{URL: rawURL}
	}

	if offset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
	}

	resp, err := d.client.Do(req)
	if err != nil {
		if isRetryableNetworkError(err) {
			return 0, 0, -1, offset, errRetryableNetwork, fmt.Errorf("http request failed: %w", err)
		}
		return 0, 0, -1, offset, nil, fmt.Errorf("http request failed: %w", err)
	}
	defer resp.Body.Close()

	statusCode = resp.StatusCode

	if offset > 0 && resp.StatusCode == http.StatusOK {
		// Server ignored range; restart full download from zero.
		if truncErr := os.Truncate(partPath, 0); truncErr != nil && !errors.Is(truncErr, os.ErrNotExist) {
			return statusCode, 0, -1, 0, nil, fmt.Errorf("restart non-range download: %w", truncErr)
		}
		return d.fetchOnce(ctx, rawURL, partPath, 0, limiter, onResponse, onProgress)
	}

	if !isSuccessStatus(resp.StatusCode) {
		statusErr := &HTTPStatusError{Code: resp.StatusCode, URL: rawURL}
		if isRetryableStatus(resp.StatusCode) {
			return statusCode, 0, -1, offset, nil, statusErr
		}
		return statusCode, 0, -1, offset, nil, statusErr
	}

	if resp.ContentLength >= 0 {
		if resp.StatusCode == http.StatusPartialContent {
			contentSize = offset + resp.ContentLength
		} else {
			contentSize = resp.ContentLength
		}
	}
	if onResponse != nil {
		onResponse(resp.StatusCode, contentSize)
	}

	flags := os.O_CREATE | os.O_WRONLY
	if offset > 0 {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
	}

	file, err := os.OpenFile(partPath, flags, 0o644)
	if err != nil {
		return statusCode, 0, -1, offset, nil, fmt.Errorf("open output file: %w", err)
	}
	defer file.Close()

	buf := streamCopyBufferPool.Get().([]byte)
	written, copyErr := copyWithProgress(file, resp.Body, buf, offset, contentSize, limiter, onProgress, ctx)
	streamCopyBufferPool.Put(buf)
	if copyErr != nil {
		if isRetryableNetworkError(copyErr) || errors.Is(copyErr, io.ErrUnexpectedEOF) || errors.Is(copyErr, io.EOF) {
			return statusCode, written, contentSize, offset + written, errRetryableNetwork, fmt.Errorf("stream interrupted: %w", copyErr)
		}
		return statusCode, written, contentSize, offset + written, nil, fmt.Errorf("stream copy failed: %w", copyErr)
	}

	return statusCode, written, contentSize, offset + written, nil, nil
}

func copyWithProgress(
	dst io.Writer,
	src io.Reader,
	buf []byte,
	initial int64,
	total int64,
	limiter ByteLimiter,
	onProgress func(downloaded int64, total int64),
	ctx context.Context,
) (int64, error) {
	var written int64
	downloaded := initial
	if onProgress != nil {
		onProgress(downloaded, total)
	}

	for {
		nr, er := src.Read(buf)
		if nr > 0 {
			if limiter != nil {
				if waitErr := limiter.WaitN(ctx, nr); waitErr != nil {
					return written, waitErr
				}
			}
			nw, ew := dst.Write(buf[:nr])
			if nw > 0 {
				written += int64(nw)
				downloaded += int64(nw)
				if onProgress != nil {
					onProgress(downloaded, total)
				}
			}
			if ew != nil {
				return written, ew
			}
			if nw != nr {
				return written, io.ErrShortWrite
			}
		}
		if er != nil {
			if errors.Is(er, io.EOF) {
				return written, nil
			}
			return written, er
		}
	}
}

func validateRequest(req Request) error {
	if strings.TrimSpace(req.URL) == "" {
		return &InvalidURLError{URL: req.URL}
	}
	parsed, err := url.Parse(req.URL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return &InvalidURLError{URL: req.URL}
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return &InvalidURLError{URL: req.URL}
	}
	if strings.TrimSpace(req.OutputPath) == "" {
		return errors.New("output path is required")
	}
	return nil
}

func partSize(path string) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, nil
		}
		return 0, err
	}
	return info.Size(), nil
}

func promotePartFile(partPath, targetPath string) error {
	if err := os.Remove(targetPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.Rename(partPath, targetPath)
}

func shouldRetry(err error) bool {
	if err == nil {
		return false
	}

	var statusErr *HTTPStatusError
	if errors.As(err, &statusErr) {
		return isRetryableStatus(statusErr.Code)
	}

	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return true
	}

	if strings.Contains(strings.ToLower(err.Error()), "timeout") {
		return true
	}

	return errors.Is(err, errRetryableNetwork)
}

func isSuccessStatus(code int) bool {
	return code >= 200 && code < 300
}

func isRetryableStatus(code int) bool {
	if code == http.StatusRequestTimeout || code == http.StatusTooManyRequests {
		return true
	}
	return code >= 500 && code <= 599
}

func isRetryableNetworkError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "connection reset") || strings.Contains(msg, "broken pipe")
}

func backoffWithJitter(rng *rand.Rand, base, max time.Duration, retryIndex int) time.Duration {
	backoff := base
	for i := 0; i < retryIndex; i++ {
		if backoff >= max/2 {
			backoff = max
			break
		}
		backoff *= 2
	}
	if backoff > max {
		backoff = max
	}
	if backoff <= 0 {
		return 0
	}
	jitter := time.Duration(rng.Int63n(int64(backoff / 2)))
	return backoff + jitter
}

func finalizeWithError(res Result, err error) (Result, error) {
	res.EndTime = time.Now()
	return res, err
}

func classifyAttemptCode(statusCode int, err error, retryableErr error) string {
	if statusCode >= 400 {
		return errcode.HTTPStatus(statusCode)
	}
	code := errcode.Of(err)
	if code != errcode.CodeUnknown {
		return code
	}
	if errors.Is(retryableErr, errRetryableNetwork) {
		if errcode.Of(retryableErr) == errcode.CodeNetworkTime {
			return errcode.CodeNetworkTime
		}
		return errcode.CodeNetworkError
	}
	return errcode.CodeUnknown
}

func wrapDownloadError(req Request, res Result, cause error, attempts []AttemptMetadata, retriesExhausted bool) error {
	code := errcode.Of(cause)
	if code == errcode.CodeUnknown {
		code = classifyAttemptCode(res.StatusCode, cause, nil)
	}
	return &DownloadError{
		Code:             code,
		URL:              req.URL,
		OutputPath:       req.OutputPath,
		Attempts:         append([]AttemptMetadata(nil), attempts...),
		RetriesExhausted: retriesExhausted,
		Cause:            cause,
	}
}
