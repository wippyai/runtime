// Package runner provides implementations for running registry operations
package runner

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/ponyruntime/pony/api/event"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/system/registry/topology"
	"go.uber.org/zap"
)

// BusRunner executes registry operations sequentially through an event bus, handling
// state transitions, rollbacks, and error handling. It maintains operation order
// and provides transactional semantics through the event bus.
type BusRunner struct {
	bus         event.Bus
	log         *zap.Logger
	acceptChan  chan event.Event
	rejectChan  chan event.Event
	acceptSubID event.SubscriberID
	rejectSubID event.SubscriberID
	builder     *topology.StateBuilder
}

// NewBusRunner creates a new BusRunner. This is a sequential bus, order of operations matter.
func NewBusRunner(bus event.Bus, log *zap.Logger) *BusRunner {
	return &BusRunner{
		bus:        bus,
		log:        log,
		acceptChan: make(chan event.Event),
		rejectChan: make(chan event.Event),
		builder:    topology.NewStateBuilder(log),
	}
}

// Transition applies a series of operations to transform the registry from an initial state
// to a new state. If any operation fails, it rolls back all previously applied operations
// to maintain consistency. The process is coordinated through the event bus with Accept/Reject
// events determining the success of each operation.
func (br *BusRunner) Transition(
	ctx context.Context,
	initialState registry.State,
	cs registry.ChangeSet,
) (registry.State, error) {
	currentState := topology.NewStateMap(initialState)
	originalState := topology.NewStateMap(initialState) // Keep a copy of the original state for rollbacks

	if err := br.subscribeToEvents(ctx); err != nil {
		return nil, err
	}
	defer br.unsubscribeFromEvents(ctx)

	br.bus.Send(ctx, event.Event{
		System: registry.System,
		Kind:   registry.Begin,
	})

	for i, op := range cs {
		newState, err := br.applyOperation(ctx, currentState, op)
		if err != nil {
			if ctx.Err() != nil {
				br.log.Error("context canceled during operation",
					zap.Int("operation_index", i),
					zap.String("operation_kind", op.Kind),
					zap.String("entry_id", op.Entry.ID.String()),
					zap.Error(err))
				return nil, err
			}

			br.log.Warn("operation failed, initiating rollback",
				zap.Int("operation_index", i),
				zap.Int("total_operations", len(cs)),
				zap.String("operation_kind", op.Kind),
				zap.String("entry_id", op.Entry.ID.String()),
				zap.String("entry_kind", op.Entry.Kind),
				zap.Any("operation", op),
				zap.Error(err))

			newState = br.rollback(ctx, originalState, newState)

			// Only send Discard if there was an error, and rollback already happened
			br.bus.Send(ctx, event.Event{
				System: registry.System,
				Kind:   registry.Discard,
				Data:   err,
			})

			return newState.ToSlice(), fmt.Errorf("operation failed: %w", err)
		}

		currentState = newState
	}

	br.bus.Send(ctx, event.Event{
		System: registry.System,
		Kind:   registry.Commit,
	})

	return currentState.ToSlice(), nil
}

func (br *BusRunner) applyOperation(
	ctx context.Context,
	state topology.StateMap,
	op registry.Operation,
) (topology.StateMap, error) {
	if err := br.builder.ValidateOperation(state, op); err != nil {
		return state, fmt.Errorf("invalid operation: %w", err)
	}

	if op.Entry.Kind == "" {
		// resolve from reg or fail
		entry, ok := state[op.Entry.ID]
		if !ok {
			return state, fmt.Errorf("entry kind can not be found: %s", entry.ID)
		}

		op.Entry.Kind = entry.Kind
	}

	allowProcess := []registry.Kind{
		registry.KindEntry,
		registry.KindNamespaceDefinition,
		registry.KindNamespaceDependency,
		registry.KindNamespaceRequirement,
	}

	if slices.Contains(allowProcess, op.Entry.Kind) {
		// with entry events we dont propagate any events and handle them internally
		// use registry.entry for dynamic configs
		newState, err := br.builder.ApplyOperation(state, op)
		if err != nil {
			return state, fmt.Errorf("applying change to state: %w", err)
		}

		return newState, nil
	}

	// Clear any pending events from previous operations
	br.clearPendingEvents()

	br.bus.Send(ctx, event.Event{
		System: registry.System,
		Kind:   op.Kind,
		Path:   op.Entry.ID.String(),
		Data:   op.Entry,
	})

	// Wait for the specific event for this operation
	for {
		select {
		case confirmation := <-br.acceptChan:
			id := registry.ParseID(confirmation.Path)
			if id == op.Entry.ID {
				// Apply the change to the state
				newState, err := br.builder.ApplyOperation(state, op)
				if err != nil {
					return state, fmt.Errorf("applying change to state: %w", err)
				}
				return newState, nil
			}
			// Ignore events for other operations - they might be from previous operations
			br.log.Debug("ignoring accept event for different operation",
				zap.String("received_id", id.String()),
				zap.String("expected_id", op.Entry.ID.String()))

		case rejection := <-br.rejectChan:
			id := registry.ParseID(rejection.Path)
			if id == op.Entry.ID {
				err, ok := rejection.Data.(error)
				if !ok {
					return state, errors.New("operation rejected, no details")
				}
				return state, err
			}
			// Ignore events for other operations - they might be from previous operations
			br.log.Debug("ignoring reject event for different operation",
				zap.String("received_id", id.String()),
				zap.String("expected_id", op.Entry.ID.String()))

		case <-ctx.Done():
			return state, fmt.Errorf("failed to apply operation %s (%s): %w", op.Entry.ID, op.Entry.Kind, ctx.Err())
		}
	}
}

// clearPendingEvents clears any pending events from the accept and reject channels
func (br *BusRunner) clearPendingEvents() {
	// Drain accept channel
	for {
		select {
		case <-br.acceptChan:
			// Continue draining
		default:
			// Channel is empty
			goto acceptDone
		}
	}
acceptDone:

	// Drain reject channel
	for {
		select {
		case <-br.rejectChan:
			// Continue draining
		default:
			// Channel is empty
			goto rejectDone
		}
	}
rejectDone:
}

func (br *BusRunner) rollback(
	ctx context.Context,
	originalState, currentState topology.StateMap,
) topology.StateMap {
	br.log.Debug("starting rollback")

	// Convert states to registry.State format for BuildDelta
	fromState := currentState.ToSlice()
	toState := originalState.ToSlice()

	// Use BuildDelta to generate ordered operations
	delta, err := br.builder.BuildDelta(fromState, toState)
	if err != nil {
		br.log.Error("failed to build rollback delta", zap.Error(err))
		return currentState
	}

	br.log.Debug("rollback delta calculated", zap.Any("delta", delta))

	// Apply rollback operations
	for _, op := range delta {
		br.log.Debug("applying rollback operation",
			zap.String("kind", op.Kind),
			zap.String("id", op.Entry.ID.String()),
			zap.Any("meta", op.Entry.Meta))

		newState, err := br.applyOperation(ctx, currentState, op)
		if err != nil {
			br.log.Error("failed to apply rollback operation",
				zap.Any("operation", op),
				zap.Error(err))
			// Continue trying other operations instead of returning
			continue
		}
		currentState = newState
	}
	return currentState
}

// subscribeToEvents subscribes to Accept and Reject eventbus.
func (br *BusRunner) subscribeToEvents(ctx context.Context) error {
	var err error
	br.acceptSubID, err = br.bus.SubscribeP(ctx, registry.System, registry.Accept, br.acceptChan)
	if err != nil {
		br.log.Error("failed to subscribe to accept events", zap.Error(err))
		return fmt.Errorf("listening events: %w", err)
	}

	br.rejectSubID, err = br.bus.SubscribeP(ctx, registry.System, registry.Reject, br.rejectChan)
	if err != nil {
		br.log.Error("failed to subscribe to reject events, cleaning up accept subscription", zap.Error(err))
		br.bus.Unsubscribe(ctx, br.acceptSubID) // Clean up accept subscription if reject subscription fails
		return fmt.Errorf("listening events: %w", err)
	}

	return nil
}

// unsubscribeFromEvents unsubscribes from Accept and Reject eventbus.
func (br *BusRunner) unsubscribeFromEvents(ctx context.Context) {
	br.bus.Unsubscribe(ctx, br.acceptSubID)
	br.bus.Unsubscribe(ctx, br.rejectSubID)
}
