package ingest

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	// ErrEmptyInput is returned when stdin contains no JSON payload.
	ErrEmptyInput = errors.New("empty stdin")

	// ErrInvalidJSON is returned when stdin is not syntactically valid JSON.
	ErrInvalidJSON = errors.New("invalid JSON")

	// ErrMissingSource is returned when the ingest source flag is blank.
	ErrMissingSource = errors.New("missing --source")
)

// RawEvent is the minimal capture envelope for an unparsed hook payload.
type RawEvent struct {
	EventID    string          `json:"event_id"`
	Source     string          `json:"source"`
	ReceivedAt time.Time       `json:"received_at"`
	RawPayload json.RawMessage `json:"raw_payload"`
}

// Options provides test seams for event metadata generation.
type Options struct {
	Now        func() time.Time
	GenerateID func() (string, error)
}

// Capture validates raw JSON input and wraps it in a raw event envelope.
func Capture(raw []byte, source string, opts Options) (*RawEvent, error) {
	if strings.TrimSpace(source) == "" {
		return nil, ErrMissingSource
	}

	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil, ErrEmptyInput
	}

	if !json.Valid(trimmed) {
		return nil, ErrInvalidJSON
	}

	now := opts.Now
	if now == nil {
		now = func() time.Time {
			return time.Now().UTC()
		}
	}

	generateID := opts.GenerateID
	if generateID == nil {
		generateID = NewEventID
	}

	eventID, err := generateID()
	if err != nil {
		return nil, fmt.Errorf("generate event id: %w", err)
	}

	return &RawEvent{
		EventID:    eventID,
		Source:     strings.TrimSpace(source),
		ReceivedAt: now().UTC(),
		RawPayload: json.RawMessage(trimmed),
	}, nil
}

// NewEventID returns a random event identifier for captured raw events.
func NewEventID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}

	return "evt_" + hex.EncodeToString(b[:]), nil
}
