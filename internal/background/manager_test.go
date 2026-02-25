package background

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestBackgroundHelperProcess(t *testing.T) {
	if os.Getenv("WGET_BG_HELPER") != "1" {
		return
	}
	fmt.Fprintln(os.Stdout, "bg-stdout-line")
	fmt.Fprintln(os.Stderr, "bg-stderr-line")
	os.Exit(0)
}

func TestStart_LogCorrectness(t *testing.T) {
	tmpDir := t.TempDir()
	start := time.Now()

	res, err := Start(Config{
		Executable: os.Args[0],
		Args:       []string{"-test.run=TestBackgroundHelperProcess"},
		WorkingDir: tmpDir,
		Env:        []string{"WGET_BG_HELPER=1"},
	})
	if err != nil {
		t.Fatalf("Start returned error: %v", err)
	}

	if res.PID <= 0 {
		t.Fatalf("expected positive pid, got %d", res.PID)
	}

	content, err := waitForFileContains(res.LogPath, 3*time.Second, "bg-stdout-line", "bg-stderr-line")
	if err != nil {
		t.Fatalf("log validation failed: %v", err)
	}
	if !strings.Contains(content, "bg-stdout-line") || !strings.Contains(content, "bg-stderr-line") {
		t.Fatalf("expected combined stdout/stderr in log, got: %q", content)
	}

	pidRaw, err := os.ReadFile(res.PIDPath)
	if err != nil {
		t.Fatalf("read pid file: %v", err)
	}
	pidInFile, err := strconv.Atoi(strings.TrimSpace(string(pidRaw)))
	if err != nil {
		t.Fatalf("parse pid file: %v", err)
	}
	if pidInFile != res.PID {
		t.Fatalf("pid mismatch: file=%d result=%d", pidInFile, res.PID)
	}

	if time.Since(start) > 700*time.Millisecond {
		t.Fatalf("background start took too long; expected immediate return")
	}
}

func TestStart_BackgroundExecutionValidation(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "custom.log")
	pidPath := filepath.Join(tmpDir, "custom.pid")

	res, err := Start(Config{
		Executable: os.Args[0],
		Args:       []string{"-test.run=TestBackgroundHelperProcess", "--background", "-B"},
		WorkingDir: tmpDir,
		Env:        []string{"WGET_BG_HELPER=1"},
		LogFile:    logPath,
		PIDFile:    pidPath,
	})
	if err != nil {
		t.Fatalf("Start returned error: %v", err)
	}

	if res.LogPath != filepath.Clean(logPath) {
		t.Fatalf("expected log path %q, got %q", filepath.Clean(logPath), res.LogPath)
	}
	if res.PIDPath != filepath.Clean(pidPath) {
		t.Fatalf("expected pid path %q, got %q", filepath.Clean(pidPath), res.PIDPath)
	}

	_, err = waitForFileContains(res.LogPath, 3*time.Second, "bg-stdout-line")
	if err != nil {
		t.Fatalf("background child output was not captured: %v", err)
	}
}

func waitForFileContains(path string, timeout time.Duration, parts ...string) (string, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil {
			content := string(data)
			found := true
			for _, p := range parts {
				if !strings.Contains(content, p) {
					found = false
					break
				}
			}
			if found {
				return content, nil
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	return "", fmt.Errorf("timed out waiting for content in %s", path)
}
