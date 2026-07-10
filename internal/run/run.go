// Package run orchestrates one benchmark run: one scheduler goroutine owns
// the precomputed send timeline (ADR-0001), per-request goroutines own
// stream lifecycles, and a single-writer recorder emits raw events.
//
// Open-loop invariant: nothing downstream — response latency, disk stalls,
// recorder backpressure, target saturation — can delay or reorder the send
// schedule. If the client host itself cannot keep the schedule, the
// schedule-slip watchdog aborts the run with typed reason schedule_slip and
// the run is INVALID (never silently misleading data).
package run

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"sync"
	"time"

	"github.com/duy-tung/inferbench/internal/client"
	"github.com/duy-tung/inferbench/internal/events"
	"github.com/duy-tung/inferbench/internal/schedule"
)

// Typed abort reasons (run-invalidating).
var (
	ErrScheduleSlip      = errors.New("schedule_slip: client could not keep the precomputed send schedule; run INVALID")
	ErrTargetUnreachable = errors.New("target_unreachable: target did not respond at run start; run INVALID")
	ErrRecorderFailure   = errors.New("recorder_failure: raw-event write failed; run INVALID")
)

// Options configures one run execution.
type Options struct {
	Plan       *schedule.Plan
	Client     *client.Client
	RunID      string
	Repetition int
	// MaxSlip is the schedule-slip watchdog threshold (intended vs actual
	// dispatch time). <= 0 selects the default 100ms.
	MaxSlip time.Duration
	// EventSink receives the JSONL raw events.
	EventSink io.Writer
	// Log receives the human-readable run log (schedule dump, per-request
	// determinism hashes, summary). Optional.
	Log io.Writer
}

// Result summarizes one executed run.
type Result struct {
	Sent     int
	OK       int
	Errors   int
	Shed     int
	Canceled int
	// DispatchSlips[i] is actual-minus-scheduled dispatch time of item i
	// (schedule-keeping evidence; the CO-safety test asserts on it).
	DispatchSlips []time.Duration
	MaxSlip       time.Duration
	UsageMissing  int
	Started       time.Time
	Finished      time.Time
}

// Execute runs the plan. It returns a run-invalidating typed error
// (ErrScheduleSlip, ErrRecorderFailure, ctx errors) or the Result.
// Per-request failures never abort the run — they are classified events.
func Execute(ctx context.Context, opts Options) (*Result, error) {
	if opts.MaxSlip <= 0 {
		opts.MaxSlip = 100 * time.Millisecond
	}
	logger := log.New(io.Discard, "", 0)
	if opts.Log != nil {
		logger = log.New(opts.Log, "", 0)
	}

	// Dump the precomputed schedule before the first send: the timeline is
	// already fixed, and the dump is the replay-determinism evidence.
	logger.Printf("run_id=%s repetition=%d requests=%d max_slip=%s",
		opts.RunID, opts.Repetition, len(opts.Plan.Items), opts.MaxSlip)
	for _, it := range opts.Plan.Items {
		logger.Printf("schedule item=%d offset=%s input_tokens=%d max_tokens=%d",
			it.Index, it.SendOffset, it.InputTokens, it.MaxTokens)
	}

	recorder := events.NewRecorder(opts.EventSink, 4096)
	res := &Result{DispatchSlips: make([]time.Duration, len(opts.Plan.Items))}

	var (
		wg sync.WaitGroup
		mu sync.Mutex // guards res counters + logger after dispatch starts
	)

	start := time.Now()
	res.Started = start
	var abort error

	// The scheduler loop: fires each item at start+SendOffset. It never
	// waits on anything a response could influence.
	for _, it := range opts.Plan.Items {
		target := start.Add(it.SendOffset)
		if d := time.Until(target); d > 0 {
			timer := time.NewTimer(d)
			select {
			case <-ctx.Done():
				timer.Stop()
				abort = ctx.Err()
			case <-timer.C:
			}
		}
		if abort != nil {
			break
		}
		slip := time.Since(target)
		res.DispatchSlips[it.Index] = slip
		if slip > res.MaxSlip {
			res.MaxSlip = slip
		}
		if slip > opts.MaxSlip {
			abort = fmt.Errorf("%w (item=%d slip=%s threshold=%s)",
				ErrScheduleSlip, it.Index, slip, opts.MaxSlip)
			break
		}

		res.Sent++
		wg.Add(1)
		go func(it schedule.Item) {
			defer wg.Done()
			reqID := fmt.Sprintf("%s-r%d-%06d", opts.RunID, opts.Repetition, it.Index)
			prompt := opts.Plan.Prompt(it)
			out := opts.Client.Do(ctx, client.Request{
				RequestID: reqID,
				Prompt:    prompt,
				MaxTokens: it.MaxTokens,
			})
			recorder.Record(outcomeEvent(opts, it, reqID, out))

			mu.Lock()
			switch out.Status {
			case events.StatusOK:
				res.OK++
			case events.StatusShed:
				res.Shed++
			case events.StatusCanceled:
				res.Canceled++
			default:
				res.Errors++
			}
			if out.UsageMissing {
				res.UsageMissing++
			}
			logger.Printf("request item=%d id=%s status=%s body_sha256=%s",
				it.Index, reqID, out.Status, out.BodySHA256)
			mu.Unlock()
		}(it)
	}

	// Drain in-flight streams; each is bounded by the client request
	// timeout, so this always terminates.
	wg.Wait()
	res.Finished = time.Now()

	if err := recorder.Close(); err != nil {
		return res, fmt.Errorf("%w: %v", ErrRecorderFailure, err)
	}
	if abort != nil {
		logger.Printf("ABORT reason=%v sent=%d", abort, res.Sent)
		return res, abort
	}
	logger.Printf("summary sent=%d ok=%d errors=%d shed=%d canceled=%d max_dispatch_slip=%s usage_missing=%d wall=%s",
		res.Sent, res.OK, res.Errors, res.Shed, res.Canceled, res.MaxSlip, res.UsageMissing,
		res.Finished.Sub(res.Started).Round(time.Millisecond))
	return res, nil
}

// outcomeEvent maps a client outcome onto the raw-event record.
func outcomeEvent(opts Options, it schedule.Item, reqID string, out client.Outcome) events.Event {
	ev := events.Event{
		RunID:        opts.RunID,
		Repetition:   opts.Repetition,
		RequestID:    reqID,
		WorkloadItem: it.Index,
		SendTS:       events.Timestamp(out.SendTS),
		EndTS:        events.Timestamp(out.EndTS),
		Status:       out.Status,
		ErrorClass:   out.ErrorClass,
		TTFTSeconds:  out.TTFTSeconds,
		ITL:          out.ITL,
		InputTokens:  out.InputTokens,
		OutputTokens: out.OutputTokens,
		Shed:         out.Shed,
		Retries:      0, // the generator NEVER retries (ADR-0001)
	}
	if out.Status == events.StatusCanceled {
		ev.CancellationPoint = &events.CancellationPoint{
			ElapsedSeconds: out.EndTS.Sub(out.SendTS).Seconds(),
		}
	}
	return ev
}
