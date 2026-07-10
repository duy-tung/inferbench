// Package events defines the raw-event record (contracts-owned
// raw-event.schema.json, Contract 3) and the single-writer JSONL recorder.
//
// Field semantics follow the schema exactly: nullable required fields assert
// "not applicable", never "not recorded". All latencies are client-side
// measurements in seconds (the client-side mirror series; gateway-side
// series are separate by definition).
package events

import (
	"encoding/json"
	"fmt"
	"time"
)

// Status values (raw-event.schema.json).
const (
	StatusOK       = "ok"
	StatusError    = "error"
	StatusCanceled = "canceled"
	StatusShed     = "shed"
)

// Event is one JSONL record per request. Pointer fields marshal to explicit
// null (they are required-but-nullable in the schema, so no omitempty).
type Event struct {
	RunID             string             `json:"run_id"`
	Repetition        int                `json:"repetition"`
	RequestID         string             `json:"request_id"`
	WorkloadItem      int                `json:"workload_item"`
	SendTS            Timestamp          `json:"send_ts"`
	EndTS             Timestamp          `json:"end_ts"`
	Status            string             `json:"status"`
	ErrorClass        *string            `json:"error_class"`
	TTFTSeconds       *float64           `json:"ttft_seconds"`
	ITL               *ITL               `json:"itl"`
	InputTokens       int                `json:"input_tokens"`
	OutputTokens      int                `json:"output_tokens"`
	Shed              bool               `json:"shed"`
	Retries           int                `json:"retries"`
	CancellationPoint *CancellationPoint `json:"cancellation_point"`
}

// ITL is the inter-chunk latency record (series form; the summary form
// exists in the schema for tools that cannot keep the series, but this
// generator always can, and pooled ITL percentiles require the series).
type ITL struct {
	SeriesSeconds   []float64 `json:"series_seconds"`
	MaxStallSeconds float64   `json:"max_stall_seconds"`
}

// CancellationPoint records where the client canceled.
type CancellationPoint struct {
	ElapsedSeconds       float64 `json:"elapsed_seconds"`
	OutputTokensAtCancel *int    `json:"output_tokens_at_cancel,omitempty"`
}

// Timestamp marshals as RFC 3339 UTC with microsecond precision
// (schema format: date-time).
type Timestamp time.Time

// MarshalJSON implements json.Marshaler.
func (t Timestamp) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Time(t).UTC().Format("2006-01-02T15:04:05.000000Z07:00"))
}

// UnmarshalJSON implements json.Unmarshaler (used by tests and replay).
func (t *Timestamp) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	parsed, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return fmt.Errorf("events: bad timestamp %q: %w", s, err)
	}
	*t = Timestamp(parsed)
	return nil
}
