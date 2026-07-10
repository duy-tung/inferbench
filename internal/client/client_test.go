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
