package fs

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveAndPrepare_PathTraversalProtection(t *testing.T) {
	root := t.TempDir()

	_, err := ResolveAndPrepare(ResolveOptions{
		URL:        "https://example.com/file.txt",
		OutputDir:  root,
		OutputName: ".." + string(filepath.Separator) + "escape.txt",
		Force:      false,
	})
	if err == nil {
		t.Fatal("expected traversal-related error")
	}
	if !errors.Is(err, ErrPathTraversal) && !strings.Contains(err.Error(), "invalid output filename") {
		t.Fatalf("expected path safety validation error, got: %v", err)
	}
}

func TestResolveAndPrepare_OverwriteBehavior(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "existing.txt")
	if err := os.WriteFile(target, []byte("existing"), 0o644); err != nil {
		t.Fatalf("seed existing file: %v", err)
	}

	_, err := ResolveAndPrepare(ResolveOptions{
		URL:        "https://example.com/existing.txt",
		OutputDir:  root,
		OutputName: "existing.txt",
		Force:      false,
	})
	if !errors.Is(err, ErrTargetExists) {
		t.Fatalf("expected ErrTargetExists, got %v", err)
	}

	plan, err := ResolveAndPrepare(ResolveOptions{
		URL:        "https://example.com/existing.txt",
		OutputDir:  root,
		OutputName: "existing.txt",
		Force:      true,
	})
	if err != nil {
		t.Fatalf("expected force overwrite to pass, got %v", err)
	}
	if plan.TargetPath != target {
		t.Fatalf("unexpected target path: got %q want %q", plan.TargetPath, target)
	}
}

func TestResolveAndPrepare_DefaultFilenameForTrailingSlash(t *testing.T) {
	root := t.TempDir()
	plan, err := ResolveAndPrepare(ResolveOptions{
		URL:       "https://example.com/docs/",
		OutputDir: root,
		Force:     false,
	})
	if err != nil {
		t.Fatalf("ResolveAndPrepare returned error: %v", err)
	}
	if plan.FileName != "index.html" {
		t.Fatalf("expected index.html, got %q", plan.FileName)
	}
}

func TestResolveAndPrepare_UsesWorkingDirWhenOutputDirMissing(t *testing.T) {
	wd := t.TempDir()
	plan, err := ResolveAndPrepare(ResolveOptions{
		URL:        "https://example.com/a.txt",
		WorkingDir: wd,
	})
	if err != nil {
		t.Fatalf("ResolveAndPrepare returned error: %v", err)
	}
	if plan.RootDir != wd {
		t.Fatalf("expected root dir %q, got %q", wd, plan.RootDir)
	}
}

func TestResolveAndPrepare_ExpandsTildeOutputDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	plan, err := ResolveAndPrepare(ResolveOptions{
		URL:       "https://example.com/file.txt",
		OutputDir: "~/Downloads",
	})
	if err != nil {
		t.Fatalf("ResolveAndPrepare returned error: %v", err)
	}

	wantRoot := filepath.Join(home, "Downloads")
	if plan.RootDir != wantRoot {
		t.Fatalf("expected root dir %q, got %q", wantRoot, plan.RootDir)
	}
}
