package interceptor

import (
	"context"
	"fmt"
	"sync"

	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/function"
	apiinterceptor "github.com/wippyai/runtime/api/interceptor"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/system/eventbus"
	"go.uber.org/zap"
)

// Registry manages available interceptors
type Registry struct {
	ctx          context.Context
	logger       *zap.Logger
	bus          event.Bus
	interceptors []apiinterceptor.Interceptor
	names        map[string]apiinterceptor.Interceptor
	mu           sync.RWMutex
	subscriber   *eventbus.Subscriber
}

// NewInterceptorRegistry creates a new interceptor registry
func NewInterceptorRegistry(bus event.Bus, logger *zap.Logger) *Registry {
	return &Registry{
		ctx:          nil,
		logger:       logger,
		bus:          bus,
		interceptors: make([]apiinterceptor.Interceptor, 0),
		names:        make(map[string]apiinterceptor.Interceptor),
		mu:           sync.RWMutex{},
		subscriber:   nil,
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
	case apiinterceptor.Update:
		r.updateInterceptor(e)
	case apiinterceptor.Delete:
		r.deleteInterceptor(e)
	case apiinterceptor.Accept, apiinterceptor.Reject, function.OptionsAccept, function.OptionsReject:
		// ignore
	default:
		r.logger.Warn("unknown event kind",
			zap.String("kind", e.Kind),
			zap.String("path", e.Path))
	}
}

// registerInterceptor processes a register event
func (r *Registry) registerInterceptor(e event.Event) {
	interceptor, ok := e.Data.(apiinterceptor.Interceptor)
	if !ok {
		r.logger.Error("invalid interceptor payload",
			zap.String("interceptor", e.Path),
			zap.String("type", fmt.Sprintf("%T", e.Data)))

		r.sendReject(e.Path, "invalid interceptor data type")
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// Check if interceptor already exists
	for _, i := range r.interceptors {
		if i == interceptor {
			r.logger.Warn("interceptor already registered",
				zap.String("interceptor", e.Path))
			r.sendReject(e.Path, "interceptor already registered")
			return
		}
	}

	r.interceptors = append(r.interceptors, interceptor)
	r.logger.Debug("interceptor registered", zap.String("interceptor", e.Path))
	r.sendAccept(e.Path)
}

// updateInterceptor processes an update event
func (r *Registry) updateInterceptor(e event.Event) {
	interceptor, ok := e.Data.(apiinterceptor.Interceptor)
	if !ok {
		r.logger.Error("invalid interceptor payload",
			zap.String("interceptor", e.Path),
			zap.String("type", fmt.Sprintf("%T", e.Data)))

		r.sendReject(e.Path, "invalid interceptor data type")
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	found := false
	for i, existing := range r.interceptors {
		if existing == interceptor {
			r.interceptors[i] = interceptor
			found = true
			break
		}
	}

	if !found {
		r.logger.Warn("interceptor not found", zap.String("interceptor", e.Path))
		r.sendReject(e.Path, "interceptor not found")
		return
	}

	r.logger.Debug("interceptor updated", zap.String("interceptor", e.Path))
	r.sendAccept(e.Path)
}

// deleteInterceptor processes a delete event
func (r *Registry) deleteInterceptor(e event.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()

	found := false
	for i, interceptor := range r.interceptors {
		if interceptor == e.Data {
			r.interceptors = append(r.interceptors[:i], r.interceptors[i+1:]...)
			found = true
			break
		}
	}

	if !found {
		r.logger.Warn("interceptor not found", zap.String("interceptor", e.Path))
		r.sendReject(e.Path, "interceptor not found")
		return
	}

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

// Register registers an interceptor with the given name
func (r *Registry) Register(name string, interceptor apiinterceptor.Interceptor) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.names[name]; exists {
		return fmt.Errorf("interceptor %s already registered", name)
	}

	for _, i := range r.interceptors {
		if i == interceptor {
			return fmt.Errorf("interceptor %s already registered", name)
		}
	}

	r.interceptors = append(r.interceptors, interceptor)
	r.names[name] = interceptor
	return nil
}

// Unregister removes an interceptor by name
func (r *Registry) Unregister(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	interceptor, exists := r.names[name]
	if !exists {
		return fmt.Errorf("interceptor %s not found", name)
	}

	for i, existing := range r.interceptors {
		if existing == interceptor {
			r.interceptors = append(r.interceptors[:i], r.interceptors[i+1:]...)
			break
		}
	}

	delete(r.names, name)

	r.bus.Send(r.ctx, event.Event{
		System: apiinterceptor.System,
		Kind:   apiinterceptor.Delete,
		Path:   "interceptor/" + name,
		Data:   interceptor,
	})

	return nil
}

// Get returns an interceptor by name
func (r *Registry) Get(name string) (apiinterceptor.Interceptor, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	interceptor, exists := r.names[name]
	if !exists {
		return nil, fmt.Errorf("interceptor %s not found", name)
	}

	return interceptor, nil
}

// List returns all registered interceptor names
func (r *Registry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.names))
	for name := range r.names {
		names = append(names, name)
	}
	return names
}

// Execute implements the Chain interface by creating a chain and executing it
func (r *Registry) Execute(ctx context.Context, f function.Func, task runtime.Task) (chan *runtime.Result, error) {
	r.mu.RLock()
	interceptors := make([]apiinterceptor.Interceptor, len(r.interceptors))
	copy(interceptors, r.interceptors)
	r.mu.RUnlock()

	chain := newChain(interceptors)
	return chain.Execute(ctx, f, task)
}
