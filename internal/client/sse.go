package client

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"strings"
	"time"

	"github.com/duy-tung/inferbench/internal/events"
)

// readStream consumes a Contract 1 SSE response (`data: <json-chunk>`
// events, terminal `data: [DONE]`, usage in the final chunk when
// stream_options.include_usage=true, mid-stream failures as one
// standardized error-envelope event followed by stream close).
//
// Measurement scope (metrics contract §4/§8, client-side mirror series):
//   - client TTFT = scheduledAt (the CO-safe basis, never send_ts) to first
//     body byte at the client (firstByteReader).
//   - client ITL series = gaps between content-bearing chunks (chunks
//     carrying a non-empty content delta; role-only, usage-only and [DONE]
//     never define gaps — same rule as the gateway series). Each chunk is
//     stamped the moment the scanner returns its line, before any parsing,
//     so parse cost cannot smear a gap. Max stall = largest gap.
//   - All stamps come from time.Now() → monotonic-clock subtraction.
//
// Cancellation (IB-T004): the output-tokens trigger counts content deltas
// as observed by the client (one token per mock/engine content chunk; the
// honest client-side observable) and issues the cancel via the request
// context. An elapsed-trigger cancel lands here as a context-canceled read
// error. Both paths record TTFT/ITL/tokens received before the cancel —
// tokens emitted before cancellation are billable (Contract 1).
func (c *Client) readStream(ctx context.Context, cc *cancelController, req Request, body io.Reader, sent sendState) Outcome {
	fr := &firstByteReader{r: body}
	scanner := bufio.NewScanner(fr)
	scanner.Buffer(make([]byte, 0, 64*1024), 4<<20)

	hash := sha256.New()
	var (
		contentTimes     []time.Time
		usageSeen        bool
		inputTokens      int
		usageOutput      int
		doneSeen         bool
		canceledByTokens bool
		errClass         string
	)

scan:
	for scanner.Scan() {
		arrived := time.Now() // stamp on arrival, before parsing
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue // id: lines, comments, blank separators
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "[DONE]" {
			doneSeen = true
			break scan
		}
		hash.Write([]byte(payload))
		hash.Write([]byte{'\n'})

		// Mid-stream error event: the non-stream error envelope.
		var env errorEnvelope
		if json.Unmarshal([]byte(payload), &env) == nil && env.Error != nil {
			if taxonomy[env.Error.Type] {
				errClass = env.Error.Type
			} else {
				errClass = "upstream_error"
			}
			break scan
		}

		var chunk struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
			Usage *usage `json:"usage"`
		}
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			errClass = "upstream_error" // malformed chunk: contract violation
			break scan
		}
		if chunk.Usage != nil {
			usageSeen = true
			inputTokens = chunk.Usage.PromptTokens
			usageOutput = chunk.Usage.CompletionTokens
		}
		if len(chunk.Choices) > 0 && chunk.Choices[0].Delta.Content != "" {
			contentTimes = append(contentTimes, arrived)
			if cc.observeTokens(len(contentTimes), req.CancelAfterTokens) {
				canceledByTokens = true
				break scan
			}
		}
	}
	end := time.Now()
	scanErr := scanner.Err()

	out := Outcome{
		SendTS:        sent.sendTS(),
		SendCompleted: sent.completed(),
		EndTS:         end,
		Status:        events.StatusOK,
		BodySHA256:    hex.EncodeToString(hash.Sum(nil)),
	}
	if !fr.first.IsZero() {
		out.TTFTSeconds = f64ptr(fr.first.Sub(req.ScheduledAt).Seconds())
	}
	if len(contentTimes) >= 2 {
		itl := &events.ITL{SeriesSeconds: make([]float64, 0, len(contentTimes)-1)}
		for i := 1; i < len(contentTimes); i++ {
			gap := contentTimes[i].Sub(contentTimes[i-1]).Seconds()
			if gap < 0 {
				gap = 0
			}
			itl.SeriesSeconds = append(itl.SeriesSeconds, gap)
			if gap > itl.MaxStallSeconds {
				itl.MaxStallSeconds = gap
			}
		}
		out.ITL = itl
	}
	// Output tokens: server-reported usage when the stream carried it,
	// otherwise the content chunks the client actually received (the honest
	// client-side count; tokens received before a cancel/failure are still
	// tokens received, and billable on cancellation per Contract 1).
	if usageSeen {
		out.InputTokens = inputTokens
		out.OutputTokens = usageOutput
	} else {
		out.OutputTokens = len(contentTimes)
	}

	switch {
	case canceledByTokens:
		return canceledStream(cc, out, sent, end)
	case scanErr != nil:
		switch {
		case isCtxCanceled(ctx, scanErr):
			return canceledStream(cc, out, sent, end)
		case isCtxDeadline(ctx, scanErr):
			class := "upstream_timeout"
			out.Status = events.StatusError
			out.ErrorClass = &class
		default:
			class := "upstream_error"
			out.Status = events.StatusError
			out.ErrorClass = &class
		}
	case errClass != "":
		out.Status = events.StatusError
		out.ErrorClass = &errClass
	case !doneSeen:
		// Stream closed without [DONE] or an error event: contract
		// violation, classified as upstream_error.
		class := "upstream_error"
		out.Status = events.StatusError
		out.ErrorClass = &class
	default:
		if !usageSeen {
			// A COMPLETED stream without usage: flag it (the run summary
			// warns; token counts are client chunk counts).
			out.UsageMissing = true
		}
	}
	return out
}

// canceledStream converts a partially-read stream outcome into the canceled
// record, keeping the measurements taken before the cancel (TTFT, ITL,
// tokens received).
func canceledStream(cc *cancelController, out Outcome, sent sendState, end time.Time) Outcome {
	class := "canceled"
	out.Status = events.StatusCanceled
	out.ErrorClass = &class
	out.Cancellation = cc.point(sent.sendTS(), end)
	return out
}
