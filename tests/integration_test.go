package tests

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"wget/internal/concurrency"
	"wget/internal/downloader"
	"wget/internal/mirror"
)

func TestIntegration_DownloadManagerAggregatesErrors(t *testing.T) {
	payload := "integration-ok"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok1", "/ok2":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(payload + r.URL.Path))
		case "/fail":
			w.WriteHeader(http.StatusInternalServerError)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	outDir := t.TempDir()
	d := downloader.New(server.Client())
	mgr, err := concurrency.NewManager(d, concurrency.Config{
		Workers:        3,
		CaptureResults: true,
	})
	if err != nil {
		t.Fatalf("NewManager returned error: %v", err)
	}

	jobs := []concurrency.Job{
		{ID: 1, Request: integrationRequest(server.URL+"/ok1", filepath.Join(outDir, "ok1.bin"))},
		{ID: 2, Request: integrationRequest(server.URL+"/fail", filepath.Join(outDir, "fail.bin"))},
		{ID: 3, Request: integrationRequest(server.URL+"/ok2", filepath.Join(outDir, "ok2.bin"))},
	}

	summary, runErr := mgr.Run(context.Background(), jobs)
	if runErr == nil {
		t.Fatal("expected aggregate error")
	}
	var aggErr *concurrency.AggregateError
	if !errors.As(runErr, &aggErr) {
		t.Fatalf("expected AggregateError, got %T", runErr)
	}

	if summary.Total != 3 || summary.Succeeded != 2 || summary.Failed != 1 {
		t.Fatalf("unexpected summary counts: %#v", summary)
	}
	if len(summary.Results) != 3 {
		t.Fatalf("expected all results captured, got %d", len(summary.Results))
	}

	verifyFileContains(t, filepath.Join(outDir, "ok1.bin"), payload+"/ok1")
	verifyFileContains(t, filepath.Join(outDir, "ok2.bin"), payload+"/ok2")
	if _, err := os.Stat(filepath.Join(outDir, "fail.bin")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed target should not exist, stat err=%v", err)
	}
}

func TestIntegration_MirrorDownloadsPagesAndAssets(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/robots.txt":
			w.Header().Set("Content-Type", "text/plain")
			fmt.Fprint(w, "User-agent: *\nDisallow:\n")
		case "/":
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, `<html><body><a href="/docs/page.html">docs</a><img src="/assets/logo.png"></body></html>`)
		case "/docs/page.html":
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, `<html><body><a href="/">home</a></body></html>`)
		case "/assets/logo.png":
			w.Header().Set("Content-Type", "image/png")
			fmt.Fprint(w, "PNGDATA")
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	outDir := t.TempDir()
	engine := mirror.NewEngine(server.Client())
	summary, err := engine.Run(context.Background(), []string{server.URL + "/"}, mirror.Config{
		OutputDir:         outDir,
		MaxDepth:          5,
		MaxPages:          20,
		MaxTotalBytes:     1 << 20,
		AllowSchemeChange: true,
		RespectRobots:     true,
		ConvertLinks:      true,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if summary.PagesVisited < 2 {
		t.Fatalf("expected at least 2 pages visited, got %d", summary.PagesVisited)
	}
	if summary.ResourcesDownloaded < 3 {
		t.Fatalf("expected at least 3 resources downloaded, got %d", summary.ResourcesDownloaded)
	}

	host := strings.Split(strings.TrimPrefix(server.URL, "http://"), ":")[0]
	paths := []string{
		filepath.Join(outDir, host, "index.html"),
		filepath.Join(outDir, host, "docs", "page.html"),
		filepath.Join(outDir, host, "assets", "logo.png"),
	}
	for _, p := range paths {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("expected mirrored file missing: %s err=%v", p, err)
		}
	}
}

func integrationRequest(rawURL, outputPath string) downloader.Request {
	return downloader.Request{
		URL:         rawURL,
		OutputPath:  outputPath,
		Timeout:     2 * time.Second,
		MaxRetries:  1,
		BackoffBase: 1 * time.Millisecond,
		BackoffMax:  2 * time.Millisecond,
	}
}

func verifyFileContains(t *testing.T, path string, want string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(raw) != want {
		t.Fatalf("content mismatch for %s: got %q want %q", path, string(raw), want)
	}
}
