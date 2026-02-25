package mirror

import (
	"net/url"
	"path"
	"strings"
)

func shouldExcludePath(rawPath string, excludeDirs []string) bool {
	p := normalizeURLPath(rawPath)
	if p == "" {
		p = "/"
	}

	for _, ex := range excludeDirs {
		ex = strings.TrimSpace(ex)
		if ex == "" {
			continue
		}
		ex = normalizeURLPath(ex)
		if ex == "/" {
			return true
		}
		if p == ex || strings.HasPrefix(p, ex+"/") {
			return true
		}
	}
	return false
}

func shouldRejectURL(u *url.URL, patterns []string) bool {
	if u == nil {
		return false
	}

	p := normalizeURLPath(u.EscapedPath())
	base := strings.ToLower(path.Base(p))
	full := strings.TrimPrefix(strings.ToLower(p), "/")
	ext := strings.ToLower(path.Ext(base))

	for _, raw := range patterns {
		pattern := strings.TrimSpace(strings.ToLower(raw))
		if pattern == "" {
			continue
		}

		if strings.HasPrefix(pattern, ".") && !containsGlobMeta(pattern) {
			if ext == pattern {
				return true
			}
			continue
		}
		if isPlainExtensionPattern(pattern) {
			if ext == "."+pattern {
				return true
			}
			continue
		}

		if matched, _ := path.Match(pattern, base); matched {
			return true
		}
		patternFull := strings.TrimPrefix(pattern, "/")
		if matched, _ := path.Match(patternFull, full); matched {
			return true
		}
	}
	return false
}

func containsGlobMeta(s string) bool {
	return strings.ContainsAny(s, "*?[")
}

func isPlainExtensionPattern(pattern string) bool {
	if pattern == "" {
		return false
	}
	if containsGlobMeta(pattern) {
		return false
	}
	if strings.Contains(pattern, "/") || strings.Contains(pattern, "\\") {
		return false
	}
	if strings.HasPrefix(pattern, ".") {
		return false
	}
	return true
}

func normalizeURLPath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return "/"
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	p = path.Clean(p)
	if p == "." {
		return "/"
	}
	return p
}
