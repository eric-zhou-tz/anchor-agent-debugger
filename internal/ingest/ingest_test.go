package ingest

import (
	"errors"
	"testing"
	"time"
)

func TestCaptureValidRawJSONInput(t *testing.T) {
	got, err := Capture([]byte(`{"hook":"example","nested":{"ok":true}}`), "codex", Options{
		Now: func() time.Time {
			return time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
		},
		GenerateID: func() (string, error) {
			return "evt_test", nil
		},
	})
	if err != nil {
		t.Fatalf("Capture returned error: %v", err)
	}

	if got.EventID != "evt_test" {
		t.Fatalf("EventID = %q, want %q", got.EventID, "evt_test")
	}
	if got.Source != "codex" {
		t.Fatalf("Source = %q, want %q", got.Source, "codex")
	}
	if !got.ReceivedAt.Equal(time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("ReceivedAt = %s, want fixed test time", got.ReceivedAt)
	}
	if string(got.RawPayload) != `{"hook":"example","nested":{"ok":true}}` {
		t.Fatalf("RawPayload = %s", got.RawPayload)
	}
}

func TestCaptureInvalidJSONInput(t *testing.T) {
	_, err := Capture([]byte(`{"hook":`), "codex", Options{})
	if !errors.Is(err, ErrInvalidJSON) {
		t.Fatalf("Capture error = %v, want %v", err, ErrInvalidJSON)
	}
}

func TestCaptureEmptyInput(t *testing.T) {
	_, err := Capture([]byte(" \n\t "), "codex", Options{})
	if !errors.Is(err, ErrEmptyInput) {
		t.Fatalf("Capture error = %v, want %v", err, ErrEmptyInput)
	}
}

func TestCaptureMissingSource(t *testing.T) {
	_, err := Capture([]byte(`{"hook":"example"}`), "", Options{})
	if !errors.Is(err, ErrMissingSource) {
		t.Fatalf("Capture error = %v, want %v", err, ErrMissingSource)
	}
}
