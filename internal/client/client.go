// Package client drives Contract 1 (POST /v1/chat/completions, stream and
// non-stream) and classifies every outcome onto the contract error taxonomy.
//
// Measurement points (client-side mirror series, Contract 2; latency basis
// per raw-event.schema.json v0.2.0 — coordinated-omission safety):
//   - scheduled_send_ts = the schedule-plan send time (Request.ScheduledAt).
//     NORMATIVE basis for client-side TTFT and end-to-end latency:
//     goroutine start, JSON marshal, DNS/TCP/TLS connect, and blocked body
//     writes all count against the latency a request experienced, so a
//     saturated target that slows connection acceptance can never hide
//     that delay from the measurement.
//   - send_ts   = request body write completed (httptrace.WroteRequest);
//     diagnostic only. send_ts - scheduled_send_ts is the send slip.
//   - client_ttft = scheduled_send_ts -> first response *body* byte at the
//     client, recorded only for 2xx responses (a shed or error body byte is
//     not a token). Durations use Go's monotonic clock.
//   - client_itl  = gaps between content-bearing SSE chunks; max stall is
//     the largest gap. Refined per-chunk capture is IB-T004.
//
// The client NEVER retries (ADR-0001: a retry corrupts the open-loop
// arrival process and hides errors). Transport-level idempotent replays are
// disabled by clearing Request.GetBody.
package client

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptrace"
	"time"

	"github.com/duy-tung/inferbench/internal/events"
)

// Contract 1 error taxonomy values (raw-event error_class enum).
var taxonomy = map[string]bool{
	"invalid_request": true, "authentication": true, "permission": true,
	"not_found": true, "rate_limited": true, "overloaded": true,
	"upstream_error": true, "upstream_timeout": true, "canceled": true,
	"internal": true,
}

// Config configures the client for one run.
type Config struct {
	// BaseURL is the target root (e.g. http://127.0.0.1:8080).
	BaseURL string
	// Model is the model field of every request.
	Model string
	// Stream selects SSE streaming requests (stream_options.include_usage
	// is always set on streaming requests so output token counts are
	// server-reported, not fabricated).
	Stream bool
	// RequestTimeout bounds one request end-to-end (client-imposed).
	RequestTimeout time.Duration
	// HTTPClient defaults to a transport provisioned so that connection
	// setup cannot serialize sends (ADR-0001 §5).
	HTTPClient *http.Client
}

// Request is one planned request.
type Request struct {
	RequestID string
	Prompt    string
	MaxTokens int
	// ScheduledAt is the schedule-plan send time — the latency measurement
	// basis (raw-event scheduled_send_ts). Zero means "now" (ad-hoc use).
	ScheduledAt time.Time
}

// Outcome is the classified result of one request, in raw-event terms.
type Outcome struct {
	// ScheduledAt echoes Request.ScheduledAt (raw-event scheduled_send_ts).
	ScheduledAt time.Time
	// SendSlipSeconds = SendTS - ScheduledAt clamped at 0 (raw-event
	// send_slip_seconds): the wire-level schedule-keeping evidence.
	SendSlipSeconds float64
	SendTS          time.Time
	EndTS           time.Time
	Status          string
	ErrorClass      *string // nil exactly when Status == ok
	TTFTSeconds     *float64
	ITL             *events.ITL
	InputTokens     int
	OutputTokens    int
	Shed            bool
	// UsageMissing reports that a 2xx response carried no usage payload,
	// so token counts are 0 ("not measured"); surfaced in the run log.
	UsageMissing bool
	// BodySHA256 is the hash of the response content (non-stream: raw
	// body; stream: concatenated data payloads) — determinism evidence
	// for replay verification, never published in events.
	BodySHA256 string
}

// Client issues Contract 1 requests.
type Client struct {
	cfg Config
}

// New builds a Client.
func New(cfg Config) *Client {
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{
			Transport: &http.Transport{
				MaxIdleConns:        0,
				MaxIdleConnsPerHost: 1024,
				MaxConnsPerHost:     0, // unlimited: the pool must never gate the schedule
			},
		}
	}
	if cfg.RequestTimeout <= 0 {
		cfg.RequestTimeout = 60 * time.Second
	}
	return &Client{cfg: cfg}
}

// chatRequest is the Contract 1 request subset this generator sends.
type chatRequest struct {
	Model         string        `json:"model"`
	Messages      []chatMessage `json:"messages"`
	MaxTokens     int           `json:"max_tokens"`
	Stream        bool          `json:"stream,omitempty"`
	StreamOptions *streamOpts   `json:"stream_options,omitempty"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type streamOpts struct {
	IncludeUsage bool `json:"include_usage"`
}

type usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type errorEnvelope struct {
	Error *struct {
		Message   string `json:"message"`
		Type      string `json:"type"`
		Code      any    `json:"code"`
		Param     any    `json:"param"`
		RequestID string `json:"request_id"`
	} `json:"error"`
}

// Do issues one request and always returns a classified Outcome; failures
// are recorded, never retried and never fatal to the run.
func (c *Client) Do(ctx context.Context, req Request) Outcome {
	if req.ScheduledAt.IsZero() {
		req.ScheduledAt = time.Now()
	}
	return finish(c.do(ctx, req), req.ScheduledAt)
}

// finish stamps the latency measurement basis onto an outcome: the
// scheduled send time and the wire-level send slip.
func finish(out Outcome, scheduledAt time.Time) Outcome {
	out.ScheduledAt = scheduledAt
	if slip := out.SendTS.Sub(scheduledAt).Seconds(); slip > 0 {
		out.SendSlipSeconds = slip
	}
	return out
}

func (c *Client) do(ctx context.Context, req Request) Outcome {
	body := chatRequest{
		Model:     c.cfg.Model,
		Messages:  []chatMessage{{Role: "user", Content: req.Prompt}},
		MaxTokens: req.MaxTokens,
	}
	if c.cfg.Stream {
		body.Stream = true
		body.StreamOptions = &streamOpts{IncludeUsage: true}
	}
	payload, err := json.Marshal(&body)
	if err != nil { // unreachable for this struct; keep the taxonomy honest
		return failedBefore(time.Now(), "internal")
	}

	ctx, cancel := context.WithTimeout(ctx, c.cfg.RequestTimeout)
	defer cancel()

	start := time.Now()
	sendTS := start
	trace := &httptrace.ClientTrace{
		WroteRequest: func(httptrace.WroteRequestInfo) { sendTS = time.Now() },
	}
	hreq, err := http.NewRequestWithContext(
		httptrace.WithClientTrace(ctx, trace),
		http.MethodPost, c.cfg.BaseURL+"/v1/chat/completions",
		bytes.NewReader(payload))
	if err != nil {
		return failedBefore(start, "internal")
	}
	// NO client-side retries: without GetBody the transport cannot replay
	// the request on a dead reused connection.
	hreq.GetBody = nil
	hreq.Header.Set("Content-Type", "application/json")
	hreq.Header.Set("X-Request-Id", req.RequestID)

	resp, err := c.cfg.HTTPClient.Do(hreq)
	if err != nil {
		return transportFailure(ctx, sendTS, start, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return c.classifyHTTPError(sendTS, resp)
	}
	if c.cfg.Stream {
		return c.readStream(ctx, req.ScheduledAt, sendTS, resp)
	}
	return c.readNonStream(ctx, req.ScheduledAt, sendTS, resp)
}

// readNonStream consumes a non-streaming 2xx response. TTFT is measured
// from scheduledAt (the CO-safe basis), never from sendTS.
func (c *Client) readNonStream(ctx context.Context, scheduledAt, sendTS time.Time, resp *http.Response) Outcome {
	fr := &firstByteReader{r: resp.Body}
	raw, err := io.ReadAll(io.LimitReader(fr, 8<<20))
	end := time.Now()
	if err != nil {
		return readFailure(ctx, sendTS, end, err)
	}
	out := Outcome{
		SendTS:     sendTS,
		EndTS:      end,
		Status:     events.StatusOK,
		BodySHA256: sha256Hex(raw),
	}
	if !fr.first.IsZero() {
		out.TTFTSeconds = f64ptr(fr.first.Sub(scheduledAt).Seconds())
	}
	var parsed struct {
		Usage *usage `json:"usage"`
	}
	if json.Unmarshal(raw, &parsed) == nil && parsed.Usage != nil {
		out.InputTokens = parsed.Usage.PromptTokens
		out.OutputTokens = parsed.Usage.CompletionTokens
	} else {
		out.UsageMissing = true
	}
	return out
}

// classifyHTTPError maps a non-2xx response onto the taxonomy: prefer the
// contract error envelope's type; fall back to the HTTP status.
func (c *Client) classifyHTTPError(sendTS time.Time, resp *http.Response) Outcome {
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	end := time.Now()

	class := ""
	var env errorEnvelope
	if json.Unmarshal(raw, &env) == nil && env.Error != nil && taxonomy[env.Error.Type] {
		class = env.Error.Type
	}
	if class == "" {
		class = classFromStatus(resp.StatusCode)
	}

	out := Outcome{
		SendTS:     sendTS,
		EndTS:      end,
		Status:     events.StatusError,
		ErrorClass: &class,
	}
	// Shed = typed 429/503 rejection before any output (raw-event schema).
	if class == "rate_limited" || class == "overloaded" {
		out.Status = events.StatusShed
		out.Shed = true
	}
	return out
}

// transportFailure classifies an error where no HTTP response arrived.
// The taxonomy is target-reported by design; for client-observed transport
// failures we use the closest classes: upstream_timeout for the
// client-imposed deadline, canceled for context cancellation, and
// upstream_error for connection-level failures (from the measurement's
// perspective the whole target is the upstream). Recorded in
// docs/implementation-notes.md.
func transportFailure(ctx context.Context, sendTS, start time.Time, err error) Outcome {
	if sendTS.Before(start) || sendTS.Equal(start) {
		// WroteRequest never fired; the failure precedes body-write
		// completion. send_ts falls back to request start.
		sendTS = start
	}
	end := time.Now()
	class := "upstream_error"
	switch {
	case errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded):
		class = "upstream_timeout"
	case errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled):
		return canceledOutcome(sendTS, end, 0)
	}
	return Outcome{
		SendTS:     sendTS,
		EndTS:      end,
		Status:     events.StatusError,
		ErrorClass: &class,
	}
}

// readFailure classifies a body-read failure of a 2xx response.
func readFailure(ctx context.Context, sendTS, end time.Time, err error) Outcome {
	switch {
	case errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded):
		class := "upstream_timeout"
		return Outcome{SendTS: sendTS, EndTS: end, Status: events.StatusError, ErrorClass: &class}
	case errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled):
		return canceledOutcome(sendTS, end, 0)
	default:
		class := "upstream_error"
		return Outcome{SendTS: sendTS, EndTS: end, Status: events.StatusError, ErrorClass: &class}
	}
}

func canceledOutcome(sendTS, end time.Time, outputTokens int) Outcome {
	class := "canceled"
	return Outcome{
		SendTS:       sendTS,
		EndTS:        end,
		Status:       events.StatusCanceled,
		ErrorClass:   &class,
		OutputTokens: outputTokens,
	}
}

func failedBefore(ts time.Time, class string) Outcome {
	return Outcome{
		SendTS:     ts,
		EndTS:      time.Now(),
		Status:     events.StatusError,
		ErrorClass: &class,
	}
}

func classFromStatus(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "invalid_request"
	case http.StatusUnauthorized:
		return "authentication"
	case http.StatusForbidden:
		return "permission"
	case http.StatusNotFound:
		return "not_found"
	case http.StatusTooManyRequests:
		return "rate_limited"
	case http.StatusServiceUnavailable:
		return "overloaded"
	case http.StatusGatewayTimeout:
		return "upstream_timeout"
	case http.StatusBadGateway:
		return "upstream_error"
	default:
		if status >= 500 {
			return "internal"
		}
		return "invalid_request"
	}
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func f64ptr(v float64) *float64 {
	if v < 0 {
		v = 0
	}
	return &v
}

// firstByteReader timestamps the first successful Read of the response body.
type firstByteReader struct {
	r     io.Reader
	first time.Time
}

func (f *firstByteReader) Read(p []byte) (int, error) {
	n, err := f.r.Read(p)
	if n > 0 && f.first.IsZero() {
		f.first = time.Now()
	}
	return n, err
}
