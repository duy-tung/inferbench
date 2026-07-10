package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/duy-tung/inferbench/internal/events"
)

func newClient(url string, stream bool, timeout time.Duration) *Client {
	return New(Config{BaseURL: url, Model: "mock-8b", Stream: stream, RequestTimeout: timeout})
}

func do(t *testing.T, c *Client) Outcome {
	t.Helper()
	return c.Do(context.Background(), Request{RequestID: "req-test-1", Prompt: "hello", MaxTokens: 16})
}

func TestNonStreamOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Request-Id") != "req-test-1" {
			t.Errorf("X-Request-Id not sent")
		}
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req["stream"] == true {
			t.Errorf("non-stream client sent stream: true")
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"chatcmpl-x","object":"chat.completion","created":1,"model":"mock-8b",
			"choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],
			"usage":{"prompt_tokens":21,"completion_tokens":5,"total_tokens":26}}`)
	}))
	defer srv.Close()

	out := do(t, newClient(srv.URL, false, time.Second))
	if out.Status != events.StatusOK || out.ErrorClass != nil {
		t.Fatalf("want ok, got %s (%v)", out.Status, out.ErrorClass)
	}
	if out.InputTokens != 21 || out.OutputTokens != 5 || out.UsageMissing {
		t.Fatalf("usage not captured: %+v", out)
	}
	if out.TTFTSeconds == nil || *out.TTFTSeconds < 0 {
		t.Fatal("ttft missing on 2xx with body")
	}
	if out.ITL != nil {
		t.Fatal("non-stream response must carry null itl (<2 content chunks)")
	}
	if !out.EndTS.After(out.SendTS) && !out.EndTS.Equal(out.SendTS) {
		t.Fatal("end_ts before send_ts")
	}
	if out.BodySHA256 == "" {
		t.Fatal("determinism hash missing")
	}
}

func TestShedClassification(t *testing.T) {
	cases := []struct {
		status int
		body   string
		class  string
	}{
		{429, `{"error":{"message":"slow down","type":"rate_limited","code":null,"param":null,"request_id":"r"}}`, "rate_limited"},
		{503, `{"error":{"message":"full","type":"overloaded","code":null,"param":null,"request_id":"r"}}`, "overloaded"},
	}
	for _, tc := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(tc.status)
			fmt.Fprint(w, tc.body)
		}))
		out := do(t, newClient(srv.URL, false, time.Second))
		srv.Close()
		if out.Status != events.StatusShed || !out.Shed {
			t.Fatalf("%d: want shed, got %s", tc.status, out.Status)
		}
		if out.ErrorClass == nil || *out.ErrorClass != tc.class {
			t.Fatalf("%d: want %s, got %v", tc.status, tc.class, out.ErrorClass)
		}
		if out.TTFTSeconds != nil || out.ITL != nil {
			t.Fatalf("%d: shed must carry null ttft and itl", tc.status)
		}
	}
}

func TestErrorEnvelopeClassification(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(400)
		fmt.Fprint(w, `{"error":{"message":"bad","type":"invalid_request","code":"unsupported_value","param":"stream","request_id":"r"}}`)
	}))
	defer srv.Close()
	out := do(t, newClient(srv.URL, false, time.Second))
	if out.Status != events.StatusError || out.ErrorClass == nil || *out.ErrorClass != "invalid_request" {
		t.Fatalf("want error/invalid_request, got %s/%v", out.Status, out.ErrorClass)
	}
	if out.Shed {
		t.Fatal("invalid_request is not a shed")
	}
}

func TestStatusFallbackWhenEnvelopeUnparseable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(502)
		fmt.Fprint(w, "bad gateway")
	}))
	defer srv.Close()
	out := do(t, newClient(srv.URL, false, time.Second))
	if out.ErrorClass == nil || *out.ErrorClass != "upstream_error" {
		t.Fatalf("want upstream_error fallback, got %v", out.ErrorClass)
	}
}

func TestTransportFailure(t *testing.T) {
	// Point at a closed port.
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close()
	out := do(t, newClient(url, false, time.Second))
	if out.Status != events.StatusError || out.ErrorClass == nil || *out.ErrorClass != "upstream_error" {
		t.Fatalf("want error/upstream_error, got %s/%v", out.Status, out.ErrorClass)
	}
}

func TestClientTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	}))
	defer srv.Close()
	out := do(t, newClient(srv.URL, false, 100*time.Millisecond))
	if out.Status != events.StatusError || out.ErrorClass == nil || *out.ErrorClass != "upstream_timeout" {
		t.Fatalf("want error/upstream_timeout, got %s/%v", out.Status, out.ErrorClass)
	}
}

// sseHandler emits a Contract 1 stream: role chunk, N content chunks with
// the given gaps, finish chunk, optional usage chunk, then [DONE].
func sseHandler(t *testing.T, gaps []time.Duration, includeUsage bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req["stream"] != true {
			t.Errorf("streaming client did not set stream: true")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		f := w.(http.Flusher)
		id := 0
		emit := func(payload string) {
			fmt.Fprintf(w, "id: %d\ndata: %s\n\n", id, payload)
			id++
			f.Flush()
		}
		emit(`{"id":"c","object":"chat.completion.chunk","created":1,"model":"mock-8b","choices":[{"index":0,"delta":{"role":"assistant","content":""}}]}`)
		for i, gap := range gaps {
			time.Sleep(gap)
			emit(fmt.Sprintf(`{"id":"c","object":"chat.completion.chunk","created":1,"model":"mock-8b","choices":[{"index":0,"delta":{"content":"tok%d "}}]}`, i))
		}
		emit(`{"id":"c","object":"chat.completion.chunk","created":1,"model":"mock-8b","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`)
		if includeUsage {
			emit(`{"id":"c","object":"chat.completion.chunk","created":1,"model":"mock-8b","choices":[],"usage":{"prompt_tokens":21,"completion_tokens":4,"total_tokens":25}}`)
		}
		emit("[DONE]")
	}
}

func TestStreamOK(t *testing.T) {
	gaps := []time.Duration{
		10 * time.Millisecond, 20 * time.Millisecond,
		60 * time.Millisecond, 20 * time.Millisecond,
	}
	srv := httptest.NewServer(sseHandler(t, gaps, true))
	defer srv.Close()

	out := do(t, newClient(srv.URL, true, 5*time.Second))
	if out.Status != events.StatusOK || out.ErrorClass != nil {
		t.Fatalf("want ok, got %s (%v)", out.Status, out.ErrorClass)
	}
	if out.TTFTSeconds == nil || *out.TTFTSeconds <= 0 {
		t.Fatal("stream ttft missing")
	}
	if out.ITL == nil || len(out.ITL.SeriesSeconds) != 3 {
		t.Fatalf("want 3 inter-chunk gaps for 4 content chunks, got %+v", out.ITL)
	}
	// The 60ms injected stall must dominate max_stall (loose bounds; exact
	// calibration is IB-T004).
	if out.ITL.MaxStallSeconds < 0.03 || out.ITL.MaxStallSeconds > 0.5 {
		t.Fatalf("max stall %.3fs, want ~0.06s", out.ITL.MaxStallSeconds)
	}
	if out.InputTokens != 21 || out.OutputTokens != 4 || out.UsageMissing {
		t.Fatalf("usage chunk not captured: %+v", out)
	}
}

func TestStreamWithoutUsageCountsChunks(t *testing.T) {
	srv := httptest.NewServer(sseHandler(t, []time.Duration{time.Millisecond, time.Millisecond}, false))
	defer srv.Close()
	out := do(t, newClient(srv.URL, true, 5*time.Second))
	if out.Status != events.StatusOK {
		t.Fatalf("want ok, got %s", out.Status)
	}
	if !out.UsageMissing || out.OutputTokens != 2 {
		t.Fatalf("want usage-missing with chunk-count fallback 2, got %+v", out)
	}
}

func TestStreamMidStreamErrorEvent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		f := w.(http.Flusher)
		fmt.Fprint(w, "id: 0\ndata: {\"id\":\"c\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"m\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"tok\"}}]}\n\n")
		f.Flush()
		fmt.Fprint(w, "id: 1\ndata: {\"error\":{\"message\":\"backend died\",\"type\":\"upstream_error\",\"code\":null,\"param\":null,\"request_id\":\"r\"}}\n\n")
		f.Flush()
		// Stream closes; no [DONE] after an error event.
	}))
	defer srv.Close()
	out := do(t, newClient(srv.URL, true, 5*time.Second))
	if out.Status != events.StatusError || out.ErrorClass == nil || *out.ErrorClass != "upstream_error" {
		t.Fatalf("want error/upstream_error from SSE error event, got %s/%v", out.Status, out.ErrorClass)
	}
}

func TestStreamTruncatedWithoutDone(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		f := w.(http.Flusher)
		fmt.Fprint(w, "id: 0\ndata: {\"id\":\"c\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"m\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"tok\"}}]}\n\n")
		f.Flush()
	}))
	defer srv.Close()
	out := do(t, newClient(srv.URL, true, 5*time.Second))
	if out.Status != events.StatusError || out.ErrorClass == nil || *out.ErrorClass != "upstream_error" {
		t.Fatalf("stream without terminal event must classify upstream_error, got %s/%v", out.Status, out.ErrorClass)
	}
}

// TTFT and slip are measured from the scheduled send time, not wire-write:
// a request scheduled 500ms before it could actually run reports >= 500ms
// of TTFT and send slip (coordinated-omission safety).
func TestLatencyBasisIsScheduledSend(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"x","object":"chat.completion","created":1,"model":"mock-8b",
			"choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],
			"usage":{"prompt_tokens":3,"completion_tokens":1,"total_tokens":4}}`)
	}))
	defer srv.Close()

	c := newClient(srv.URL, false, time.Second)
	scheduled := time.Now().Add(-500 * time.Millisecond)
	out := c.Do(context.Background(), Request{
		RequestID: "r", Prompt: "hi", MaxTokens: 4, ScheduledAt: scheduled,
	})
	if out.Status != events.StatusOK {
		t.Fatalf("want ok, got %s", out.Status)
	}
	if !out.ScheduledAt.Equal(scheduled) {
		t.Fatal("outcome must echo the scheduled send time")
	}
	if out.TTFTSeconds == nil || *out.TTFTSeconds < 0.5 {
		t.Fatalf("ttft %v must include the 500ms pre-send delay (basis = scheduled_send_ts)", out.TTFTSeconds)
	}
	if out.SendSlipSeconds < 0.5 {
		t.Fatalf("send slip %v must report the 500ms delay", out.SendSlipSeconds)
	}
	if !out.SendTS.After(scheduled) {
		t.Fatal("send_ts (wire write) must remain the actual send time")
	}
}

// pacedSSEHandler streams content chunks forever (until the client goes
// away), one every gap, after an initial ttft delay. It reports how many
// chunks it wrote and signals when the client disconnected.
func pacedSSEHandler(ttft, gap time.Duration, maxChunks int, disconnected chan<- int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		f := w.(http.Flusher)
		sent := 0
		defer func() {
			if disconnected != nil {
				disconnected <- sent
			}
		}()
		select {
		case <-r.Context().Done():
			return
		case <-time.After(ttft):
		}
		for i := 0; maxChunks <= 0 || i < maxChunks; i++ {
			if i > 0 {
				select {
				case <-r.Context().Done():
					return
				case <-time.After(gap):
				}
			}
			if _, err := fmt.Fprintf(w,
				`data: {"id":"c","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"content":"tok%d "}}]}`+"\n\n", i); err != nil {
				return
			}
			f.Flush()
			sent++
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
		f.Flush()
	}
}

// Deliberate cancel before the first token: the profiled elapsed point
// fires while the server is still "prefilling". Honest record: status
// canceled, zero tokens at cancel, no TTFT (no body byte arrived).
func TestDeliberateCancelPreFirstToken(t *testing.T) {
	srv := httptest.NewServer(pacedSSEHandler(2*time.Second, 10*time.Millisecond, 0, nil))
	defer srv.Close()

	c := newClient(srv.URL, true, 10*time.Second)
	after := 80 * time.Millisecond
	start := time.Now()
	out := c.Do(context.Background(), Request{
		RequestID: "cancel-pre", Prompt: "hi", MaxTokens: 64,
		ScheduledAt: start, CancelAfter: &after,
	})
	if out.Status != events.StatusCanceled || out.ErrorClass == nil || *out.ErrorClass != "canceled" {
		t.Fatalf("want canceled, got %s/%v", out.Status, out.ErrorClass)
	}
	if out.Cancellation == nil {
		t.Fatal("deliberate cancel must record cancellation_point")
	}
	if out.Cancellation.OutputTokensAtCancel == nil || *out.Cancellation.OutputTokensAtCancel != 0 {
		t.Fatalf("pre-first-token cancel: tokens at cancel %v, want 0", out.Cancellation.OutputTokensAtCancel)
	}
	if out.TTFTSeconds != nil {
		t.Fatalf("no body byte arrived; ttft must be nil, got %v", out.TTFTSeconds)
	}
	// Issued at ~80ms, well before the 2s first token; elapsed recorded
	// relative to send_ts (schema), so slightly under the 80ms plan point.
	if e := out.Cancellation.ElapsedSeconds; e > 1.0 {
		t.Fatalf("cancel issued at %.3fs, want ~0.08s", e)
	}
	if wall := time.Since(start); wall > time.Second {
		t.Fatalf("cancel did not interrupt the stream (wall %s)", wall)
	}
}

// Deliberate mid-stream cancel via the elapsed trigger: measurements taken
// before the cancel (TTFT, ITL, received tokens) are kept — tokens emitted
// before cancellation are billable (Contract 1).
func TestDeliberateCancelMidStreamElapsed(t *testing.T) {
	srv := httptest.NewServer(pacedSSEHandler(20*time.Millisecond, 20*time.Millisecond, 0, nil))
	defer srv.Close()

	c := newClient(srv.URL, true, 10*time.Second)
	after := 400 * time.Millisecond
	out := c.Do(context.Background(), Request{
		RequestID: "cancel-mid", Prompt: "hi", MaxTokens: 64,
		ScheduledAt: time.Now(), CancelAfter: &after,
	})
	if out.Status != events.StatusCanceled {
		t.Fatalf("want canceled, got %s (%v)", out.Status, out.ErrorClass)
	}
	if out.Cancellation == nil || out.Cancellation.OutputTokensAtCancel == nil {
		t.Fatal("cancellation_point with token count required")
	}
	if got := *out.Cancellation.OutputTokensAtCancel; got < 5 || got > 40 {
		t.Fatalf("tokens at cancel %d, want mid-stream (~19 at 20ms cadence)", got)
	}
	if out.OutputTokens != *out.Cancellation.OutputTokensAtCancel {
		t.Fatalf("billable output tokens %d != tokens at cancel %d",
			out.OutputTokens, *out.Cancellation.OutputTokensAtCancel)
	}
	if out.TTFTSeconds == nil {
		t.Fatal("mid-stream cancel must keep the measured TTFT")
	}
	if out.ITL == nil || len(out.ITL.SeriesSeconds) < 4 {
		t.Fatalf("mid-stream cancel must keep the ITL series, got %+v", out.ITL)
	}
	if e := out.Cancellation.ElapsedSeconds; e < 0.3 || e > 0.8 {
		t.Fatalf("cancel elapsed %.3fs, want ~0.4s", e)
	}
}

// Deliberate cancel via the output-tokens trigger: the client counts
// content deltas and cancels at the threshold; the server observes the
// disconnect (cancellation propagates per Contract 1).
func TestDeliberateCancelAfterTokens(t *testing.T) {
	disconnected := make(chan int, 1)
	srv := httptest.NewServer(pacedSSEHandler(5*time.Millisecond, 5*time.Millisecond, 0, disconnected))
	defer srv.Close()

	c := newClient(srv.URL, true, 10*time.Second)
	n := 8
	out := c.Do(context.Background(), Request{
		RequestID: "cancel-tokens", Prompt: "hi", MaxTokens: 64,
		ScheduledAt: time.Now(), CancelAfterTokens: &n,
	})
	if out.Status != events.StatusCanceled {
		t.Fatalf("want canceled, got %s (%v)", out.Status, out.ErrorClass)
	}
	if out.Cancellation == nil || out.Cancellation.OutputTokensAtCancel == nil ||
		*out.Cancellation.OutputTokensAtCancel != 8 {
		t.Fatalf("want exactly 8 tokens at cancel, got %+v", out.Cancellation)
	}
	if out.OutputTokens != 8 {
		t.Fatalf("billable tokens %d, want 8", out.OutputTokens)
	}
	select {
	case sent := <-disconnected:
		if sent < 8 {
			t.Fatalf("server wrote only %d chunks before disconnect; client counted 8", sent)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server never observed the client disconnect (cancel did not propagate)")
	}
}

// A planned cancel that loses the race against stream completion is
// recorded as the completed outcome — never a back-dated cancellation.
func TestCancelAfterCompletionIsOK(t *testing.T) {
	srv := httptest.NewServer(sseHandler(t, []time.Duration{time.Millisecond, time.Millisecond}, true))
	defer srv.Close()
	c := newClient(srv.URL, true, 5*time.Second)
	after := 3 * time.Second // fires long after the stream ends
	out := c.Do(context.Background(), Request{
		RequestID: "cancel-late", Prompt: "hi", MaxTokens: 8,
		ScheduledAt: time.Now(), CancelAfter: &after,
	})
	if out.Status != events.StatusOK || out.Cancellation != nil {
		t.Fatalf("completed stream must stay ok with no cancellation_point, got %s %+v",
			out.Status, out.Cancellation)
	}
}

// Slow-client emulation: reads are paced at the profiled byte rate, so a
// response the server produces instantly takes >= bytes/rate to consume,
// plus the initial read delay. The e2e window (and TTFT, via the delayed
// first read) honestly includes the self-imposed pacing.
func TestSlowClientBoundedReadRate(t *testing.T) {
	const chunks = 30
	srv := httptest.NewServer(sseHandler(t, make([]time.Duration, chunks), true))
	defer srv.Close()

	// Measure the full-speed baseline body size via a fast request.
	c := newClient(srv.URL, true, 30*time.Second)
	fast := c.Do(context.Background(), Request{
		RequestID: "fast", Prompt: "hi", MaxTokens: 64, ScheduledAt: time.Now(),
	})
	if fast.Status != events.StatusOK {
		t.Fatalf("baseline: %s", fast.Status)
	}

	// ~30 content chunks x ~120 B ≈ 4+ KB of SSE payload; at 4096 B/s the
	// paced read needs >= ~1s; the initial delay adds 0.3s.
	scheduled := time.Now()
	out := c.Do(context.Background(), Request{
		RequestID: "slow", Prompt: "hi", MaxTokens: 64, ScheduledAt: scheduled,
		SlowRead: &SlowRead{BytesPerSecond: 4096, InitialDelay: 300 * time.Millisecond},
	})
	if out.Status != events.StatusOK {
		t.Fatalf("slow read must still complete: %s (%v)", out.Status, out.ErrorClass)
	}
	if out.BodySHA256 != fast.BodySHA256 {
		t.Fatal("throttling must not alter the received body")
	}
	e2e := out.EndTS.Sub(scheduled).Seconds()
	if e2e < 1.0 {
		t.Fatalf("e2e %.3fs too fast for 4KB at 4096 B/s + 0.3s delay (pacing not applied)", e2e)
	}
	if out.TTFTSeconds == nil || *out.TTFTSeconds < 0.3 {
		t.Fatalf("ttft %v must include the initial read delay (client-side series is honest)", out.TTFTSeconds)
	}
}

// When the send never completes (connect failure), send_slip_seconds must
// be ABSENT — SendCompleted=false with send_ts falling back to the request
// start (CO re-review residual).
func TestSendSlipAbsentWhenSendNeverCompletes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close() // connection refused: WroteRequest never fires

	c := newClient(url, false, time.Second)
	scheduled := time.Now()
	out := c.Do(context.Background(), Request{
		RequestID: "refused", Prompt: "hi", MaxTokens: 4, ScheduledAt: scheduled,
	})
	if out.Status != events.StatusError {
		t.Fatalf("want error, got %s", out.Status)
	}
	if out.SendCompleted {
		t.Fatal("connect failure: SendCompleted must be false")
	}
	if out.SendSlipSeconds != 0 {
		t.Fatalf("send slip %v fabricated for a send that never happened", out.SendSlipSeconds)
	}
	if out.SendTS.IsZero() || out.SendTS.Before(scheduled) {
		t.Fatalf("send_ts fallback must be the request start, got %v", out.SendTS)
	}
}

// A cancel profiled at 0s (queued stage) races the dispatch itself: the
// request must settle as canceled with no fabricated send slip, race-free
// (the WroteRequest stamp is written by the transport's write goroutine).
func TestDeliberateCancelAtDispatch(t *testing.T) {
	srv := httptest.NewServer(pacedSSEHandler(time.Second, 10*time.Millisecond, 0, nil))
	defer srv.Close()
	c := newClient(srv.URL, true, 10*time.Second)
	zero := time.Duration(0)
	for i := 0; i < 20; i++ {
		out := c.Do(context.Background(), Request{
			RequestID: fmt.Sprintf("cancel-dispatch-%d", i), Prompt: "hi", MaxTokens: 8,
			ScheduledAt: time.Now(), CancelAfter: &zero,
		})
		if out.Status != events.StatusCanceled {
			t.Fatalf("want canceled, got %s (%v)", out.Status, out.ErrorClass)
		}
		if out.Cancellation == nil {
			t.Fatal("cancellation_point required")
		}
		if !out.SendCompleted && out.SendSlipSeconds != 0 {
			t.Fatalf("send never completed but slip %v fabricated", out.SendSlipSeconds)
		}
	}
}
