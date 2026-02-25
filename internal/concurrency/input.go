package concurrency

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
)

// LoadURLsFromFile loads URLs from a plain-text file.
// Empty lines and lines starting with '#' are ignored.
func LoadURLsFromFile(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open URL input file: %w", err)
	}
	defer f.Close()

	return ParseURLs(f)
}

// ParseURLs parses URLs from a reader.
func ParseURLs(r io.Reader) ([]string, error) {
	scanner := bufio.NewScanner(r)
	out := make([]string, 0)
	lineNo := 0

	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, line)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read URL input: %w", err)
	}
	return out, nil
}
