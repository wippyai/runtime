package interceptor

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/function"
	apiinterceptor "github.com/wippyai/runtime/api/interceptor"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/system/eventbus"
	"go.uber.org/zap"
)

type entry struct {
	interceptor apiinterceptor.Interceptor
	order       int
	name        string
}

// Registry manages available interceptors
type Registry struct {
	ctx        context.Context
	logger     *zap.Logger
	bus        event.Bus
	entries    []entry
	chain      *Chain
	mu         sync.RWMutex
	subscriber *eventbus.Subscriber
}

// NewInterceptorRegistry creates a new interceptor registry
func NewInterceptorRegistry(bus event.Bus, logger *zap.Logger) *Registry {
	return &Registry{
		ctx:        nil,
		logger:     logger,
		bus:        bus,
		entries:    make([]entry, 0),
		chain:      nil,
		mu:         sync.RWMutex{},
		subscriber: nil,
	}
}

// Start initializes the registry and begins listening for interceptor events
func (r *Registry) Start(ctx context.Context) error {
	r.ctx = ctx

	// Subscribe to interceptor events
	sub, err := eventbus.NewSubscriber(
		r.ctx,
		r.bus,
		apiinterceptor.System,
		"*",
		r.handleEvent,
	)
	if err != nil {
		return fmt.Errorf("failed to create subscriber: %w", err)
	}
	r.subscriber = sub

	return nil
}

// Stop cleanly shuts down the registry by closing its event subscriber
func (r *Registry) Stop() error {
	if r.subscriber != nil {
		r.subscriber.Close()
	}
	return nil
}

// handleEvent processes incoming events
func (r *Registry) handleEvent(e event.Event) {
	switch e.Kind {
	case apiinterceptor.Register:
		r.registerInterceptor(e)
	case apiinterceptor.Delete:
		r.deleteInterceptor(e)
	case apiinterceptor.Accept, apiinterceptor.Reject:
		// ignore
	default:
		r.logger.Warn("unknown event kind",
			zap.String("kind", e.Kind),
			zap.String("path", e.Path))
	}
}

// rebuild recalculates the interceptor chain (must be called with lock held)
func (r *Registry) rebuild() {
	sort.Slice(r.entries, func(i, j int) bool {
		return r.entries[i].order < r.entries[j].order
	})

	interceptors := make([]apiinterceptor.Interceptor, len(r.entries))
	for i, e := range r.entries {
		interceptors[i] = e.interceptor
	}

	chain := newChain(interceptors)
	r.chain = &chain
}

// registerInterceptor processes a register event
func (r *Registry) registerInterceptor(e event.Event) {
	var interceptor apiinterceptor.Interceptor
	var order int

	if payload, ok := e.Data.(apiinterceptor.Entry); ok {
		interceptor = payload.Interceptor
		order = payload.Order
	} else if ic, ok := e.Data.(apiinterceptor.Interceptor); ok {
		interceptor = ic
		order = 100
	} else {
		r.logger.Error("invalid interceptor payload",
			zap.String("interceptor", e.Path),
			zap.String("type", fmt.Sprintf("%T", e.Data)))

		r.sendReject(e.Path, "invalid interceptor data type")
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	for _, entry := range r.entries {
		if entry.name == e.Path {
			r.logger.Warn("interceptor already registered",
				zap.String("interceptor", e.Path))
			r.sendReject(e.Path, "interceptor already registered")
			return
		}
	}

	r.entries = append(r.entries, entry{
		interceptor: interceptor,
		order:       order,
		name:        e.Path,
	})

	r.rebuild()

	r.logger.Debug("interceptor registered",
		zap.String("interceptor", e.Path),
		zap.Int("order", order))
	r.sendAccept(e.Path)
}

// updateInterceptor processes an update event
func (r *Registry) updateInterceptor(e event.Event) {
	r.logger.Warn("update not supported for interceptors, use delete + register",
		zap.String("interceptor", e.Path))
	r.sendReject(e.Path, "update not supported")
}

// deleteInterceptor processes a delete event
func (r *Registry) deleteInterceptor(e event.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()

	found := false
	for i, entry := range r.entries {
		if entry.name == e.Path {
			r.entries = append(r.entries[:i], r.entries[i+1:]...)
			found = true
			break
		}
	}

	if !found {
		r.logger.Warn("interceptor not found", zap.String("interceptor", e.Path))
		r.sendReject(e.Path, "interceptor not found")
		return
	}

	r.rebuild()

	r.logger.Debug("interceptor removed", zap.String("interceptor", e.Path))
	r.sendAccept(e.Path)
}

// sendAccept sends an accept event
func (r *Registry) sendAccept(path event.Path) {
	r.bus.Send(r.ctx, event.Event{
		System: apiinterceptor.System,
		Kind:   apiinterceptor.Accept,
		Path:   path,
	})
}

// sendReject sends a reject event
func (r *Registry) sendReject(path event.Path, reason string) {
	r.bus.Send(r.ctx, event.Event{
		System: apiinterceptor.System,
		Kind:   apiinterceptor.Reject,
		Path:   path,
		Data:   reason,
	})
}

// Execute implements the Chain interface using the pre-built chain
func (r *Registry) Execute(ctx context.Context, f function.Func, task runtime.Task) (chan *runtime.Result, error) {
	r.mu.RLock()
	chain := r.chain
	r.mu.RUnlock()

	if chain == nil {
		return f(ctx, task)
	}

	return chain.Execute(ctx, f, task)
}
