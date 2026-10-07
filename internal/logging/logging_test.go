package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

func TestParseLevel(t *testing.T) {
	for in, want := range map[string]slog.Level{
		"debug": slog.LevelDebug, "INFO": slog.LevelInfo, "": slog.LevelInfo,
		"warn": slog.LevelWarn, "warning": slog.LevelWarn, " error ": slog.LevelError,
	} {
		if got, err := ParseLevel(in); err != nil || got != want {
			t.Errorf("ParseLevel(%q) = %v, %v", in, got, err)
		}
	}
	if _, err := ParseLevel("verbose"); err == nil {
		t.Error("unknown level should fail")
	}
	if _, err := New(&bytes.Buffer{}, slog.LevelInfo, "xml"); err == nil {
		t.Error("unknown format should fail")
	}
}

func TestRequestDetailsInEveryLine(t *testing.T) {
	var buf bytes.Buffer
	log, err := New(&buf, slog.LevelInfo, "json")
	if err != nil {
		t.Fatal(err)
	}
	ctx := WithRequest(context.Background(), "abc123")
	log.InfoContext(ctx, "before sign-in")
	SetUser(ctx, "alice")
	// A context derived later still carries the request.
	derived := context.WithValue(ctx, struct{}{}, 1)
	log.With("component", "test").InfoContext(derived, "after sign-in")
	log.InfoContext(context.Background(), "outside a request")
	log.Debug("hidden at info level")

	var lines []map[string]any
	for _, l := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		var m map[string]any
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			t.Fatalf("not JSON: %q", l)
		}
		lines = append(lines, m)
	}
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3:\n%s", len(lines), buf.String())
	}
	if lines[0]["request_id"] != "abc123" || lines[0]["user"] != nil {
		t.Errorf("before sign-in: %v", lines[0])
	}
	if lines[1]["request_id"] != "abc123" || lines[1]["user"] != "alice" || lines[1]["component"] != "test" {
		t.Errorf("after sign-in: %v", lines[1])
	}
	if _, ok := lines[2]["request_id"]; ok {
		t.Errorf("outside a request: %v", lines[2])
	}
}

func TestDebugAddsSource(t *testing.T) {
	var buf bytes.Buffer
	log, _ := New(&buf, slog.LevelDebug, "text")
	log.Debug("here")
	if !strings.Contains(buf.String(), "source=") || !strings.Contains(buf.String(), "logging_test.go") {
		t.Fatalf("debug lines should name their source: %s", buf.String())
	}
}

func TestRequestIDs(t *testing.T) {
	a, b := NewRequestID(), NewRequestID()
	if len(a) != 16 || a == b || !ValidRequestID(a) {
		t.Fatalf("ids %q %q", a, b)
	}
	for id, ok := range map[string]bool{
		"f0e1d2c3-b4a5": true, "req.42_x": true, "": false, "two words": false,
		"line\nbreak": false, `quote"`: false, strings.Repeat("a", 65): false,
	} {
		if ValidRequestID(id) != ok {
			t.Errorf("ValidRequestID(%q) = %v", id, !ok)
		}
	}
}
