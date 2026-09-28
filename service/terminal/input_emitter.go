// SPDX-License-Identifier: MPL-2.0

package terminal

import (
	"sync"
	"time"
)

const mouseMotionInterval = 8 * time.Millisecond

// inputEmitter bounds high-rate pointer motion at the physical ingress. It
// keeps only the newest motion during a frame interval, flushes it before any
// discrete event, and allocates no idle ticker or scheduler package.
type inputEmitter struct {
	send     func(*TTYEvent)
	pending  *TTYEvent
	timer    *time.Timer
	mu       sync.Mutex
	delivery sync.Mutex
	stopped  bool
}

func newInputEmitter(send func(*TTYEvent)) *inputEmitter {
	return &inputEmitter{send: send}
}

func (e *inputEmitter) emit(event *TTYEvent) {
	if event == nil {
		return
	}
	if event.Type == "mouse" && event.Action == "motion" {
		e.mu.Lock()
		if e.stopped {
			e.mu.Unlock()
			return
		}
		copy := *event
		e.pending = &copy
		if e.timer == nil {
			e.timer = time.AfterFunc(mouseMotionInterval, e.flush)
		}
		e.mu.Unlock()
		return
	}
	e.deliverPending(event)
}

func (e *inputEmitter) flush() {
	e.deliverPending()
}

func (e *inputEmitter) takePending() (*TTYEvent, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.stopped {
		return nil, false
	}
	if e.timer != nil {
		e.timer.Stop()
		e.timer = nil
	}
	event := e.pending
	e.pending = nil
	return event, true
}

func (e *inputEmitter) deliverPending(events ...*TTYEvent) {
	e.delivery.Lock()
	defer e.delivery.Unlock()
	pending, active := e.takePending()
	if !active {
		return
	}
	if pending != nil {
		e.send(pending)
	}
	for _, event := range events {
		if event != nil {
			e.send(event)
		}
	}
}

func (e *inputEmitter) stop() {
	// Serialize shutdown with discrete events and timer-driven flushes. Once
	// stopped is visible, later deliveries enter the same gate and become no-ops.
	e.delivery.Lock()
	defer e.delivery.Unlock()
	e.mu.Lock()
	e.stopped = true
	e.pending = nil
	if e.timer != nil {
		e.timer.Stop()
		e.timer = nil
	}
	e.mu.Unlock()
}
