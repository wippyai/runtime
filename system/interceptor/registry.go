package interceptor

import (
	"context"
	"fmt"
	"sync"

	"github.com/ponyruntime/pony/api/function"
	"github.com/ponyruntime/pony/api/runtime"

	"github.com/ponyruntime/pony/api/event"
	apiinterceptor "github.com/ponyruntime/pony/api/interceptor"
	"github.com/ponyruntime/pony/system/eventbus"
	"go.uber.org/zap"
)

// Registry manages available interceptors
type Registry struct {
	ctx          context.Context
	logger       *zap.Logger
	bus          event.Bus
	interceptors []apiinterceptor.Interceptor
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

	for _, i := range r.interceptors {
		if i == interceptor {
			return fmt.Errorf("interceptor %s already registered", name)
		}
	}

	r.interceptors = append(r.interceptors, interceptor)
	return nil
}

// Unregister removes an interceptor by name
func (r *Registry) Unregister(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for i, interceptor := range r.interceptors {
		if interceptor == nil {
			continue
		}
		// Since we don't have a way to get the name from the interceptor,
		// we'll need to rely on the caller to provide the correct interceptor
		r.interceptors = append(r.interceptors[:i], r.interceptors[i+1:]...)
		return nil
	}
	return fmt.Errorf("interceptor %s not found", name)
}

// Get returns an interceptor by name
func (r *Registry) Get(name string) (apiinterceptor.Interceptor, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, interceptor := range r.interceptors {
		if interceptor == nil {
			continue
		}
		// Since we don't have a way to get the name from the interceptor,
		// we'll need to rely on the caller to provide the correct interceptor
		return interceptor, nil
	}
	return nil, fmt.Errorf("interceptor %s not found", name)
}

// List returns all registered interceptor names
func (r *Registry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.interceptors))
	for _, interceptor := range r.interceptors {
		if interceptor == nil {
			continue
		}
		// Since we don't have a way to get the name from the interceptor,
		// we'll need to rely on the caller to provide the correct interceptor
		names = append(names, "interceptor")
	}
	return names
}

// GetChain returns all registered interceptors as a Chain
func (r *Registry) GetChain() apiinterceptor.Chain {
	r.mu.RLock()
	defer r.mu.RUnlock()

	interceptors := make([]apiinterceptor.Interceptor, len(r.interceptors))
	copy(interceptors, r.interceptors)
	return NewChain(interceptors...)
}

// Chain represents a sequence of interceptors that can be executed in order
type Chain struct {
	interceptors []apiinterceptor.Interceptor
}

// NewChain creates a new Chain with the given interceptors
func NewChain(interceptors ...apiinterceptor.Interceptor) Chain {
	return Chain{
		interceptors: interceptors,
	}
}

// Execute executes the chain of interceptors
func (c Chain) Execute(ctx context.Context, f function.Func, task runtime.Task) (chan *runtime.Result, error) {
	// Create a result channel
	resultChan := make(chan *runtime.Result, 1)

	// Create a next function that will be passed to each interceptor
	next := c.getNext(ctx, resultChan, 0, f, task)
	result := next()
	if result != nil && result.Error != nil {
		close(resultChan)
		return nil, result.Error
	}

	resultChan <- result

	return resultChan, nil
}

func (c Chain) getNext(ctx context.Context, resultChan chan *runtime.Result, index int, f function.Func, task runtime.Task) func() *runtime.Result {
	if index >= len(c.interceptors) {
		return func() *runtime.Result {
			// All interceptors have been executed, now run the actual function
			ch, err := f(ctx, task)
			if err != nil {
				fmt.Printf("function returned error: %+v\n", err)
				return &runtime.Result{Error: err}
			}

			// Forward the result from the function's channel to our result channel
			result := <-ch
			if result != nil && result.Error != nil {
				return result
			}

			fmt.Printf("forwarding function result through interceptor chain: %+v\n", result)

			return result
		}
	}

	interceptor := c.interceptors[index]

	return func() *runtime.Result {
		return interceptor.Handle(
			ctx,
			c.getNext(ctx, resultChan, index+1, f, task),
		)
	}
}
