package main

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"wget/internal/cli"
	"wget/internal/fs"
	"wget/internal/logger"
)

func TestRun_SingleURLDownload(t *testing.T) {
	t.Setenv("HTTP_PROXY", "")
	t.Setenv("HTTPS_PROXY", "")
	t.Setenv("http_proxy", "")
	t.Setenv("https_proxy", "")

	payload := []byte("jpeg-bytes")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(payload)
	}))
	defer server.Close()

	outDir := t.TempDir()
	log := newTestLogger(t)
	cfg := cli.Config{
		OutputDir: outDir,
		Force:     false,
		URLs:      []string{server.URL + "/EMtmPFLWkAA8CIS.jpg"},
	}

	if err := run(cfg, log); err != nil {
		t.Fatalf("run returned error: %v", err)
	}

	target := filepath.Join(outDir, "EMtmPFLWkAA8CIS.jpg")
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("failed reading downloaded file: %v", err)
	}
	if string(got) != string(payload) {
		t.Fatalf("downloaded payload mismatch: got %q want %q", string(got), string(payload))
	}
}

func TestRun_FailsWhenTargetExistsWithoutForce(t *testing.T) {
	t.Setenv("HTTP_PROXY", "")
	t.Setenv("HTTPS_PROXY", "")
	t.Setenv("http_proxy", "")
	t.Setenv("https_proxy", "")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("x"))
	}))
	defer server.Close()

	outDir := t.TempDir()
	target := filepath.Join(outDir, "EMtmPFLWkAA8CIS.jpg")
	if err := os.WriteFile(target, []byte("existing"), 0o644); err != nil {
		t.Fatalf("seed existing file: %v", err)
	}

	log := newTestLogger(t)
	cfg := cli.Config{
		OutputDir: outDir,
		URLs:      []string{server.URL + "/EMtmPFLWkAA8CIS.jpg"},
	}

	err := run(cfg, log)
	if err == nil {
		t.Fatal("expected error when target exists without --force")
	}
	if !errors.Is(err, fs.ErrTargetExists) {
		t.Fatalf("expected fs.ErrTargetExists, got %v", err)
	}
}

func newTestLogger(t *testing.T) *logger.Logger {
	t.Helper()
	log, err := logger.New(logger.Config{
		Format: "human",
		Writer: io.Discard,
	})
	if err != nil {
		t.Fatalf("new logger: %v", err)
	}
	return log
}
