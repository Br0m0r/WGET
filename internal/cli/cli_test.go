package cli

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/pflag"
)

func TestParseArgs_ValidFlags(t *testing.T) {
	t.Run("single URL with -O and -P", func(t *testing.T) {
		cfg, err := ParseArgs([]string{
			"-O", "index.html",
			"-P", "/downloads/site",
			"--force",
			"--rate-limit", "500k",
			"https://example.com",
		})
		if err != nil {
			t.Fatalf("ParseArgs returned error: %v", err)
		}

		if cfg.OutputName != "index.html" {
			t.Fatalf("expected OutputName=index.html, got %q", cfg.OutputName)
		}
		if cfg.OutputDir != filepath.Clean("/downloads/site") {
			t.Fatalf("expected OutputDir=%q, got %q", filepath.Clean("/downloads/site"), cfg.OutputDir)
		}
		if !cfg.Force {
			t.Fatal("expected Force=true")
		}
		if cfg.RateLimitBytes != 500*1024 {
			t.Fatalf("expected RateLimitBytes=%d, got %d", 500*1024, cfg.RateLimitBytes)
		}
		if len(cfg.URLs) != 1 || cfg.URLs[0] != "https://example.com" {
			t.Fatalf("unexpected URLs parsed: %#v", cfg.URLs)
		}
	})

	t.Run("mirror options", func(t *testing.T) {
		cfg, err := ParseArgs([]string{
			"--mirror",
			"--strict-robots",
			"--convert-links",
			"-R", ".png,.jpg",
			"-X", "/admin,/tmp",
			"https://docs.example.com",
		})
		if err != nil {
			t.Fatalf("ParseArgs returned error: %v", err)
		}

		if !cfg.Mirror || !cfg.ConvertLinks {
			t.Fatalf("expected mirror and convert-links enabled")
		}
		if !cfg.StrictRobots {
			t.Fatalf("expected strict robots enabled")
		}
		if len(cfg.RejectPatterns) != 2 {
			t.Fatalf("expected 2 reject patterns, got %#v", cfg.RejectPatterns)
		}
		if len(cfg.ExcludeDirs) != 2 {
			t.Fatalf("expected 2 exclude dirs, got %#v", cfg.ExcludeDirs)
		}
	})

	t.Run("rate limit explicit form", func(t *testing.T) {
		cfg, err := ParseArgs([]string{
			"--debug",
			"--trace",
			"--log-format", "json",
			"--rate-limit", "2MiB/s",
			"https://example.com/file.bin",
		})
		if err != nil {
			t.Fatalf("ParseArgs returned error: %v", err)
		}
		if cfg.RateLimitBytes != 2*1024*1024 {
			t.Fatalf("expected RateLimitBytes=%d, got %d", 2*1024*1024, cfg.RateLimitBytes)
		}
		if !cfg.Debug || !cfg.Trace {
			t.Fatalf("expected debug+trace true, got debug=%v trace=%v", cfg.Debug, cfg.Trace)
		}
		if cfg.LogFormat != "json" {
			t.Fatalf("expected log format json, got %q", cfg.LogFormat)
		}
	})
}

func TestParseArgs_InvalidFlags(t *testing.T) {
	t.Run("unknown flag", func(t *testing.T) {
		_, err := ParseArgs([]string{"--unknown", "https://example.com"})
		if err == nil {
			t.Fatal("expected error for unknown flag")
		}
		if !strings.Contains(err.Error(), "unknown flag") {
			t.Fatalf("expected unknown flag error, got %v", err)
		}
	})

	t.Run("invalid rate limit", func(t *testing.T) {
		_, err := ParseArgs([]string{
			"--rate-limit", "10z",
			"https://example.com",
		})
		if err == nil {
			t.Fatal("expected error for invalid rate limit")
		}
		if !strings.Contains(err.Error(), "invalid --rate-limit") {
			t.Fatalf("expected invalid --rate-limit error, got %v", err)
		}
	})

	t.Run("invalid log format", func(t *testing.T) {
		_, err := ParseArgs([]string{
			"--log-format", "xml",
			"https://example.com",
		})
		if err == nil {
			t.Fatal("expected error for invalid log format")
		}
		if !strings.Contains(err.Error(), "--log-format must be either human or json") {
			t.Fatalf("expected log format validation error, got %v", err)
		}
	})

	t.Run("help output", func(t *testing.T) {
		_, err := ParseArgs([]string{"--help"})
		if !errors.Is(err, pflag.ErrHelp) {
			t.Fatalf("expected pflag.ErrHelp, got %v", err)
		}
	})
}

func TestParseArgs_ConflictingFlags(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{
			name:    "convert-links without mirror",
			args:    []string{"--convert-links", "https://example.com"},
			wantErr: "--convert-links requires --mirror",
		},
		{
			name:    "strict robots without mirror",
			args:    []string{"--strict-robots", "https://example.com"},
			wantErr: "--strict-robots requires --mirror",
		},
		{
			name:    "reject without mirror",
			args:    []string{"-R", ".png", "https://example.com"},
			wantErr: "-R/--reject requires --mirror",
		},
		{
			name:    "exclude directories without mirror",
			args:    []string{"-X", "/tmp", "https://example.com"},
			wantErr: "-X/--exclude-directories requires --mirror",
		},
		{
			name:    "input file with positional url",
			args:    []string{"-i", "urls.txt", "https://example.com"},
			wantErr: "cannot mix positional URLs with -i/--input-file",
		},
		{
			name:    "output with mirror",
			args:    []string{"-O", "index.html", "--mirror", "https://example.com"},
			wantErr: "-O/--output-document cannot be combined with --mirror",
		},
		{
			name:    "output with input file",
			args:    []string{"-O", "index.html", "-i", "urls.txt"},
			wantErr: "-O/--output-document cannot be combined with -i/--input-file",
		},
		{
			name:    "multiple urls with output filename",
			args:    []string{"-O", "index.html", "https://a.example.com", "https://b.example.com"},
			wantErr: "-O/--output-document supports exactly one URL",
		},
		{
			name:    "missing input",
			args:    []string{"-P", "/tmp"},
			wantErr: "a URL or -i/--input-file is required",
		},
		{
			name:    "invalid output name as path",
			args:    []string{"-O", "nested/index.html", "https://example.com"},
			wantErr: "-O/--output-document must be a filename, not a path",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseArgs(tc.args)
			if err == nil {
				t.Fatalf("expected error containing %q", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestParseRateLimit(t *testing.T) {
	tests := []struct {
		in      string
		want    int64
		wantErr bool
	}{
		{in: "500k", want: 500 * 1024},
		{in: "2m", want: 2 * 1024 * 1024},
		{in: "500KiB/s", want: 500 * 1024},
		{in: "2MiB/s", want: 2 * 1024 * 1024},
		{in: "500KB/s", want: 500 * 1024},
		{in: "2MB/s", want: 2 * 1024 * 1024},
		{in: "0k", wantErr: true},
		{in: "bad", wantErr: true},
	}

	for _, tc := range tests {
		got, err := ParseRateLimit(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Fatalf("ParseRateLimit(%q) expected error", tc.in)
			}
			continue
		}
		if err != nil {
			t.Fatalf("ParseRateLimit(%q) returned error: %v", tc.in, err)
		}
		if got != tc.want {
			t.Fatalf("ParseRateLimit(%q) expected %d, got %d", tc.in, tc.want, got)
		}
	}
}

func TestParseArgs_PowerShellSplitValueNormalization(t *testing.T) {
	cfg, err := ParseArgs([]string{
		"-O=test_20MB",
		".zip",
		"https://assets.01-edu.org/wgetDataSamples/20MB.zip",
	})
	if err != nil {
		t.Fatalf("ParseArgs returned error: %v", err)
	}
	if cfg.OutputName != "test_20MB.zip" {
		t.Fatalf("expected normalized output name, got %q", cfg.OutputName)
	}
	if len(cfg.URLs) != 1 {
		t.Fatalf("expected one URL after normalization, got %#v", cfg.URLs)
	}
}
