package fs

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
)

var (
	// ErrPathTraversal is returned when the resolved target escapes the configured root.
	ErrPathTraversal = errors.New("resolved path escapes allowed root")
	// ErrTargetExists is returned when the target file already exists and force is disabled.
	ErrTargetExists = errors.New("target file already exists")
)

// ResolveOptions defines path resolution inputs from CLI/runtime config.
type ResolveOptions struct {
	URL        string
	OutputDir  string
	OutputName string
	WorkingDir string
	Force      bool
}

// PathPlan is the resolved write plan for a download.
type PathPlan struct {
	RootDir    string
	TargetPath string
	PartPath   string
	FileName   string
}

// ResolveAndPrepare resolves target paths safely and prepares output directories/files.
func ResolveAndPrepare(opts ResolveOptions) (PathPlan, error) {
	root, err := resolveRoot(opts.OutputDir, opts.WorkingDir)
	if err != nil {
		return PathPlan{}, err
	}

	fileName, err := resolveFileName(opts.URL, opts.OutputName)
	if err != nil {
		return PathPlan{}, err
	}

	target := filepath.Join(root, fileName)
	targetAbs, err := filepath.Abs(filepath.Clean(target))
	if err != nil {
		return PathPlan{}, fmt.Errorf("resolve target path: %w", err)
	}

	if !withinRoot(root, targetAbs) {
		return PathPlan{}, ErrPathTraversal
	}

	if err := os.MkdirAll(filepath.Dir(targetAbs), 0o755); err != nil {
		return PathPlan{}, fmt.Errorf("create output directory: %w", err)
	}

	if !opts.Force {
		if _, statErr := os.Stat(targetAbs); statErr == nil {
			return PathPlan{}, ErrTargetExists
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return PathPlan{}, fmt.Errorf("inspect target file: %w", statErr)
		}
	}

	return PathPlan{
		RootDir:    root,
		TargetPath: targetAbs,
		PartPath:   targetAbs + ".part",
		FileName:   fileName,
	}, nil
}

func resolveRoot(outputDir, workingDir string) (string, error) {
	base := strings.TrimSpace(outputDir)
	if expanded, err := expandTildePath(base); err != nil {
		return "", err
	} else if expanded != "" {
		base = expanded
	}
	if base == "" {
		base = strings.TrimSpace(workingDir)
		if base == "" {
			var err error
			base, err = os.Getwd()
			if err != nil {
				return "", fmt.Errorf("resolve working directory: %w", err)
			}
		}
	}

	abs, err := filepath.Abs(filepath.Clean(base))
	if err != nil {
		return "", fmt.Errorf("resolve output root: %w", err)
	}

	if err := os.MkdirAll(abs, 0o755); err != nil {
		return "", fmt.Errorf("create output root: %w", err)
	}

	return abs, nil
}

func expandTildePath(p string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", nil
	}
	if p == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve user home directory: %w", err)
		}
		return home, nil
	}
	if strings.HasPrefix(p, "~/") || strings.HasPrefix(p, "~\\") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve user home directory: %w", err)
		}
		return filepath.Join(home, p[2:]), nil
	}
	return p, nil
}

func resolveFileName(rawURL, outputName string) (string, error) {
	name := strings.TrimSpace(outputName)
	if name != "" {
		if filepath.Base(name) != name || name == "." || name == ".." {
			return "", fmt.Errorf("invalid output filename %q", outputName)
		}
		return name, nil
	}

	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("invalid URL %q", rawURL)
	}

	p := u.EscapedPath()
	if p == "" || strings.HasSuffix(p, "/") {
		return "index.html", nil
	}

	base := path.Base(p)
	if base == "." || base == "/" || base == "" {
		return "index.html", nil
	}

	decoded, err := url.PathUnescape(base)
	if err != nil {
		return "", fmt.Errorf("decode URL filename: %w", err)
	}
	decoded = strings.TrimSpace(decoded)
	if decoded == "" || decoded == "." || decoded == ".." {
		return "index.html", nil
	}

	decoded = strings.ReplaceAll(decoded, string(filepath.Separator), "_")
	if strings.Contains(decoded, "/") || strings.Contains(decoded, "\\") {
		decoded = strings.NewReplacer("/", "_", "\\", "_").Replace(decoded)
	}

	return decoded, nil
}

func withinRoot(root, candidate string) bool {
	rootClean := filepath.Clean(root)
	candidateClean := filepath.Clean(candidate)
	rel, err := filepath.Rel(rootClean, candidateClean)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
