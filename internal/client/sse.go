package client

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/duy-tung/inferbench/internal/events"
)

// readStream consumes a Contract 1 SSE response (`data: <json-chunk>`
// events, terminal `data: [DONE]`, usage in the final chunk when
// stream_options.include_usage=true, mid-stream failures as one
// standardized error-envelope event followed by stream close).
//
// Measurement scope: TTFT = scheduledAt (the CO-safe basis, never sendTS)
// to first body byte; ITL series = gaps between content-bearing chunks;
// max stall = largest gap. Finer-grained capture and calibration are
// IB-T004.
func (c *Client) readStream(ctx context.Context, scheduledAt, sendTS time.Time, resp *http.Response) Outcome {
	fr := &firstByteReader{r: resp.Body}
	scanner := bufio.NewScanner(fr)
	scanner.Buffer(make([]byte, 0, 64*1024), 4<<20)

	hash := sha256.New()
	var (
		contentTimes []time.Time
		outputTokens int
		usageSeen    bool
		inputTokens  int
		doneSeen     bool
		errClass     string
	)

scan:
	for scanner.Scan() {
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
			outputTokens = chunk.Usage.CompletionTokens
		}
		if len(chunk.Choices) > 0 && chunk.Choices[0].Delta.Content != "" {
			contentTimes = append(contentTimes, time.Now())
		}
	}
	end := time.Now()

	if err := scanner.Err(); err != nil {
		return readFailure(ctx, sendTS, end, err)
	}

	out := Outcome{
		SendTS:     sendTS,
		EndTS:      end,
		Status:     events.StatusOK,
		BodySHA256: hex.EncodeToString(hash.Sum(nil)),
	}
	if !fr.first.IsZero() {
		out.TTFTSeconds = f64ptr(fr.first.Sub(scheduledAt).Seconds())
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
	if usageSeen {
		out.InputTokens = inputTokens
		out.OutputTokens = outputTokens
	} else {
		out.UsageMissing = true
		// Without usage the only honest output count for the mirror
		// series is the content chunks actually received; token-accurate
		// counting is a Contract 4 capability decision (IB-T004).
		out.OutputTokens = len(contentTimes)
	}

	switch {
	case errClass != "":
		out.Status = events.StatusError
		out.ErrorClass = &errClass
	case !doneSeen:
		// Stream closed without [DONE] or an error event: contract
		// violation, classified as upstream_error.
		class := "upstream_error"
		out.Status = events.StatusError
		out.ErrorClass = &class
	}
	return out
}
