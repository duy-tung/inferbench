// Package client drives Contract 1 (POST /v1/chat/completions, stream and
// non-stream) and classifies every outcome onto the contract error taxonomy.
//
// Measurement points (client-side mirror series, Contract 2; latency basis
// per raw-event.schema.json v0.2.0 — coordinated-omission safety):
//   - scheduled_send_ts = the schedule-plan send time (Request.ScheduledAt).
//     NORMATIVE basis for client-side TTFT and end-to-end latency
//     (client_ttft_seconds / client_e2e_duration_seconds — the client-side
//     mirror series, named distinctly from the gateway's inference_*):
//     goroutine start, JSON marshal, DNS/TCP/TLS connect, and blocked body
//     writes all count against the latency a request experienced, so a
//     saturated target that slows connection acceptance can never hide
//     that delay from the measurement.
//   - send_ts = request body write completed (httptrace.WroteRequest);
//     diagnostic only. send_ts − scheduled_send_ts is the send slip.
//     FALLBACK SEMANTICS: when the send never completes (WroteRequest never
//     fires — connect failure, or a cancel/timeout before the body write),
//     send_ts falls back to the request-start instant (the best-known lower
//     bound; the schema requires a non-null send_ts) and the outcome carries
//     SendCompleted=false so send_slip_seconds is emitted ABSENT, never as a
//     fabricated ~0 measurement.
//   - client_ttft = scheduled_send_ts -> first response *body* byte at the
//     client, recorded only for 2xx responses (a shed or error body byte is
//     not a token).
//   - client_itl = gaps between content-bearing SSE chunks, stamped when
//     each chunk arrives at the client; max stall is the largest gap.
//
// MONOTONIC CLOCKS: every latency is a time.Time subtraction where both
// stamps come from time.Now() (or arithmetic on such stamps via Add), so Go
// uses the monotonic clock reading and wall-clock steps/skew cannot corrupt
// a measurement. Timestamps are converted to wall-clock strings only at
// event serialization, never fed back into latency arithmetic.
//
// The client NEVER retries (ADR-0001: a retry corrupts the open-loop
// arrival process and hides errors). Transport-level idempotent replays are
// disabled by clearing Request.GetBody.
//
// Deliberate cancellation (IB-T004): a workload cancellation profile plans a
// cancel point per selected request — elapsed-seconds (measured from the
// scheduled send time, the same plan basis as every latency) or
// output-tokens (content deltas observed by the client). The cancel is
// issued by canceling the request context, which closes the connection
// (Contract 1: client disconnect MUST propagate upstream); the event records
// the honest cancellation_point (elapsed after send_ts, per the raw-event
// schema, plus output tokens received when the cancel was issued). A planned
// cancel that loses the race against stream completion is recorded as the
// completed outcome — never back-dated into a fake cancellation.
//
// Slow-client emulation (IB-T004): a planned slow item reads the response
// body through a pacing reader bounded at read_bytes_per_second (optional
// initial read delay), emulating a client that consumes the stream slower
// than the server produces it. All measurements stay honest client-side
// observations: TTFT/ITL then include the self-imposed read pacing.
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
	"sync"
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

// SlowRead is the slow-client profile applied to one request
// (workload.schema.json slow_client).
type SlowRead struct {
	// BytesPerSecond is the sustained response-body read throughput.
	BytesPerSecond int
	// InitialDelay postpones the first body read.
	InitialDelay time.Duration
}

// Request is one planned request.
type Request struct {
	RequestID string
	Prompt    string
	MaxTokens int
	// ScheduledAt is the schedule-plan send time — the latency measurement
	// basis (raw-event scheduled_send_ts). Zero means "now" (ad-hoc use).
	ScheduledAt time.Time
	// CancelAfter, when non-nil, issues a deliberate cancellation at
	// ScheduledAt + CancelAfter (elapsed-seconds trigger; plan basis so the
	// issuance time is response-independent and deterministic).
	CancelAfter *time.Duration
	// CancelAfterTokens, when non-nil, cancels once the client has observed
	// at least this many output tokens (output-tokens trigger; streaming
	// only — enforced by the caller).
	CancelAfterTokens *int
	// SlowRead, when non-nil, bounds the response-body read rate.
	SlowRead *SlowRead
}

// Outcome is the classified result of one request, in raw-event terms.
type Outcome struct {
	// ScheduledAt echoes Request.ScheduledAt (raw-event scheduled_send_ts).
	ScheduledAt time.Time
	// SendCompleted reports whether the request body write finished
	// (httptrace.WroteRequest fired). When false, SendTS is the
	// request-start fallback and SendSlipSeconds is meaningless — the
	// event emits send_slip_seconds ABSENT.
	SendCompleted bool
	// SendSlipSeconds = SendTS - ScheduledAt clamped at 0 (raw-event
	// send_slip_seconds): the wire-level schedule-keeping evidence. Only
	// meaningful when SendCompleted.
	SendSlipSeconds float64
	SendTS          time.Time
	EndTS           time.Time
	Status          string
	ErrorClass      *string // nil exactly when Status == ok
	TTFTSeconds     *float64
	ITL             *events.ITL
	// Cancellation is the deliberate-cancel record (raw-event
	// cancellation_point); non-nil exactly when Status == canceled.
	Cancellation *events.CancellationPoint
	InputTokens  int
	OutputTokens int
	Shed         bool
	// UsageMissing reports that a COMPLETED 2xx response carried no usage
	// payload, so token counts fall back to client-observed chunk counts;
	// surfaced in the run log. (Canceled/failed streams are expected to
	// lack usage and do not set this.)
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
// scheduled send time and (for completed sends) the wire-level send slip.
func finish(out Outcome, scheduledAt time.Time) Outcome {
	out.ScheduledAt = scheduledAt
	if out.SendCompleted {
		if slip := out.SendTS.Sub(scheduledAt).Seconds(); slip > 0 {
			out.SendSlipSeconds = slip
		}
	}
	return out
}

// cancelController owns one request's deliberate-cancel state: it issues at
// most one cancel (elapsed timer or token trigger), remembers when and at
// how many observed output tokens, and ignores triggers that fire after the
// request already settled.
type cancelController struct {
	mu       sync.Mutex
	cancel   context.CancelFunc
	timer    *time.Timer
	tokens   int
	issued   bool
	issuedAt time.Time
	settled  bool
}

// armElapsed schedules a cancel at the absolute instant `at` (plan basis:
// scheduledAt + CancelAfter). A nonpositive delay fires immediately — the
// planned point is already due.
func (cc *cancelController) armElapsed(at time.Time) {
	cc.timer = time.AfterFunc(time.Until(at), cc.fire)
}

// observeTokens updates the client-observed output token count and fires
// the token trigger when the planned threshold is reached. It returns true
// when the cancel was issued by this observation.
func (cc *cancelController) observeTokens(n int, threshold *int) bool {
	cc.mu.Lock()
	defer cc.mu.Unlock()
	cc.tokens = n
	if threshold != nil && n >= *threshold && !cc.issued && !cc.settled {
		cc.issueLocked()
		return true
	}
	return false
}

func (cc *cancelController) fire() {
	cc.mu.Lock()
	defer cc.mu.Unlock()
	if cc.issued || cc.settled {
		return
	}
	cc.issueLocked()
}

func (cc *cancelController) issueLocked() {
	cc.issued = true
	cc.issuedAt = time.Now()
	if cc.cancel != nil {
		cc.cancel()
	}
}

// settle marks the request as settled and stops the timer, so a late timer
// fire cannot record a cancellation that never affected the request.
func (cc *cancelController) settle() {
	cc.mu.Lock()
	cc.settled = true
	if cc.timer != nil {
		cc.timer.Stop()
	}
	cc.mu.Unlock()
}

// wasIssued reports whether a deliberate cancel was issued, with its stamp
// and the output tokens observed at issuance.
func (cc *cancelController) wasIssued() (bool, time.Time, int) {
	cc.mu.Lock()
	defer cc.mu.Unlock()
	return cc.issued, cc.issuedAt, cc.tokens
}

// point builds the raw-event cancellation_point. Elapsed is measured from
// send_ts per the schema ("seconds after send_ts at which the client closed
// the connection"), clamped at 0 for cancels issued before the send
// completed (queued-stage cancels, where send_ts is the request-start
// fallback).
func (cc *cancelController) point(sendTS, fallback time.Time) *events.CancellationPoint {
	issued, at, tokens := cc.wasIssued()
	if !issued {
		// The request was canceled externally (run shutdown / context):
		// record the settle time as the close instant — honest, not planned.
		at = fallback
	}
	elapsed := at.Sub(sendTS).Seconds()
	if elapsed < 0 {
		elapsed = 0
	}
	t := tokens
	return &events.CancellationPoint{
		ElapsedSeconds:       elapsed,
		OutputTokensAtCancel: &t,
	}
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

	cc := &cancelController{cancel: cancel}
	defer cc.settle()
	if req.CancelAfter != nil {
		cc.armElapsed(req.ScheduledAt.Add(*req.CancelAfter))
	}

	start := time.Now()
	// sendTS stays zero until WroteRequest fires. The callback runs on the
	// transport's write goroutine, which can still be live when Do returns
	// early (e.g. a deliberate cancel racing the body write), so the stamp
	// is mutex-guarded.
	var (
		sendMu sync.Mutex
		sendTS time.Time
	)
	trace := &httptrace.ClientTrace{
		WroteRequest: func(httptrace.WroteRequestInfo) {
			sendMu.Lock()
			sendTS = time.Now()
			sendMu.Unlock()
		},
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
	sendMu.Lock()
	sent := sendState{ts: sendTS, start: start}
	sendMu.Unlock()
	if err != nil {
		return transportFailure(ctx, cc, sent, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return c.classifyHTTPError(sent, resp)
	}
	respBody := io.Reader(resp.Body)
	if req.SlowRead != nil {
		respBody = &throttledReader{
			ctx:          ctx,
			r:            resp.Body,
			bps:          req.SlowRead.BytesPerSecond,
			initialDelay: req.SlowRead.InitialDelay,
		}
	}
	if c.cfg.Stream {
		return c.readStream(ctx, cc, req, respBody, sent)
	}
	return c.readNonStream(ctx, cc, req.ScheduledAt, sent, respBody)
}

// sendState carries the wire-write stamp with its fallback semantics: ts is
// zero until httptrace.WroteRequest fires; start is the request-start
// fallback used for send_ts when the send never completed.
type sendState struct {
	ts    time.Time
	start time.Time
}

// completed reports whether the body write finished.
func (s sendState) completed() bool { return !s.ts.IsZero() }

// sendTS is the event send_ts: the wire-write time, or the request-start
// fallback (documented in the package comment) when the send never
// completed.
func (s sendState) sendTS() time.Time {
	if s.completed() {
		return s.ts
	}
	return s.start
}

// readNonStream consumes a non-streaming 2xx response. TTFT is measured
// from scheduledAt (the CO-safe basis), never from send_ts.
func (c *Client) readNonStream(ctx context.Context, cc *cancelController, scheduledAt time.Time, sent sendState, body io.Reader) Outcome {
	fr := &firstByteReader{r: body}
	raw, err := io.ReadAll(io.LimitReader(fr, 8<<20))
	end := time.Now()
	if err != nil {
		return readFailure(ctx, cc, sent, end, err)
	}
	out := Outcome{
		SendTS:        sent.sendTS(),
		SendCompleted: sent.completed(),
		EndTS:         end,
		Status:        events.StatusOK,
		BodySHA256:    sha256Hex(raw),
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
func (c *Client) classifyHTTPError(sent sendState, resp *http.Response) Outcome {
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
		SendTS:        sent.sendTS(),
		SendCompleted: sent.completed(),
		EndTS:         end,
		Status:        events.StatusError,
		ErrorClass:    &class,
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
func transportFailure(ctx context.Context, cc *cancelController, sent sendState, err error) Outcome {
	end := time.Now()
	class := "upstream_error"
	switch {
	case isCtxDeadline(ctx, err):
		class = "upstream_timeout"
	case isCtxCanceled(ctx, err):
		return canceledOutcome(cc, sent, end)
	}
	return Outcome{
		SendTS:        sent.sendTS(),
		SendCompleted: sent.completed(),
		EndTS:         end,
		Status:        events.StatusError,
		ErrorClass:    &class,
	}
}

// readFailure classifies a body-read failure of a 2xx response.
func readFailure(ctx context.Context, cc *cancelController, sent sendState, end time.Time, err error) Outcome {
	base := Outcome{
		SendTS:        sent.sendTS(),
		SendCompleted: sent.completed(),
		EndTS:         end,
		Status:        events.StatusError,
	}
	switch {
	case isCtxDeadline(ctx, err):
		class := "upstream_timeout"
		base.ErrorClass = &class
		return base
	case isCtxCanceled(ctx, err):
		return canceledOutcome(cc, sent, end)
	default:
		class := "upstream_error"
		base.ErrorClass = &class
		return base
	}
}

// isCtxCanceled / isCtxDeadline classify a request-path error against the
// request context (the transport may surface its own wrapper error while
// ctx carries the cause).
func isCtxCanceled(ctx context.Context, err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled)
}

func isCtxDeadline(ctx context.Context, err error) bool {
	return errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded)
}

// canceledOutcome records a canceled request: a deliberate profiled cancel
// (cc issued) or an external context cancel (run shutdown). Either way the
// event carries an honest cancellation_point; output tokens received before
// the cancel are billable (Contract 1) and counted by the caller when known.
func canceledOutcome(cc *cancelController, sent sendState, end time.Time) Outcome {
	class := "canceled"
	point := cc.point(sent.sendTS(), end)
	return Outcome{
		SendTS:        sent.sendTS(),
		SendCompleted: sent.completed(),
		EndTS:         end,
		Status:        events.StatusCanceled,
		ErrorClass:    &class,
		Cancellation:  point,
		OutputTokens:  *point.OutputTokensAtCancel,
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

// throttledReader emulates a slow client: it paces Reads so cumulative
// throughput never exceeds bps, optionally delaying the first read. Pacing
// sleeps are context-aware so cancellation and timeouts still interrupt a
// slow stream immediately.
type throttledReader struct {
	ctx          context.Context
	r            io.Reader
	bps          int
	initialDelay time.Duration
	started      bool
	startAt      time.Time
	consumed     int64
}

func (t *throttledReader) Read(p []byte) (int, error) {
	if !t.started {
		if err := sleepCtx(t.ctx, t.initialDelay); err != nil {
			return 0, err
		}
		t.started = true
		t.startAt = time.Now()
	}
	// Pace: byte N is due at startAt + N/bps. Sleep until the already
	// consumed bytes are due before reading more.
	due := t.startAt.Add(time.Duration(float64(t.consumed) / float64(t.bps) * float64(time.Second)))
	if err := sleepCtx(t.ctx, time.Until(due)); err != nil {
		return 0, err
	}
	// Bound each read so pacing granularity stays ~100ms even against a
	// fast producer (a huge buffer would otherwise allow a burst).
	maxChunk := t.bps / 10
	if maxChunk < 1 {
		maxChunk = 1
	}
	if len(p) > maxChunk {
		p = p[:maxChunk]
	}
	n, err := t.r.Read(p)
	t.consumed += int64(n)
	return n, err
}

// sleepCtx sleeps d (no-op when d <= 0) or returns ctx.Err() early.
func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
