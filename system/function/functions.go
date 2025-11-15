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
		"function.(register|delete)",
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
	default:
		f.logger.Warn("unknown event kind",
			zap.String("kind", e.Kind),
			zap.String("path", e.Path))
	}
}

func (f *Registry) registerFunction(e event.Event) {
	reg, ok := e.Data.(*function.FuncEntry)
	if !ok {
		f.logger.Error("invalid register function payload",
			zap.String("function", e.Path),
			zap.String("type", fmt.Sprintf("%T", e.Data)))

		f.sendReject(e.Path, "invalid register function payload")
		return
	}

	id := registry.ParseID(e.Path)

	// Store the function handler
	f.handlers.Store(id, reg.Handler)

	// Store options if provided (note: interceptors not fully working yet)
	if reg.Options != nil {
		f.options.Store(id, reg.Options)
	} else {
		// Remove options if nil (handles updates that clear options)
		f.options.Delete(id)
	}

	f.logger.Debug("function registered", zap.String("function", e.Path))
	f.sendAccept(e.Path)
}

func (f *Registry) deleteFunction(e event.Event) {
	id := registry.ParseID(e.Path)

	// Check if the function exists before removing
	_, exists := f.handlers.Load(id)
	if !exists {
		f.logger.Warn("function not found", zap.String("function", e.Path))
		f.sendReject(e.Path, "function not found")
		return
	}

	// Remove the function handler
	f.handlers.Delete(id)

	// Remove associated options
	f.options.Delete(id)

	f.logger.Debug("function removed", zap.String("function", e.Path))
	f.sendAccept(e.Path)
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
	ctx, fc := ctxapi.OpenFrameContext(ctx)

	// Generate PID for this function call
	gen := pidgen.GetGenerator(ctx)
	pid := gen.Generate(function.HostID, task.ID)

	// Pre-allocate pairs slice with exact size to avoid reallocation
	pairsLen := 3 + len(task.Context)
	pairs := make([]ctxapi.Pair, pairsLen)
	pairs[0] = ctxapi.Pair{Key: runtimeapi.FrameIDKey, Value: task.ID}
	pairs[1] = ctxapi.Pair{Key: runtimeapi.FramePIDKey, Value: pid}
	pairs[2] = ctxapi.Pair{Key: runtimeapi.FrameHostKey, Value: f.host}

	// Add task context overrides (actor, scope, etc.)
	if len(task.Context) > 0 {
		copy(pairs[3:], task.Context)
	}

	if err := fc.SetMultiple(pairs...); err != nil {
		return nil, fmt.Errorf("failed to set frame context: %w", err)
	}

	// Merge preset and runtime options
	var mergedOptions interceptor.Options
	storedOptions, hasPreset := f.options.Load(task.ID)
	hasRuntime := task.Options != nil

	if hasPreset || hasRuntime {
		var bag interceptor.Bag

		if hasPreset {
			if presetOpts, ok := storedOptions.(interceptor.Bag); ok {
				bag = presetOpts
			}
		}

		if hasRuntime {
			if runtimeOpts, ok := task.Options.(interceptor.Bag); ok {
				if bag != nil {
					bag = bag.Merge(runtimeOpts)
				} else {
					bag = runtimeOpts
				}
			}
		}

		if bag == nil {
			bag = interceptor.NewBag()
		}

		mergedOptions = bag

		// Store options in FrameContext
		if err := interceptor.SetOptions(ctx, mergedOptions); err != nil {
			f.logger.Warn("failed to set interceptor options",
				zap.String("function", task.ID.String()),
				zap.Error(err))
		}
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
