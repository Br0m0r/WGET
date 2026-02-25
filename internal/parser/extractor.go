package parser

import (
	"bytes"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strings"

	"golang.org/x/net/html"
)

var attrByTag = map[string][]string{
	"a":      {"href"},
	"link":   {"href"},
	"script": {"src"},
	"img":    {"src", "srcset"},
	"source": {"src", "srcset"},
	"video":  {"src", "poster"},
	"audio":  {"src"},
	"iframe": {"src"},
}

var cssURLPattern = regexp.MustCompile(`(?is)url\(\s*(['"]?)([^'")]+)(?:['"]?)\s*\)`)

// ExtractLinks parses HTML and returns absolute http/https links resolved from baseURL.
func ExtractLinks(baseURL string, body []byte) ([]string, error) {
	return ExtractLinksFromReader(baseURL, bytes.NewReader(body))
}

// ExtractLinksFromReader parses HTML from a reader and returns resolved absolute links.
func ExtractLinksFromReader(baseURL string, r io.Reader) ([]string, error) {
	base, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || base.Scheme == "" || base.Host == "" {
		return nil, fmt.Errorf("invalid base URL %q", baseURL)
	}

	root, err := html.Parse(r)
	if err != nil {
		return nil, fmt.Errorf("parse html: %w", err)
	}

	out := make([]string, 0)
	seen := make(map[string]struct{})

	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n == nil {
			return
		}

		if n.Type == html.ElementNode {
			tag := strings.ToLower(n.Data)
			if attrs, ok := attrByTag[tag]; ok {
				for _, attr := range attrs {
					vals := attrValues(n, attr)
					for _, raw := range vals {
						canon, ok := normalizeLink(base, raw)
						if !ok {
							continue
						}
						if _, exists := seen[canon]; exists {
							continue
						}
						seen[canon] = struct{}{}
						out = append(out, canon)
					}
				}
			}

			for _, rawStyle := range attrValues(n, "style") {
				for _, cssLink := range extractCSSURLs(rawStyle) {
					canon, ok := normalizeLink(base, cssLink)
					if !ok {
						continue
					}
					if _, exists := seen[canon]; exists {
						continue
					}
					seen[canon] = struct{}{}
					out = append(out, canon)
				}
			}

			if tag == "style" {
				for c := n.FirstChild; c != nil; c = c.NextSibling {
					if c.Type != html.TextNode {
						continue
					}
					for _, cssLink := range extractCSSURLs(c.Data) {
						canon, ok := normalizeLink(base, cssLink)
						if !ok {
							continue
						}
						if _, exists := seen[canon]; exists {
							continue
						}
						seen[canon] = struct{}{}
						out = append(out, canon)
					}
				}
			}
		}

		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)

	return out, nil
}

func extractCSSURLs(css string) []string {
	matches := cssURLPattern.FindAllStringSubmatch(css, -1)
	if len(matches) == 0 {
		return nil
	}
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		if len(m) < 3 {
			continue
		}
		raw := strings.TrimSpace(m[2])
		if raw == "" {
			continue
		}
		out = append(out, raw)
	}
	return out
}

func attrValues(node *html.Node, key string) []string {
	values := make([]string, 0)
	for _, a := range node.Attr {
		if strings.EqualFold(a.Key, key) {
			if strings.EqualFold(key, "srcset") {
				values = append(values, parseSrcSet(a.Val)...)
				continue
			}
			values = append(values, strings.TrimSpace(a.Val))
		}
	}
	return values
}

func parseSrcSet(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		fields := strings.Fields(p)
		if len(fields) == 0 {
			continue
		}
		out = append(out, fields[0])
	}
	return out
}

func normalizeLink(base *url.URL, raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}

	lower := strings.ToLower(raw)
	if strings.HasPrefix(lower, "javascript:") || strings.HasPrefix(lower, "mailto:") || strings.HasPrefix(lower, "tel:") || strings.HasPrefix(lower, "data:") {
		return "", false
	}

	ref, err := url.Parse(raw)
	if err != nil {
		return "", false
	}

	abs := base.ResolveReference(ref)
	abs.Fragment = ""
	abs.Scheme = strings.ToLower(abs.Scheme)
	abs.Host = strings.ToLower(abs.Host)

	if abs.Scheme != "http" && abs.Scheme != "https" {
		return "", false
	}
	if abs.Host == "" {
		return "", false
	}

	return abs.String(), true
}
