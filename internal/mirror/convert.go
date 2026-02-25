package mirror

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"golang.org/x/net/html"
)

var mirrorAttrByTag = map[string][]string{
	"a":      {"href"},
	"link":   {"href"},
	"script": {"src"},
	"img":    {"src", "srcset"},
	"source": {"src", "srcset"},
	"video":  {"src", "poster"},
	"audio":  {"src"},
	"iframe": {"src"},
}

var mirrorCSSURLPattern = regexp.MustCompile(`(?is)url\(\s*(['"]?)([^'")]+)(?:['"]?)\s*\)`)

func convertHTMLFile(filePath string, pageURL *url.URL, outputRoot string, seedDomains []string, seedSchemes map[string]struct{}, cfg Config) error {
	f, err := os.Open(filePath)
	if err != nil {
		return err
	}

	root, err := html.Parse(f)
	_ = f.Close()
	if err != nil {
		return fmt.Errorf("parse html: %w", err)
	}

	changed := false

	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n == nil {
			return
		}

		if n.Type == html.ElementNode {
			tag := strings.ToLower(n.Data)
			keys, ok := mirrorAttrByTag[tag]
			if ok {
				for i := range n.Attr {
					key := strings.ToLower(n.Attr[i].Key)
					if !containsString(keys, key) {
						continue
					}

					if key == "srcset" {
						rewritten, did := rewriteSrcSet(n.Attr[i].Val, pageURL, filePath, outputRoot, seedDomains, seedSchemes, cfg)
						if did {
							n.Attr[i].Val = rewritten
							changed = true
						}
						continue
					}

					rewritten, did := rewriteLinkValue(n.Attr[i].Val, pageURL, filePath, outputRoot, seedDomains, seedSchemes, cfg)
					if did {
						n.Attr[i].Val = rewritten
						changed = true
					}
				}
			}

			for i := range n.Attr {
				if !strings.EqualFold(n.Attr[i].Key, "style") {
					continue
				}
				rewritten, did := rewriteCSSURLValues(n.Attr[i].Val, pageURL, filePath, outputRoot, seedDomains, seedSchemes, cfg)
				if did {
					n.Attr[i].Val = rewritten
					changed = true
				}
			}

			if tag == "style" {
				for c := n.FirstChild; c != nil; c = c.NextSibling {
					if c.Type != html.TextNode {
						continue
					}
					rewritten, did := rewriteCSSURLValues(c.Data, pageURL, filePath, outputRoot, seedDomains, seedSchemes, cfg)
					if did {
						c.Data = rewritten
						changed = true
					}
				}
			}
		}

		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)

	if !changed {
		return nil
	}

	tmpPath := filePath + ".tmp"
	out, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("create temp converted html: %w", err)
	}
	if err := html.Render(out, root); err != nil {
		_ = out.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("render converted html: %w", err)
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("close converted html: %w", err)
	}

	if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("remove original html before replace: %w", err)
	}
	if err := os.Rename(tmpPath, filePath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("replace converted html: %w", err)
	}
	return nil
}

func rewriteCSSURLValues(css string, pageURL *url.URL, currentFilePath, outputRoot string, seedDomains []string, seedSchemes map[string]struct{}, cfg Config) (string, bool) {
	changed := false
	out := mirrorCSSURLPattern.ReplaceAllStringFunc(css, func(match string) string {
		parts := mirrorCSSURLPattern.FindStringSubmatch(match)
		if len(parts) < 3 {
			return match
		}
		quote := parts[1]
		raw := strings.TrimSpace(parts[2])
		rewritten, did := rewriteLinkValue(raw, pageURL, currentFilePath, outputRoot, seedDomains, seedSchemes, cfg)
		if !did {
			return match
		}
		changed = true
		return "url(" + quote + rewritten + quote + ")"
	})
	return out, changed
}

func rewriteSrcSet(value string, pageURL *url.URL, currentFilePath, outputRoot string, seedDomains []string, seedSchemes map[string]struct{}, cfg Config) (string, bool) {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	changed := false

	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		fields := strings.Fields(p)
		if len(fields) == 0 {
			continue
		}

		rewritten, did := rewriteLinkValue(fields[0], pageURL, currentFilePath, outputRoot, seedDomains, seedSchemes, cfg)
		if did {
			changed = true
			fields[0] = rewritten
		}
		out = append(out, strings.Join(fields, " "))
	}

	if len(out) == 0 {
		return value, false
	}
	return strings.Join(out, ", "), changed
}

func rewriteLinkValue(raw string, pageURL *url.URL, currentFilePath, outputRoot string, seedDomains []string, seedSchemes map[string]struct{}, cfg Config) (string, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return raw, false
	}

	lower := strings.ToLower(trimmed)
	if strings.HasPrefix(lower, "javascript:") || strings.HasPrefix(lower, "mailto:") || strings.HasPrefix(lower, "tel:") || strings.HasPrefix(lower, "data:") || strings.HasPrefix(lower, "#") {
		return raw, false
	}

	ref, err := url.Parse(trimmed)
	if err != nil {
		return raw, false
	}
	abs := pageURL.ResolveReference(ref)
	if abs.Scheme != "http" && abs.Scheme != "https" {
		return raw, false
	}
	if abs.Host == "" {
		return raw, false
	}

	if !matchesSeedDomain(abs.Hostname(), seedDomains) {
		return raw, false
	}
	if !cfg.AllowSchemeChange {
		if _, ok := seedSchemes[strings.ToLower(abs.Scheme)]; !ok {
			return raw, false
		}
	}
	if shouldExcludePath(abs.EscapedPath(), cfg.ExcludeDirs) || shouldRejectURL(abs, cfg.RejectPatterns) {
		return raw, false
	}

	targetPath, err := mapURLToPath(outputRoot, abs)
	if err != nil {
		return raw, false
	}
	rel, err := filepath.Rel(filepath.Dir(currentFilePath), targetPath)
	if err != nil {
		return raw, false
	}

	rel = filepath.ToSlash(rel)
	if rel == "" {
		rel = "./"
	}
	if abs.Fragment != "" {
		rel += "#" + abs.Fragment
	}
	return rel, true
}

func containsString(values []string, v string) bool {
	for _, item := range values {
		if item == v {
			return true
		}
	}
	return false
}
