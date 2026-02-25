package logger

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestLogger_StructuredJSONOutput(t *testing.T) {
	buf := &bytes.Buffer{}
	log, err := New(Config{
		Format: "json",
		Writer: buf,
	})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	log.Info("download-start", "url", "https://example.com", "id", 7)
	entry := parseSingleJSONLine(t, buf.String())
	if entry["msg"] != "download-start" {
		t.Fatalf("expected msg=download-start, got %v", entry["msg"])
	}
	if entry["url"] != "https://example.com" {
		t.Fatalf("expected url field, got %v", entry["url"])
	}
	if number(entry["id"]) != 7 {
		t.Fatalf("expected id=7, got %v", entry["id"])
	}
}

func TestLogger_DebugAndTraceModes(t *testing.T) {
	t.Run("debug emits debug", func(t *testing.T) {
		buf := &bytes.Buffer{}
		log, err := New(Config{
			Format: "json",
			Writer: buf,
			Debug:  true,
		})
		if err != nil {
			t.Fatalf("New returned error: %v", err)
		}

		log.Debug("d-msg", "k", "v")
		entry := parseSingleJSONLine(t, buf.String())
		if entry["msg"] != "d-msg" {
			t.Fatalf("expected debug message, got %v", entry["msg"])
		}
	})

	t.Run("trace implies debug visibility", func(t *testing.T) {
		buf := &bytes.Buffer{}
		log, err := New(Config{
			Format: "json",
			Writer: buf,
			Trace:  true,
		})
		if err != nil {
			t.Fatalf("New returned error: %v", err)
		}

		log.Trace("t-msg", "k", "v")
		log.Debug("d-msg")
		lines := parseJSONLines(t, buf.String())
		if len(lines) != 2 {
			t.Fatalf("expected 2 log lines, got %d", len(lines))
		}
		if lines[0]["msg"] != "t-msg" || lines[1]["msg"] != "d-msg" {
			t.Fatalf("unexpected trace/debug sequence: %#v", lines)
		}
	})
}

func TestLogger_ErrorStackTraceOnlyInDebugOrTrace(t *testing.T) {
	baseErr := errors.New("boom")

	t.Run("info mode omits stack", func(t *testing.T) {
		buf := &bytes.Buffer{}
		log, err := New(Config{
			Format: "json",
			Writer: buf,
		})
		if err != nil {
			t.Fatalf("New returned error: %v", err)
		}
		log.Error(baseErr, "failed")
		entry := parseSingleJSONLine(t, buf.String())
		if _, ok := entry["stack"]; ok {
			t.Fatalf("did not expect stack field in info mode: %#v", entry)
		}
		if entry["error"] != "boom" {
			t.Fatalf("expected error field boom, got %v", entry["error"])
		}
	})

	t.Run("debug mode includes stack", func(t *testing.T) {
		buf := &bytes.Buffer{}
		log, err := New(Config{
			Format: "json",
			Writer: buf,
			Debug:  true,
		})
		if err != nil {
			t.Fatalf("New returned error: %v", err)
		}
		log.Error(baseErr, "failed")
		entry := parseSingleJSONLine(t, buf.String())
		stack, ok := entry["stack"].(string)
		if !ok || stack == "" {
			t.Fatalf("expected non-empty stack field in debug mode: %#v", entry)
		}
	})
}

func TestLogger_DefaultHumanFormat(t *testing.T) {
	buf := &bytes.Buffer{}
	log, err := New(Config{
		Writer: buf,
	})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	log.Info("hello", "id", 1)
	out := buf.String()
	if !strings.Contains(out, "level=INFO") || !strings.Contains(out, "msg=hello") {
		t.Fatalf("unexpected human output: %q", out)
	}
}

func TestLogger_InvalidFormat(t *testing.T) {
	_, err := New(Config{Format: "xml"})
	if err == nil {
		t.Fatal("expected format validation error")
	}
}

func parseSingleJSONLine(t *testing.T, raw string) map[string]any {
	t.Helper()
	lines := parseJSONLines(t, raw)
	if len(lines) != 1 {
		t.Fatalf("expected exactly one line, got %d raw=%q", len(lines), raw)
	}
	return lines[0]
}

func parseJSONLines(t *testing.T, raw string) []map[string]any {
	t.Helper()
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil
	}
	lines := strings.Split(trimmed, "\n")
	out := make([]map[string]any, 0, len(lines))
	for _, line := range lines {
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("failed to unmarshal JSON log line: %v line=%q", err, line)
		}
		out = append(out, m)
	}
	return out
}

func number(v any) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int64:
		return n
	case int:
		return int64(n)
	default:
		return -1
	}
}
