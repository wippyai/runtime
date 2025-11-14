package function

import (
	"context"
	"fmt"
	"sync"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/function"
	"github.com/wippyai/runtime/api/interceptor"
	"github.com/wippyai/runtime/api/pidgen"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	runtimeapi "github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/system/eventbus"
	"go.uber.org/zap"
)

// Registry manages the execution of tasks by registered handlers in the runtime system.
// It uses an event bus for communication and supports dynamic handler registration.
type Registry struct {
	ctx        context.Context
	host       relay.Host
	logger     *zap.Logger
	bus        event.Bus
	handlers   sync.Map
	options    sync.Map
	subscriber *eventbus.Subscriber
}

// NewFunctionRegistry creates a new Registry instance with the provided event bus and logger.
func NewFunctionRegistry(bus event.Bus, host relay.Host, logger *zap.Logger) *Registry {
	return &Registry{
		bus:      bus,
		host:     host,
		logger:   logger,
		handlers: sync.Map{},
		options:  sync.Map{},
	}
}

// Start initializes the executor and begins listening for executor events.
// It sets up a subscriber for handling executor-related events on the event bus.
func (f *Registry) Start(ctx context.Context) error {
	f.ctx = ctx

	// Subscribe to executor events
	sub, err := eventbus.NewSubscriber(
		f.ctx,
		f.bus,
		function.System,
		"function.(register|delete|optionsregister|optionsdelete)",
		f.handleEvent,
	)
	if err != nil {
		return fmt.Errorf("failed to create subscriber: %w", err)
	}
	f.subscriber = sub

	return nil
}

// Stop cleanly shuts down the executor by closing its event subscriber.
func (f *Registry) Stop() error {
	if f.subscriber != nil {
		f.subscriber.Close()
	}
	return nil
}

func (f *Registry) handleEvent(e event.Event) {
	switch e.Kind {
	case function.Register:
		f.registerFunction(e)
	case function.Delete:
		f.deleteFunction(e)
	case function.OptionsRegister:
		f.registerOptions(e)
	case function.OptionsDelete:
		f.deleteOptions(e)

	default:
		f.logger.Warn("unknown event kind",
			zap.String("kind", e.Kind),
			zap.String("path", e.Path))
	}
}

func (f *Registry) registerFunction(e event.Event) {
	fn, ok := e.Data.(function.Func)
	if !ok {
		f.logger.Error("invalid register function payload",
			zap.String("function", e.Path),
			zap.String("type", fmt.Sprintf("%T", e.Data)))

		f.sendReject(e.Path, "invalid register function payload")
		return
	}

	// Store the function
	f.handlers.Store(registry.ParseID(e.Path), fn)
	f.logger.Debug("function registered", zap.String("function", e.Path))

	f.sendAccept(e.Path)
}

func (f *Registry) deleteFunction(e event.Event) {
	// Check if the function exists before removing
	_, exists := f.handlers.Load(registry.ParseID(e.Path))
	if !exists {
		f.logger.Warn("function not found", zap.String("function", e.Path))
		f.sendReject(e.Path, "function not found")
		return
	}

	// Done the function
	f.handlers.Delete(registry.ParseID(e.Path))
	f.logger.Debug("function removed", zap.String("function", e.Path))

	f.sendAccept(e.Path)
}

func (f *Registry) registerOptions(e event.Event) {
	options, ok := e.Data.(interceptor.Options)
	if !ok {
		f.logger.Error("invalid register options payload",
			zap.String("function", e.Path),
			zap.String("type", fmt.Sprintf("%T", e.Data)))

		f.sendOptionsReject(e.Path, "invalid register function payload")
		return
	}

	// Store the function
	f.options.Store(registry.ParseID(e.Path), options)
	f.logger.Debug("options registered", zap.String("options", e.Path))

	f.sendOptionsAccept(e.Path)
}

func (f *Registry) deleteOptions(e event.Event) {
	// Check if the function exists before removing
	_, exists := f.options.Load(registry.ParseID(e.Path))
	if !exists {
		f.logger.Warn("options not found", zap.String("options", e.Path))
		f.sendOptionsReject(e.Path, "options not found")
		return
	}

	// Done the function
	f.options.Delete(registry.ParseID(e.Path))
	f.logger.Debug("options removed", zap.String("options", e.Path))

	f.sendOptionsAccept(e.Path)
}

func (f *Registry) sendAccept(path event.Path) {
	f.bus.Send(f.ctx, event.Event{
		System: function.System,
		Kind:   function.Accept,
		Path:   path,
	})
}

func (f *Registry) sendReject(path event.Path, reason string) {
	f.bus.Send(f.ctx, event.Event{
		System: function.System,
		Kind:   function.Reject,
		Path:   path,
		Data:   reason,
	})
}

func (f *Registry) sendOptionsAccept(path event.Path) {
	f.bus.Send(f.ctx, event.Event{
		System: function.System,
		Kind:   function.OptionsAccept,
		Path:   path,
	})
}

func (f *Registry) sendOptionsReject(path event.Path, reason string) {
	f.bus.Send(f.ctx, event.Event{
		System: function.System,
		Kind:   function.OptionsReject,
		Path:   path,
		Data:   reason,
	})
}

// Call runs the given task using its registered handler and returns a channel
// for receiving the execution result(s). Returns an error if no handler is registered
// for the task's target or if the handler type is invalid.
func (f *Registry) Call(ctx context.Context, task runtimeapi.Task) (chan *runtimeapi.Result, error) {
	if ctx == nil {
		return nil, fmt.Errorf("nil context")
	}

	handler, exists := f.handlers.Load(task.ID)
	if !exists {
		return nil, fmt.Errorf("no handler registered for target: %s", task.ID)
	}

	execHandler, ok := handler.(function.Func)
	if !ok {
		return nil, fmt.Errorf("invalid handler type for target: %s", task.ID)
	}

	// Get or create FrameContext for this function call
	ctx, _ = ctxapi.OpenFrameContext(ctx)

	// Generate PID for this function call
	gen := pidgen.GetGenerator(ctx)
	pid := gen.Generate(function.HostID, task.ID)

	// Store frame metadata
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return nil, fmt.Errorf("no frame context available")
	}

	// Build pairs to set: frame metadata + task context overrides
	pairs := []ctxapi.Pair{
		{Key: runtimeapi.FrameIDKey, Value: task.ID},
		{Key: runtimeapi.FramePIDKey, Value: pid},
		{Key: runtimeapi.FrameHostKey, Value: f.host},
	}

	// Add task context overrides (actor, scope, custom values, etc.)
	if len(task.Context) > 0 {
		// Special handling for Values: merge with existing instead of replacing
		var mergedPairs []ctxapi.Pair
		for _, pair := range task.Context {
			if key, ok := pair.Key.(*ctxapi.Key); ok && key.Name == "values" {
				if newVals, ok := pair.Value.(*ctxapi.Values); ok {
					// Get existing values from frame
					existingVals := ctxapi.GetValues(ctx)
					mergedValues := ctxapi.NewValues()

					// Copy existing values first
					if existingVals != nil {
						existingVals.Iterate(func(k any, v any) {
							mergedValues.Set(k, v)
						})
					}

					// Overlay new values
					newVals.Iterate(func(k any, v any) {
						mergedValues.Set(k, v)
					})

					// Replace the pair with merged values
					mergedPairs = append(mergedPairs, ctxapi.ValuesPair(mergedValues))
				} else {
					mergedPairs = append(mergedPairs, pair)
				}
			} else {
				mergedPairs = append(mergedPairs, pair)
			}
		}
		pairs = append(pairs, mergedPairs...)
	}

	if err := fc.SetMultiple(pairs...); err != nil {
		return nil, fmt.Errorf("failed to set frame context: %w", err)
	}

	// Merge preset and runtime options
	var mergedOptions interceptor.Options
	storedOptions, exists := f.options.Load(task.ID)
	if exists {
		if presetOpts, ok := storedOptions.(interceptor.Options); ok {
			mergedOptions = presetOpts
		}
	}
	if mergedOptions == nil {
		mergedOptions = interceptor.NewBag()
	}
	if task.Options != nil {
		if runtimeOpts, ok := task.Options.(interceptor.Options); ok {
			mergedOptions = mergedOptions.Merge(runtimeOpts)
		}
	}

	// Store options in FrameContext
	if err := interceptor.SetOptions(ctx, mergedOptions); err != nil {
		f.logger.Warn("failed to set interceptor options",
			zap.String("function", task.ID.String()),
			zap.Error(err))
	}

	// Execute through interceptor chain if available
	chain := interceptor.GetChain(ctx)
	if chain != nil {
		ch, err := chain.Execute(ctx, execHandler, task)
		if err != nil {
			f.logger.Debug("interceptor chain execution failed",
				zap.String("function", task.ID.String()),
				zap.String("pid", pid.String()),
				zap.Error(err))
			return nil, err
		}
		return ch, nil
	}

	// Execute handler directly if no chain
	ch, err := execHandler(ctx, task)
	if err != nil {
		f.logger.Error(err.Error(),
			zap.String("function", task.ID.String()),
			zap.String("pid", pid.String()))
		return nil, err
	}

	return ch, nil
}

// Ensure Registry implements the operation.Registry interface
var _ function.Registry = (*Registry)(nil)
