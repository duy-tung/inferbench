// Package run orchestrates one benchmark run: one scheduler goroutine owns
// the precomputed send timeline (ADR-0001), per-request goroutines own
// stream lifecycles, and a single-writer recorder emits raw events.
//
// Open-loop invariant: nothing downstream — response latency, disk stalls,
// recorder backpressure, target saturation — can delay or reorder the send
// schedule. If the client cannot keep the schedule, the two-stage
// schedule-slip watchdog aborts the run with typed reason schedule_slip and
// the run is INVALID (never silently misleading data):
//
//  1. Dispatch stage — intended vs actual dispatch time, checked in the
//     scheduler loop before each request goroutine starts.
//  2. Wire stage — scheduled_send_ts vs actual wire-write time (send_ts),
//     checked when each request completes its send. This covers goroutine
//     start, JSON marshal, DNS/TCP/TLS connect, and blocked body writes —
//     the segment the 2026-07-10 CO-safety review found unmonitored
//     (ADR-0001 §3/§5).
//
// Latency basis (contracts v0.2.0): recorded TTFT/E2E are measured from
// each item's scheduled send time, never from wire-write, so any slip that
// stays under the watchdog threshold still counts against latency instead
// of vanishing (coordinated-omission safety).
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
	"github.com/duy-tung/inferbench/internal/workload"
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
	// MaxSlip is the schedule-slip watchdog threshold, applied at both
	// stages: intended vs actual dispatch time, and scheduled send vs
	// actual wire-write time. <= 0 selects the default 100ms.
	MaxSlip time.Duration
	// EventSink receives the JSONL raw events.
	EventSink io.Writer
	// Log receives the human-readable run log (epoch, schedule dump,
	// per-request determinism hashes, summary). Optional.
	Log io.Writer
}

// Result summarizes one executed run.
type Result struct {
	Sent     int
	OK       int
	Errors   int
	Shed     int
	Canceled int
	// CancelPlanned / SlowPlanned are the plan-side profile counts, logged
	// so realized cancellation (Canceled) is comparable against intent —
	// a planned cancel that loses the race against stream completion is
	// honestly recorded as the completed outcome.
	CancelPlanned int
	SlowPlanned   int
	// DispatchSlips[i] is actual-minus-scheduled dispatch time of item i
	// (schedule-keeping evidence; the CO-safety test asserts on it).
	DispatchSlips []time.Duration
	// MaxDispatchSlip is the largest scheduler-stage slip observed.
	MaxDispatchSlip time.Duration
	// MaxSendSlip is the largest wire-stage slip observed
	// (send_ts - scheduled_send_ts across completed sends).
	MaxSendSlip  time.Duration
	UsageMissing int
	// Epoch is the run start instant: scheduled_send_ts(item) =
	// Epoch + item.SendOffset. Persisted in the run log so events join
	// exactly to the plan.
	Epoch    time.Time
	Started  time.Time
	Finished time.Time
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
	cancelPlanned, slowPlanned, sharingPlanned := 0, 0, 0
	for _, it := range opts.Plan.Items {
		if it.CancelTrigger != "" {
			cancelPlanned++
		}
		if it.SlowReadBytesPerSec > 0 {
			slowPlanned++
		}
		if it.PrefixGroup > 0 {
			sharingPlanned++
		}
	}
	logger.Printf("run_id=%s repetition=%d requests=%d max_slip=%s cancel_planned=%d slow_planned=%d prefix_sharing=%d",
		opts.RunID, opts.Repetition, len(opts.Plan.Items), opts.MaxSlip,
		cancelPlanned, slowPlanned, sharingPlanned)
	for _, it := range opts.Plan.Items {
		extra := ""
		switch it.CancelTrigger {
		case workload.CancelTriggerElapsed:
			extra += fmt.Sprintf(" cancel_after=%.3fs", it.CancelAfterSeconds)
		case workload.CancelTriggerTokens:
			extra += fmt.Sprintf(" cancel_after_tokens=%d", it.CancelAfterTokens)
		}
		if it.SlowReadBytesPerSec > 0 {
			extra += fmt.Sprintf(" slow_read_bps=%d slow_delay=%s", it.SlowReadBytesPerSec, it.SlowInitialDelay)
		}
		if it.PrefixGroup > 0 {
			extra += fmt.Sprintf(" prefix_group=%d", it.PrefixGroup)
		}
		logger.Printf("schedule item=%d offset=%s input_tokens=%d max_tokens=%d%s",
			it.Index, it.SendOffset, it.InputTokens, it.MaxTokens, extra)
	}

	recorder := events.NewRecorder(opts.EventSink, 4096)
	res := &Result{
		DispatchSlips: make([]time.Duration, len(opts.Plan.Items)),
		CancelPlanned: cancelPlanned,
		SlowPlanned:   slowPlanned,
	}

	var (
		wg sync.WaitGroup
		mu sync.Mutex // guards res counters + logger after dispatch starts
	)

	// wireAbort carries the first wire-stage watchdog trip from a request
	// goroutine back to the scheduler (non-blocking send; first wins).
	wireAbort := make(chan error, 1)

	epoch := time.Now()
	res.Epoch = epoch
	res.Started = epoch
	// scheduled_send_ts(item) = epoch + SendOffset. Recording the epoch
	// makes every event joinable to the plan exactly.
	logger.Printf("epoch=%s", epoch.UTC().Format("2006-01-02T15:04:05.000000Z07:00"))
	var abort error

	// The scheduler loop: fires each item at epoch+SendOffset. It never
	// waits on anything a response could influence.
	for _, it := range opts.Plan.Items {
		target := epoch.Add(it.SendOffset)
		if d := time.Until(target); d > 0 {
			timer := time.NewTimer(d)
			select {
			case <-ctx.Done():
				timer.Stop()
				abort = ctx.Err()
			case err := <-wireAbort:
				timer.Stop()
				abort = err
			case <-timer.C:
			}
		}
		if abort == nil {
			select {
			case err := <-wireAbort:
				abort = err
			default:
			}
		}
		if abort != nil {
			break
		}
		slip := time.Since(target)
		res.DispatchSlips[it.Index] = slip
		if slip > res.MaxDispatchSlip {
			res.MaxDispatchSlip = slip
		}
		if slip > opts.MaxSlip {
			abort = fmt.Errorf("%w (stage=dispatch item=%d slip=%s threshold=%s)",
				ErrScheduleSlip, it.Index, slip, opts.MaxSlip)
			break
		}

		res.Sent++
		wg.Add(1)
		go func(it schedule.Item, scheduledAt time.Time) {
			defer wg.Done()
			reqID := fmt.Sprintf("%s-r%d-%06d", opts.RunID, opts.Repetition, it.Index)
			prompt := opts.Plan.Prompt(it)
			req := client.Request{
				RequestID:   reqID,
				Prompt:      prompt,
				MaxTokens:   it.MaxTokens,
				ScheduledAt: scheduledAt,
			}
			// Workload traffic profiles (IB-T004): deliberate cancellation
			// and slow-client read throttling, planned per item by the
			// seeded schedule.
			switch it.CancelTrigger {
			case workload.CancelTriggerElapsed:
				d := time.Duration(it.CancelAfterSeconds * float64(time.Second))
				req.CancelAfter = &d
			case workload.CancelTriggerTokens:
				n := it.CancelAfterTokens
				req.CancelAfterTokens = &n
			}
			if it.SlowReadBytesPerSec > 0 {
				req.SlowRead = &client.SlowRead{
					BytesPerSecond: it.SlowReadBytesPerSec,
					InitialDelay:   it.SlowInitialDelay,
				}
			}
			out := opts.Client.Do(ctx, req)
			recorder.Record(outcomeEvent(opts, it, reqID, out))

			// Wire-stage watchdog (ADR-0001 §3/§5): the request has
			// completed its send; if the actual wire-write time slipped
			// beyond the threshold, the client did not keep the schedule
			// on the wire and the run is INVALID. A request whose send
			// never completed (SendCompleted=false: connect failure or a
			// pre-send cancel) has no wire time to judge — its failure is
			// already a classified event with full scheduled-send latency.
			sendSlip := time.Duration(out.SendSlipSeconds * float64(time.Second))
			if out.SendCompleted && sendSlip > opts.MaxSlip {
				select {
				case wireAbort <- fmt.Errorf("%w (stage=wire item=%d send_slip=%s threshold=%s)",
					ErrScheduleSlip, it.Index, sendSlip, opts.MaxSlip):
				default:
				}
			}

			mu.Lock()
			if sendSlip > res.MaxSendSlip {
				res.MaxSendSlip = sendSlip
			}
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
			logger.Printf("request item=%d id=%s status=%s send_slip=%s body_sha256=%s",
				it.Index, reqID, out.Status, sendSlip, out.BodySHA256)
			mu.Unlock()
		}(it, target)
	}

	// Drain in-flight streams; each is bounded by the client request
	// timeout, so this always terminates.
	wg.Wait()
	res.Finished = time.Now()

	// A wire-stage trip may have landed after the scheduler loop finished
	// (e.g. on the last requests): it still invalidates the run.
	if abort == nil {
		select {
		case err := <-wireAbort:
			abort = err
		default:
		}
	}

	if err := recorder.Close(); err != nil {
		return res, fmt.Errorf("%w: %v", ErrRecorderFailure, err)
	}
	if abort != nil {
		logger.Printf("ABORT reason=%v sent=%d", abort, res.Sent)
		return res, abort
	}
	logger.Printf("summary sent=%d ok=%d errors=%d shed=%d canceled=%d cancel_planned=%d slow_planned=%d max_dispatch_slip=%s max_send_slip=%s usage_missing=%d wall=%s",
		res.Sent, res.OK, res.Errors, res.Shed, res.Canceled, res.CancelPlanned,
		res.SlowPlanned, res.MaxDispatchSlip,
		res.MaxSendSlip, res.UsageMissing,
		res.Finished.Sub(res.Started).Round(time.Millisecond))
	return res, nil
}

// outcomeEvent maps a client outcome onto the raw-event record.
func outcomeEvent(opts Options, it schedule.Item, reqID string, out client.Outcome) events.Event {
	ev := events.Event{
		RunID:             opts.RunID,
		Repetition:        opts.Repetition,
		RequestID:         reqID,
		WorkloadItem:      it.Index,
		ScheduledSendTS:   events.Timestamp(out.ScheduledAt),
		SendTS:            events.Timestamp(out.SendTS),
		EndTS:             events.Timestamp(out.EndTS),
		Status:            out.Status,
		ErrorClass:        out.ErrorClass,
		TTFTSeconds:       out.TTFTSeconds,
		ITL:               out.ITL,
		CancellationPoint: out.Cancellation,
		InputTokens:       out.InputTokens,
		OutputTokens:      out.OutputTokens,
		Shed:              out.Shed,
		Retries:           0, // the generator NEVER retries (ADR-0001)
	}
	// send_slip_seconds is emitted only when the send actually completed;
	// a request whose body write never finished has no wire-write time, and
	// a fabricated ~0 slip would be a false measurement (CO re-review
	// residual, 2026-07-10). send_ts itself falls back to the
	// request-start instant in that case (schema requires non-null).
	if out.SendCompleted {
		slip := out.SendSlipSeconds
		ev.SendSlipSeconds = &slip
	}
	if out.Status == events.StatusCanceled && ev.CancellationPoint == nil {
		// Safety net: the schema requires a cancellation_point object for
		// canceled events. The client always records one; if it ever did
		// not, the close time relative to send_ts is the honest fallback.
		elapsed := out.EndTS.Sub(out.SendTS).Seconds()
		if elapsed < 0 {
			elapsed = 0
		}
		ev.CancellationPoint = &events.CancellationPoint{ElapsedSeconds: elapsed}
	}
	return ev
}
