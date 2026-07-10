package events

import (
	"bytes"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"
)

func ts(s string) Timestamp {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		panic(err)
	}
	return Timestamp(t)
}

// Required-but-nullable fields must marshal as explicit null, and all
// required keys must be present on every record.
func TestEventMarshalNulls(t *testing.T) {
	ev := Event{
		RunID:        "run-1",
		Repetition:   1,
		RequestID:    "req-1",
		WorkloadItem: 3,
		SendTS:       ts("2026-07-10T09:01:00.000123Z"),
		EndTS:        ts("2026-07-10T09:01:00.531Z"),
		Status:       StatusShed,
		ErrorClass:   strPtr("overloaded"),
		Shed:         true,
	}
	raw, err := json.Marshal(&ev)
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	for _, want := range []string{
		`"ttft_seconds":null`, `"itl":null`, `"cancellation_point":null`,
		`"error_class":"overloaded"`, `"retries":0`, `"input_tokens":0`,
		`"output_tokens":0`, `"shed":true`,
		`"send_ts":"2026-07-10T09:01:00.000123Z"`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("marshaled event missing %s: %s", want, s)
		}
	}
}

func TestEventMarshalOKWithITL(t *testing.T) {
	ttft := 0.182
	ev := Event{
		RunID: "run-1", Repetition: 1, RequestID: "r", WorkloadItem: 0,
		SendTS: ts("2026-07-10T09:01:00Z"), EndTS: ts("2026-07-10T09:01:01Z"),
		Status: StatusOK, TTFTSeconds: &ttft,
		ITL:         &ITL{SeriesSeconds: []float64{0.02, 0.15}, MaxStallSeconds: 0.15},
		InputTokens: 21, OutputTokens: 3,
	}
	raw, err := json.Marshal(&ev)
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	for _, want := range []string{
		`"error_class":null`, `"ttft_seconds":0.182`,
		`"series_seconds":[0.02,0.15]`, `"max_stall_seconds":0.15`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %s in %s", want, s)
		}
	}
}

// Concurrent producers, one writer: every record is one intact JSON line.
func TestRecorderConcurrent(t *testing.T) {
	var buf bytes.Buffer
	r := NewRecorder(&buf, 8)
	const n = 500
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r.Record(Event{
				RunID: "run", Repetition: 1, RequestID: "req", WorkloadItem: i,
				SendTS: ts("2026-07-10T09:01:00Z"), EndTS: ts("2026-07-10T09:01:01Z"),
				Status: StatusOK,
			})
		}(i)
	}
	wg.Wait()
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != n {
		t.Fatalf("want %d lines, got %d", n, len(lines))
	}
	seen := make(map[int]bool, n)
	for _, line := range lines {
		var ev Event
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatalf("line not valid JSON: %v: %s", err, line)
		}
		seen[ev.WorkloadItem] = true
	}
	if len(seen) != n {
		t.Fatalf("lost events: %d unique of %d", len(seen), n)
	}
}

func strPtr(s string) *string { return &s }
