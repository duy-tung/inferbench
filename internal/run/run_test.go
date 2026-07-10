package run

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/duy-tung/inferbench/internal/client"
	"github.com/duy-tung/inferbench/internal/events"
	"github.com/duy-tung/inferbench/internal/schedule"
	"github.com/duy-tung/inferbench/internal/workload"
)

// okHandler returns a minimal Contract 1 non-stream response after delay.
func okHandler(delay time.Duration) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if delay > 0 {
			select {
			case <-r.Context().Done():
				return
			case <-time.After(delay):
			}
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"x","object":"chat.completion","created":1,"model":"mock-8b",
			"choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],
			"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12}}`)
	}
}

func fixedPlan(n int, gap time.Duration) *schedule.Plan {
	p := &schedule.Plan{Seed: 1, Items: make([]schedule.Item, n)}
	for i := range p.Items {
		p.Items[i] = schedule.Item{
			Index:       i,
			SendOffset:  time.Duration(i) * gap,
			InputTokens: 20,
			MaxTokens:   8,
		}
	}
	return p
}

// THE coordinated-omission safety test (ADR-0001, testing.md layer 3):
// a target that takes 2 seconds per response must not shift a single
// subsequent send time. 20 sends 50ms apart span ~0.95s of schedule; a
// closed-loop (response-coupled) generator would need >= 40s.
func TestSendScheduleIndependentOfResponseLatency(t *testing.T) {
	const responseDelay = 2 * time.Second
	srv := httptest.NewServer(okHandler(responseDelay))
	defer srv.Close()

	plan := fixedPlan(20, 50*time.Millisecond)
	var sink bytes.Buffer
	cl := client.New(client.Config{BaseURL: srv.URL, Model: "mock-8b", RequestTimeout: 10 * time.Second})

	start := time.Now()
	res, err := Execute(context.Background(), Options{
		Plan:       plan,
		Client:     cl,
		RunID:      "co-safety",
		Repetition: 1,
		MaxSlip:    150 * time.Millisecond,
		EventSink:  &sink,
	})
	wall := time.Since(start)
	if err != nil {
		t.Fatalf("run aborted: %v", err)
	}
	if res.Sent != 20 || res.OK != 20 {
		t.Fatalf("sent=%d ok=%d, want 20/20", res.Sent, res.OK)
	}

	// (1) Dispatch slips: every send left the scheduler on time, within
	// tolerance, despite 2s response latencies.
	const tolerance = 150 * time.Millisecond
	for i, slip := range res.DispatchSlips {
		if slip > tolerance {
			t.Errorf("item %d dispatched %s late (tolerance %s): schedule was perturbed", i, slip, tolerance)
		}
	}

	// (2) Wall time ~ schedule span + one response delay, nowhere near the
	// serial (coordinated) 20 * 2s.
	span := plan.Items[len(plan.Items)-1].SendOffset
	if wall > span+responseDelay+2*time.Second {
		t.Errorf("wall %s suggests sends were serialized behind responses", wall)
	}

	// (3) The recorded send_ts values match the precomputed offsets.
	sendByItem := map[int]time.Time{}
	sc := bufio.NewScanner(bytes.NewReader(sink.Bytes()))
	for sc.Scan() {
		var ev events.Event
		if err := json.Unmarshal(sc.Bytes(), &ev); err != nil {
			t.Fatalf("bad event line: %v", err)
		}
		sendByItem[ev.WorkloadItem] = time.Time(ev.SendTS)
	}
	if len(sendByItem) != 20 {
		t.Fatalf("want 20 events, got %d", len(sendByItem))
	}
	for _, it := range plan.Items {
		got := sendByItem[it.Index].Sub(sendByItem[plan.Items[0].Index])
		want := it.SendOffset
		if d := got - want; d < -tolerance || d > tolerance {
			t.Errorf("item %d sent at +%s, scheduled +%s (delta %s)", it.Index, got, want, d)
		}
	}
}

// Same seed => identical send schedule and identical request bodies.
func TestSeedDeterminism(t *testing.T) {
	w := testWorkload(t, 42, 30)

	p1, err := schedule.Build(w)
	if err != nil {
		t.Fatal(err)
	}
	p2, err := schedule.Build(testWorkload(t, 42, 30))
	if err != nil {
		t.Fatal(err)
	}
	if len(p1.Items) != len(p2.Items) {
		t.Fatalf("plan sizes differ: %d vs %d", len(p1.Items), len(p2.Items))
	}
	for i := range p1.Items {
		if p1.Items[i] != p2.Items[i] {
			t.Fatalf("schedules diverge at item %d: %+v vs %+v", i, p1.Items[i], p2.Items[i])
		}
	}

	// Execute both plans; capture request bodies keyed by X-Request-Id.
	bodies := func(p *schedule.Plan) map[string]string {
		var mu sync.Mutex
		captured := map[string]string{}
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			b, _ := io.ReadAll(r.Body)
			mu.Lock()
			captured[r.Header.Get("X-Request-Id")] = string(b)
			mu.Unlock()
			okHandler(0)(w, r)
		}))
		defer srv.Close()
		cl := client.New(client.Config{BaseURL: srv.URL, Model: "mock-8b", RequestTimeout: 5 * time.Second})
		if _, err := Execute(context.Background(), Options{
			Plan: p, Client: cl, RunID: "det", Repetition: 1, EventSink: io.Discard,
		}); err != nil {
			t.Fatalf("run failed: %v", err)
		}
		return captured
	}
	b1 := bodies(p1)
	b2 := bodies(p2)
	if len(b1) != len(p1.Items) || len(b1) != len(b2) {
		t.Fatalf("captured %d/%d bodies for %d items", len(b1), len(b2), len(p1.Items))
	}
	for id, body := range b1 {
		if b2[id] != body {
			t.Fatalf("request %s body differs across same-seed runs", id)
		}
	}

	// Different seed => different schedule.
	p3, err := schedule.Build(testWorkload(t, 43, 30))
	if err != nil {
		t.Fatal(err)
	}
	same := len(p3.Items) == len(p1.Items)
	if same {
		for i := range p1.Items {
			if p1.Items[i] != p3.Items[i] {
				same = false
				break
			}
		}
	}
	if same {
		t.Fatal("different seeds produced identical plans")
	}
}

// The watchdog aborts with the typed reason when the schedule cannot be
// kept; the run is invalid, not silently misleading.
func TestScheduleSlipWatchdog(t *testing.T) {
	srv := httptest.NewServer(okHandler(0))
	defer srv.Close()

	plan := fixedPlan(3, 10*time.Millisecond)
	// Force a slip: the second item is scheduled in the past.
	plan.Items[1].SendOffset = -time.Second

	cl := client.New(client.Config{BaseURL: srv.URL, Model: "mock-8b", RequestTimeout: time.Second})
	var log bytes.Buffer
	_, err := Execute(context.Background(), Options{
		Plan: plan, Client: cl, RunID: "slip", Repetition: 1,
		MaxSlip: 100 * time.Millisecond, EventSink: io.Discard, Log: &log,
	})
	if !errors.Is(err, ErrScheduleSlip) {
		t.Fatalf("want ErrScheduleSlip, got %v", err)
	}
	if !bytes.Contains(log.Bytes(), []byte("ABORT")) {
		t.Fatal("abort not recorded in run log")
	}
}

// Failed requests are classified events; they never abort the run and are
// never retried (each item is sent exactly once).
func TestErrorsAreRecordedNotRetried(t *testing.T) {
	var mu sync.Mutex
	perItem := map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		perItem[r.Header.Get("X-Request-Id")]++
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Retry-After", "1")
		w.WriteHeader(503)
		fmt.Fprint(w, `{"error":{"message":"full","type":"overloaded","code":null,"param":null,"request_id":"r"}}`)
	}))
	defer srv.Close()

	plan := fixedPlan(5, 5*time.Millisecond)
	var sink bytes.Buffer
	cl := client.New(client.Config{BaseURL: srv.URL, Model: "mock-8b", RequestTimeout: time.Second})
	res, err := Execute(context.Background(), Options{
		Plan: plan, Client: cl, RunID: "shed", Repetition: 1, EventSink: &sink,
	})
	if err != nil {
		t.Fatalf("shed storm aborted the run: %v", err)
	}
	if res.Shed != 5 || res.OK != 0 {
		t.Fatalf("want 5 sheds, got %+v", res)
	}
	mu.Lock()
	defer mu.Unlock()
	for id, n := range perItem {
		if n != 1 {
			t.Fatalf("request %s sent %d times: the generator must NEVER retry", id, n)
		}
	}
	sc := bufio.NewScanner(bytes.NewReader(sink.Bytes()))
	for sc.Scan() {
		var ev events.Event
		if err := json.Unmarshal(sc.Bytes(), &ev); err != nil {
			t.Fatal(err)
		}
		if ev.Status != events.StatusShed || !ev.Shed || ev.Retries != 0 ||
			ev.ErrorClass == nil || *ev.ErrorClass != "overloaded" ||
			ev.TTFTSeconds != nil || ev.ITL != nil {
			t.Fatalf("bad shed event: %s", sc.Text())
		}
	}
}

func testWorkload(t *testing.T, seed int64, count int) *workload.Workload {
	t.Helper()
	w, err := workload.Parse([]byte(fmt.Sprintf(`{
		"name": "det-test",
		"version": "0.1.0",
		"seed": %d,
		"arrival_process": {"type": "open-loop-poisson", "rate_rps": 200},
		"input_length_distribution": {"type": "uniform", "min": 16, "max": 64},
		"output_length_distribution": {"type": "constant", "value": 8},
		"prefix_sharing": {"ratio": 0},
		"cancellation": {"rate": 0},
		"slow_client": {"fraction": 0},
		"stop": {"request_count": %d}
	}`, seed, count)))
	if err != nil {
		t.Fatal(err)
	}
	return w
}
