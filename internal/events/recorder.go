package events

import (
	"bufio"
	"encoding/json"
	"io"
	"sync"
)

// Recorder is the single-writer JSONL event sink. Producer goroutines call
// Record; one goroutine owns the writer (bounded memory, no interleaved
// lines). Backpressure on the channel blocks the *request* goroutine that
// produced the event — never the scheduler, which does not write events.
type Recorder struct {
	ch   chan Event
	done chan struct{}

	mu  sync.Mutex
	err error
}

// NewRecorder starts the writer goroutine. buffer bounds queued events.
func NewRecorder(w io.Writer, buffer int) *Recorder {
	if buffer <= 0 {
		buffer = 1024
	}
	r := &Recorder{
		ch:   make(chan Event, buffer),
		done: make(chan struct{}),
	}
	go r.loop(w)
	return r
}

func (r *Recorder) loop(w io.Writer) {
	defer close(r.done)
	bw := bufio.NewWriter(w)
	enc := json.NewEncoder(bw)
	for ev := range r.ch {
		if err := enc.Encode(&ev); err != nil {
			r.setErr(err)
		}
	}
	if err := bw.Flush(); err != nil {
		r.setErr(err)
	}
}

func (r *Recorder) setErr(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err == nil {
		r.err = err
	}
}

// Record enqueues one event; blocks if the buffer is full.
func (r *Recorder) Record(ev Event) {
	r.ch <- ev
}

// Close drains the queue, flushes, and returns the first write error.
// No Record calls may happen after Close.
func (r *Recorder) Close() error {
	close(r.ch)
	<-r.done
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.err
}
