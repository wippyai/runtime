// Package runner provides implementations for running registry operations
package runner

import (
	"context"
	"slices"
	"time"

	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/registry"
	"go.uber.org/zap"
)

// runnerBuilder defines the operations needed by BusRunner for state transitions
type runnerBuilder interface {
	ValidateOperation(registry.StateMap, registry.Operation) error
	ApplyOperation(registry.StateMap, registry.Operation) (registry.StateMap, error)
	BuildDelta(registry.State, registry.State) (registry.ChangeSet, error)
}

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
	builder     runnerBuilder
}

const eventWaitTimeout = 30 * time.Second

// NewBusRunner creates a new BusRunner. This is a sequential bus, order of operations matter.
func NewBusRunner(bus event.Bus, log *zap.Logger, builder runnerBuilder) *BusRunner {
	return &BusRunner{
		bus:        bus,
		log:        log,
		acceptChan: make(chan event.Event),
		rejectChan: make(chan event.Event),
		builder:    builder,
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
	currentState := newStateMap(initialState)
	originalState := newStateMap(initialState) // Keep a copy of the original state for rollbacks

	if err := br.subscribeToEvents(ctx); err != nil {
		return nil, err
	}
	defer br.unsubscribeFromEvents(ctx)

	br.bus.Send(ctx, event.Event{
		System: registry.System,
		Kind:   registry.Begin,
	})

	for _, op := range cs {
		newState, err := br.applyOperation(ctx, currentState, op)
		if err != nil {
			if ctx.Err() != nil {
				return nil, err
			}

			br.log.Error("operation failed, initiating rollback", zap.Any("operation", op), zap.Error(err))
			newState = br.rollback(ctx, originalState, newState)

			// Only send Discard if there was an error, and rollback already happened
			br.bus.Send(ctx, event.Event{
				System: registry.System,
				Kind:   registry.Discard,
				Data:   err,
			})

			return stateMapToSlice(newState), NewOperationFailedError(err)
		}

		currentState = newState
	}

	br.bus.Send(ctx, event.Event{
		System: registry.System,
		Kind:   registry.Commit,
	})

	return stateMapToSlice(currentState), nil
}

func (br *BusRunner) applyOperation(
	ctx context.Context,
	state registry.StateMap,
	op registry.Operation,
) (registry.StateMap, error) {
	if err := br.builder.ValidateOperation(state, op); err != nil {
		return state, NewInvalidOperationError(err)
	}

	if op.Entry.Kind == "" {
		// resolve from reg or fail
		entry, ok := state[op.Entry.ID]
		if !ok {
			return state, NewEntryKindNotFoundError(op.Entry.ID)
		}

		op.Entry.Kind = entry.Kind
	}

	allowProcess := []registry.Kind{
		registry.EntryKind,
		registry.NamespaceDependencyKind,
		registry.NamespaceRequirementKind,
	}
	if slices.Contains(allowProcess, op.Entry.Kind) {
		// with entry events we dont propagate any events and handle them internally
		// use registry.entry for dynamic configs
		newState, err := br.builder.ApplyOperation(state, op)
		if err != nil {
			return state, NewApplyChangeError(err)
		}

		return newState, nil
	}

	// send the operation event
	br.bus.Send(ctx, event.Event{
		System: registry.System,
		Kind:   op.Kind,
		Path:   op.Entry.ID.String(),
		Data:   op.Entry,
	})

	timeoutCtx, cancel := context.WithTimeout(ctx, eventWaitTimeout)
	defer cancel()

	for {
		select {
		case confirmation := <-br.acceptChan:
			id := registry.ParseID(confirmation.Path)
			br.log.Debug("received accept event",
				zap.String("id", id.String()),
				zap.String("expected", op.Entry.ID.String()),
				zap.String("system", confirmation.System),
				zap.String("kind", confirmation.Kind))

			if !id.Equal(op.Entry.ID) {
				br.log.Error("unrelated accept event details",
					zap.String("received_id", id.String()),
					zap.String("expected_id", op.Entry.ID.String()),
					zap.String("expected_kind", op.Entry.Kind))
				return state, ErrUnrelatedAcceptEvent
			}

			// Apply the change to the state
			newState, err := br.builder.ApplyOperation(state, op)
			if err != nil {
				return state, NewApplyChangeError(err)
			}

			return newState, nil

		case rejection := <-br.rejectChan:
			id := registry.ParseID(rejection.Path)
			br.log.Debug("received reject event",
				zap.String("id", id.String()),
				zap.String("expected", op.Entry.ID.String()))

			if !id.Equal(op.Entry.ID) {
				return state, ErrUnrelatedRejectEvent
			}

			err, ok := rejection.Data.(error)
			if !ok {
				return state, NewOperationRejectedError(op.Entry.ID, nil)
			}
			return state, NewOperationRejectedError(op.Entry.ID, err)

		case <-timeoutCtx.Done():
			if ctx.Err() != nil {
				return state, NewOperationCanceledError(op.Entry.ID, op.Entry.Kind, ctx.Err())
			}
			br.log.Error("event handler timeout - no listener responded",
				zap.String("id", op.Entry.ID.String()),
				zap.String("kind", op.Entry.Kind),
				zap.String("operation", op.Kind),
				zap.Duration("timeout", eventWaitTimeout),
				zap.String("hint", "check if a listener is registered for this entry kind"))
			return state, NewEventHandlerTimeoutError(eventWaitTimeout, op.Entry.ID, op.Entry.Kind)
		}
	}
}

func (br *BusRunner) rollback(
	ctx context.Context,
	originalState, currentState registry.StateMap,
) registry.StateMap {
	br.log.Debug("starting rollback")

	// Convert states to registry.State format for BuildDelta
	fromState := stateMapToSlice(currentState)
	toState := stateMapToSlice(originalState)

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
		return NewListenEventsError(err)
	}

	br.rejectSubID, err = br.bus.SubscribeP(ctx, registry.System, registry.Reject, br.rejectChan)
	if err != nil {
		br.bus.Unsubscribe(ctx, br.acceptSubID) // Clean up accept subscription if reject subscription fails
		return NewListenEventsError(err)
	}

	return nil
}

// unsubscribeFromEvents unsubscribes from Accept and Reject eventbus.
func (br *BusRunner) unsubscribeFromEvents(ctx context.Context) {
	br.bus.Unsubscribe(ctx, br.acceptSubID)
	br.bus.Unsubscribe(ctx, br.rejectSubID)
}

// newStateMap creates a StateMap from a State slice
func newStateMap(state registry.State) registry.StateMap {
	m := make(registry.StateMap)
	for _, entry := range state {
		m[entry.ID] = entry
	}
	return m
}

// stateMapToSlice converts a StateMap to a State slice
func stateMapToSlice(sm registry.StateMap) registry.State {
	slice := make(registry.State, 0, len(sm))
	for _, entry := range sm {
		slice = append(slice, entry)
	}
	return slice
}

// copyStateMap creates a shallow copy of a StateMap
func copyStateMap(sm registry.StateMap) registry.StateMap {
	newMap := make(registry.StateMap, len(sm))
	for k, v := range sm {
		newMap[k] = v
	}
	return newMap
}
