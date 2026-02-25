package background

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	// DefaultLogFile is the default combined stdout/stderr file for background mode.
	DefaultLogFile = "wget-log"
	// DefaultPIDFile is the default pid file path for background mode.
	DefaultPIDFile = "wget.pid"
	// ChildEnvVar marks a process as already running in background child mode.
	ChildEnvVar = "WGET_BACKGROUND_CHILD"
)

// Config defines process launch options for background mode.
type Config struct {
	Executable string
	Args       []string
	WorkingDir string
	Env        []string
	LogFile    string
	PIDFile    string
}

// Result describes a started background process.
type Result struct {
	PID     int
	LogPath string
	PIDPath string
}

// IsBackgroundChild returns true when running inside the detached child process.
func IsBackgroundChild() bool {
	return os.Getenv(ChildEnvVar) == "1"
}

// Start launches a detached background process, redirects logs, and writes a pid file.
func Start(cfg Config) (Result, error) {
	if strings.TrimSpace(cfg.Executable) == "" {
		return Result{}, errors.New("executable is required")
	}

	wd := strings.TrimSpace(cfg.WorkingDir)
	if wd == "" {
		var err error
		wd, err = os.Getwd()
		if err != nil {
			return Result{}, fmt.Errorf("resolve working directory: %w", err)
		}
	}

	logPath, err := resolvePath(wd, cfg.LogFile, DefaultLogFile)
	if err != nil {
		return Result{}, fmt.Errorf("resolve log path: %w", err)
	}
	pidPath, err := resolvePath(wd, cfg.PIDFile, DefaultPIDFile)
	if err != nil {
		return Result{}, fmt.Errorf("resolve pid path: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		return Result{}, fmt.Errorf("create log directory: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(pidPath), 0o755); err != nil {
		return Result{}, fmt.Errorf("create pid directory: %w", err)
	}

	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return Result{}, fmt.Errorf("open log file: %w", err)
	}
	defer logFile.Close()

	filteredArgs := filterBackgroundArgs(cfg.Args)
	cmd := exec.Command(cfg.Executable, filteredArgs...)
	cmd.Dir = wd
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Stdin = nil
	cmd.Env = append(os.Environ(), cfg.Env...)
	cmd.Env = append(cmd.Env, ChildEnvVar+"=1")
	applyDetach(cmd)

	if err := cmd.Start(); err != nil {
		return Result{}, fmt.Errorf("start background process: %w", err)
	}

	pid := cmd.Process.Pid
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(pid)+"\n"), 0o644); err != nil {
		return Result{}, fmt.Errorf("write pid file: %w", err)
	}

	return Result{
		PID:     pid,
		LogPath: logPath,
		PIDPath: pidPath,
	}, nil
}

func resolvePath(wd, value, fallback string) (string, error) {
	p := strings.TrimSpace(value)
	if p == "" {
		p = fallback
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(wd, p)
	}
	abs, err := filepath.Abs(filepath.Clean(p))
	if err != nil {
		return "", err
	}
	return abs, nil
}

func filterBackgroundArgs(args []string) []string {
	filtered := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "-B" || arg == "--background" {
			continue
		}
		filtered = append(filtered, arg)
	}
	return filtered
}
