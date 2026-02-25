package concurrency

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseURLs(t *testing.T) {
	raw := `
# comment
https://example.com/a

https://example.com/b
`
	urls, err := ParseURLs(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("ParseURLs returned error: %v", err)
	}
	if len(urls) != 2 {
		t.Fatalf("expected 2 URLs, got %d", len(urls))
	}
	if urls[0] != "https://example.com/a" || urls[1] != "https://example.com/b" {
		t.Fatalf("unexpected URLs: %#v", urls)
	}
}

func TestLoadURLsFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "urls.txt")
	if err := os.WriteFile(path, []byte("https://example.com/x\n"), 0o644); err != nil {
		t.Fatalf("seed urls file: %v", err)
	}

	urls, err := LoadURLsFromFile(path)
	if err != nil {
		t.Fatalf("LoadURLsFromFile returned error: %v", err)
	}
	if len(urls) != 1 || urls[0] != "https://example.com/x" {
		t.Fatalf("unexpected URLs: %#v", urls)
	}
}
