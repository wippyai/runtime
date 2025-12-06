package option_f

import (
	"context"
	"sync"
	"sync/atomic"
)

// ProcessWrapper wraps a dispatcher to provide per-process isolation.
// Tracks active yields and allows clean eviction/cancellation.
// The wrapper implements ResultReceiver and forwards completions to the scheduler.
type ProcessWrapper struct {
	inner    Dispatcher
	receiver ResultReceiver // the scheduler's receiver
	closed   atomic.Bool

	// Optional: track active yields for cleanup/debugging
	mu           sync.Mutex
	activeYields map[any]struct{}
}

// NewProcessWrapper creates a wrapper around a dispatcher.
// receiver is the scheduler's ResultReceiver.
func NewProcessWrapper(inner Dispatcher, receiver ResultReceiver) *ProcessWrapper {
	return &ProcessWrapper{
		inner:        inner,
		receiver:     receiver,
		activeYields: make(map[any]struct{}),
	}
}

// Dispatch returns a handler wrapped to track this process's yields.
func (w *ProcessWrapper) Dispatch(cmd Command) Handler {
	h := w.inner.Dispatch(cmd)
	if h == nil {
		return nil
	}
	return &wrappedHandler{
		inner:   h,
		wrapper: w,
	}
}

// Track adds a yield tag to active set.
func (w *ProcessWrapper) Track(tag any) {
	w.mu.Lock()
	w.activeYields[tag] = struct{}{}
	w.mu.Unlock()
}

// Untrack removes a yield tag from active set.
func (w *ProcessWrapper) Untrack(tag any) {
	w.mu.Lock()
	delete(w.activeYields, tag)
	w.mu.Unlock()
}

// CompleteYield implements ResultReceiver.
// Forwards to scheduler if wrapper is not closed.
func (w *ProcessWrapper) CompleteYield(tag any, data any, err error) {
	if w.closed.Load() {
		return // silently drop completions after eviction
	}
	w.Untrack(tag)
	w.receiver.CompleteYield(tag, data, err)
}

// Close marks wrapper as closed. Future completions are silently dropped.
// Returns count of yields that were still active (for debugging/metrics).
func (w *ProcessWrapper) Close() int {
	w.closed.Store(true)
	w.mu.Lock()
	count := len(w.activeYields)
	w.activeYields = nil // let GC collect
	w.mu.Unlock()
	return count
}

// ActiveCount returns number of active (pending) yields.
func (w *ProcessWrapper) ActiveCount() int {
	w.mu.Lock()
	n := len(w.activeYields)
	w.mu.Unlock()
	return n
}

// IsClosed returns true if wrapper has been closed.
func (w *ProcessWrapper) IsClosed() bool {
	return w.closed.Load()
}

// wrappedHandler wraps a handler to use the ProcessWrapper as receiver.
type wrappedHandler struct {
	inner   Handler
	wrapper *ProcessWrapper
}

func (h *wrappedHandler) Handle(ctx context.Context, cmd Command, tag any, receiver ResultReceiver) error {
	// Track yield before dispatch
	h.wrapper.Track(tag)
	// Use wrapper as receiver instead of the scheduler directly
	return h.inner.Handle(ctx, cmd, tag, h.wrapper)
}

// LightWrapper is a minimal wrapper that just checks closed status.
// No tracking, just safe eviction. For when you don't need to know active yield count.
type LightWrapper struct {
	receiver ResultReceiver
	closed   atomic.Bool
}

// NewLightWrapper creates a minimal wrapper.
func NewLightWrapper(receiver ResultReceiver) *LightWrapper {
	return &LightWrapper{receiver: receiver}
}

// CompleteYield implements ResultReceiver.
// Forwards to scheduler if wrapper is not closed.
func (w *LightWrapper) CompleteYield(tag any, data any, err error) {
	if w.closed.Load() {
		return
	}
	w.receiver.CompleteYield(tag, data, err)
}

// Close marks wrapper as closed.
func (w *LightWrapper) Close() {
	w.closed.Store(true)
}

// Reset clears wrapper for reuse.
func (w *LightWrapper) Reset(receiver ResultReceiver) {
	w.closed.Store(false)
	w.receiver = receiver
}

// SchedulerWithWrapper is an alternative scheduler that uses ProcessWrapper.
// This demonstrates the composite pattern where each execution gets its own wrapper.
type SchedulerWithWrapper struct {
	dispatcher Dispatcher
	queue      *EventQueue
	output     StepOutput
	gen        uint64
	wrapper    *ProcessWrapper // current execution's wrapper
}

// NewSchedulerWithWrapper creates a scheduler that uses process wrappers.
func NewSchedulerWithWrapper(d Dispatcher) *SchedulerWithWrapper {
	return &SchedulerWithWrapper{
		dispatcher: d,
		queue:      NewEventQueue(),
	}
}

// Queue returns the event queue.
func (s *SchedulerWithWrapper) Queue() *EventQueue {
	return s.queue
}

// CompleteYield implements ResultReceiver.
func (s *SchedulerWithWrapper) CompleteYield(tag any, data any, err error) {
	s.queue.Push(Event{
		Type:  EventYieldComplete,
		Tag:   tag,
		Data:  data,
		Error: err,
	}, s.gen)
}

// Run executes process to completion with a process-specific wrapper.
func (s *SchedulerWithWrapper) Run(ctx context.Context, proc Process, method string, input any) (any, error) {
	if err := proc.Init(ctx, method, input); err != nil {
		return nil, err
	}
	defer proc.Close()

	s.queue.Reset()
	s.gen = s.queue.Generation()
	defer s.queue.Close()

	// Create wrapper for this execution
	s.wrapper = NewProcessWrapper(s.dispatcher, s)
	defer func() {
		// Clean up wrapper, get active count for debugging
		_ = s.wrapper.Close()
		s.wrapper = nil
	}()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		events := s.queue.Drain()
		s.output.Reset()

		if err := proc.Step(events, &s.output); err != nil {
			return nil, err
		}

		if s.output.IsDone() {
			return s.output.Result(), nil
		}

		// Dispatch through wrapper
		s.output.ForEachYield(func(y Yield) {
			handler := s.wrapper.Dispatch(y.Cmd)
			if handler == nil {
				s.queue.PushDirect(Event{
					Type:  EventYieldComplete,
					Tag:   y.Tag,
					Error: &UnknownCommandError{CmdID: y.Cmd.CmdID()},
				})
				return
			}
			// Note: handler.Handle already uses wrapper as receiver
			if err := handler.Handle(ctx, y.Cmd, y.Tag, s.wrapper); err != nil {
				s.queue.PushDirect(Event{
					Type:  EventYieldComplete,
					Tag:   y.Tag,
					Error: err,
				})
			}
		})

		if s.output.Count() == 0 && !s.queue.HasEvents() {
			select {
			case <-s.queue.Signal():
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
	}
}

// NewMessageSender creates a sender for external messages.
func (s *SchedulerWithWrapper) NewMessageSender() *MessageSender {
	return s.queue.NewMessageSender()
}

// SchedulerWithLightWrapper is scheduler using minimal LightWrapper.
// Embeds the wrapper directly to avoid allocation per Run.
type SchedulerWithLightWrapper struct {
	dispatcher Dispatcher
	queue      *EventQueue
	output     StepOutput
	gen        uint64
	wrapper    LightWrapper // embedded, not pointer
}

// NewSchedulerWithLightWrapper creates a scheduler with embedded light wrapper.
func NewSchedulerWithLightWrapper(d Dispatcher) *SchedulerWithLightWrapper {
	s := &SchedulerWithLightWrapper{
		dispatcher: d,
		queue:      NewEventQueue(),
	}
	return s
}

// Queue returns the event queue.
func (s *SchedulerWithLightWrapper) Queue() *EventQueue {
	return s.queue
}

// CompleteYield implements ResultReceiver.
func (s *SchedulerWithLightWrapper) CompleteYield(tag any, data any, err error) {
	s.queue.Push(Event{
		Type:  EventYieldComplete,
		Tag:   tag,
		Data:  data,
		Error: err,
	}, s.gen)
}

// Run executes process to completion.
func (s *SchedulerWithLightWrapper) Run(ctx context.Context, proc Process, method string, input any) (any, error) {
	if err := proc.Init(ctx, method, input); err != nil {
		return nil, err
	}
	defer proc.Close()

	s.queue.Reset()
	s.gen = s.queue.Generation()
	defer s.queue.Close()

	// Reset embedded wrapper for this execution
	s.wrapper.Reset(s)
	defer s.wrapper.Close()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		events := s.queue.Drain()
		s.output.Reset()

		if err := proc.Step(events, &s.output); err != nil {
			return nil, err
		}

		if s.output.IsDone() {
			return s.output.Result(), nil
		}

		// Dispatch yields with wrapper as receiver
		s.output.ForEachYield(func(y Yield) {
			handler := s.dispatcher.Dispatch(y.Cmd)
			if handler == nil {
				s.queue.PushDirect(Event{
					Type:  EventYieldComplete,
					Tag:   y.Tag,
					Error: &UnknownCommandError{CmdID: y.Cmd.CmdID()},
				})
				return
			}
			// Pass wrapper as receiver - drops completions after eviction
			if err := handler.Handle(ctx, y.Cmd, y.Tag, &s.wrapper); err != nil {
				s.queue.PushDirect(Event{
					Type:  EventYieldComplete,
					Tag:   y.Tag,
					Error: err,
				})
			}
		})

		if s.output.Count() == 0 && !s.queue.HasEvents() {
			select {
			case <-s.queue.Signal():
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
	}
}

// NewMessageSender creates a sender for external messages.
func (s *SchedulerWithLightWrapper) NewMessageSender() *MessageSender {
	return s.queue.NewMessageSender()
}
