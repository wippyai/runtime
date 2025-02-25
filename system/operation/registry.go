package operation

import (
	"context"
	"fmt"
	"sync"

	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/operation"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/internal/metamatch"
	"github.com/ponyruntime/pony/system/eventbus"
	"go.uber.org/zap"
)

// Registry manages operation registration and execution
type Registry struct {
	ctx        context.Context
	logger     *zap.Logger
	bus        events.Bus
	operations sync.Map // map[string]operation.Operation
	subscriber *eventbus.Subscriber
}

// NewOperationRegistry creates a new operation registry instance
func NewOperationRegistry(bus events.Bus, logger *zap.Logger) *Registry {
	return &Registry{
		bus:    bus,
		logger: logger,
	}
}

// Start initializes the registry and begins listening for operation events
func (r *Registry) Start(ctx context.Context) error {
	r.ctx = ctx

	// Subscribe to operation events
	sub, err := eventbus.NewSubscriber(
		r.ctx,
		r.bus,
		operation.System,
		"operation.(register|delete)",
		r.handleEvent,
	)
	if err != nil {
		return fmt.Errorf("failed to create subscriber: %w", err)
	}
	r.subscriber = sub

	r.logger.Info("operation registry started")
	return nil
}

// Stop cleanly shuts down the registry
func (r *Registry) Stop() error {
	if r.subscriber != nil {
		r.subscriber.Close()
	}
	return nil
}

func (r *Registry) handleEvent(e events.Event) {
	switch e.Kind {
	case operation.OpRegister:
		r.registerOperation(e)
	case operation.OpDelete:
		r.deleteOperation(e)
	default:
		r.logger.Warn("unknown event kind",
			zap.String("kind", e.Kind),
			zap.String("path", e.Path))
	}
}

func (r *Registry) registerOperation(e events.Event) {
	op, ok := e.Data.(operation.Operation)
	if !ok {
		r.logger.Error("invalid register operation payload",
			zap.String("operation", e.Path),
			zap.String("type", fmt.Sprintf("%T", e.Data)))

		r.sendReject(e.Path, "invalid register operation payload")
		return
	}

	// Get operation metadata and use path as key
	metadata := op.Meta()
	key := e.Path

	// Store the operation
	r.operations.Store(key, op)
	r.logger.Debug("operation registered",
		zap.String("operation", key),
		zap.Any("metadata", metadata))

	r.sendAccept(e.Path)
}

func (r *Registry) deleteOperation(e events.Event) {
	key := e.Path

	// Check if the operation exists before removing
	_, exists := r.operations.Load(key)
	if !exists {
		r.logger.Warn("operation not found", zap.String("operation", key))
		r.sendReject(e.Path, "operation not found")
		return
	}

	// Remove the operation
	r.operations.Delete(key)
	r.logger.Debug("operation removed", zap.String("operation", key))

	r.sendAccept(e.Path)
}

func (r *Registry) sendAccept(path events.Path) {
	r.bus.Send(r.ctx, events.Event{
		System: operation.System,
		Kind:   operation.OpAccept,
		Path:   path,
	})
}

func (r *Registry) sendReject(path events.Path, reason string) {
	r.bus.Send(r.ctx, events.Event{
		System: operation.System,
		Kind:   operation.OpReject,
		Path:   path,
		Data:   reason,
	})
}

// Find implements the operation.Registry interface by searching for operations
// that match the provided metadata criteria
func (r *Registry) Find(searchMeta registry.Metadata) ([]operation.Operation, error) {
	// Create a matcher from the search metadata
	matcher := metadataToMatcher(searchMeta)

	var result []operation.Operation
	r.operations.Range(func(_, value interface{}) bool {
		op := value.(operation.Operation)
		if matcher.Match(op.Meta()) {
			result = append(result, op)
		}
		return true
	})

	return result, nil
}

// metadataToMatcher converts a metadata map to a metamatch.Matcher
func metadataToMatcher(metadata registry.Metadata) *metamatch.Matcher {
	matcher := metamatch.NewMatcher()

	// Add exact value conditions for each metadata entry
	for key, value := range metadata {
		switch v := value.(type) {
		case string:
			matcher = matcher.WithStringValue(key, v)
		case bool:
			matcher = matcher.WithBoolValue(key, v)
		case int:
			matcher = matcher.WithIntValue(key, v)
		case []string:
			// For string arrays, we assume ALL values must be present (AND logic)
			for _, tag := range v {
				matcher = matcher.WithTagContains(key, tag)
			}
		default:
			// For other types, use exact value matching
			matcher = matcher.WithExactValue(key, value)
		}
	}

	return matcher
}

// Ensure Registry implements the operation.Registry interface
var _ operation.Registry = (*Registry)(nil)
