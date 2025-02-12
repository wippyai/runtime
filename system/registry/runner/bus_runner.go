// Package runner provides implementations for running registry operations
package runner

import (
	"context"
	"errors"
	"fmt"

	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/system/registry/topology"
	"go.uber.org/zap"
)

// BusRunner executes registry operations sequentially through an event bus, handling
// state transitions, rollbacks, and error handling. It maintains operation order
// and provides transactional semantics through the event bus.
type BusRunner struct {
	bus         events.Bus
	log         *zap.Logger
	acceptChan  chan events.Event
	rejectChan  chan events.Event
	acceptSubID events.SubscriberID
	rejectSubID events.SubscriberID
	builder     *topology.StateBuilder
}

// NewBusRunner creates a new BusRunner. This is a sequential bus, order of operations matter.
func NewBusRunner(bus events.Bus, log *zap.Logger) *BusRunner {
	return &BusRunner{
		bus:        bus,
		log:        log,
		acceptChan: make(chan events.Event),
		rejectChan: make(chan events.Event),
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

	br.bus.Send(ctx, events.Event{
		System: registry.System,
		Kind:   registry.Begin,
	})

	for _, op := range cs {
		newState, err := br.applyOperation(ctx, currentState, op)
		if err != nil {
			if ctx.Err() != nil {
				return nil, err
			}

			br.log.Warn("operation failed, initiating rollback", zap.Any("operation", op), zap.Error(err))
			newState = br.rollback(ctx, originalState, newState)

			// Only send Discard if there was an error, and rollback already happened
			br.bus.Send(ctx, events.Event{
				System: registry.System,
				Kind:   registry.Discard,
				Data:   err,
			})

			return newState.ToSlice(), fmt.Errorf("operation failed: %w", err)
		}

		currentState = newState
	}

	br.bus.Send(ctx, events.Event{
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

	br.log.Debug("starting operation",
		zap.String("kind", op.Kind),
		zap.String("id", op.Entry.ID.String()),
		zap.Any("meta", op.Entry.Meta))

	// send the operation event
	br.bus.Send(ctx, events.Event{
		System: registry.System,
		Kind:   op.Kind,
		Path:   op.Entry.ID.String(),
		Data:   op.Entry,
	})

	for {
		select {
		case confirmation := <-br.acceptChan:
			id := registry.ParseID(confirmation.Path)
			br.log.Debug("received accept event",
				zap.String("id", id.String()),
				zap.String("expected", op.Entry.ID.String()))

			if id != op.Entry.ID {
				return state, errors.New("unrelated accept event")
			}

			// Apply the change to the state
			newState, err := br.builder.ApplyOperation(state, op)
			if err != nil {
				return state, fmt.Errorf("applying change to state: %w", err)
			}

			return newState, nil

		case rejection := <-br.rejectChan:
			id := registry.ParseID(rejection.Path)
			br.log.Debug("received reject event",
				zap.String("id", id.String()),
				zap.String("expected", op.Entry.ID.String()),
				zap.Any("data", rejection.Data))

			if id != op.Entry.ID {
				return state, errors.New("unrelated reject event")
			}

			err, ok := rejection.Data.(error)
			if !ok {
				return state, errors.New("operation rejected, no details")
			}
			return state, err

		case <-ctx.Done():
			return state, fmt.Errorf("failed to apply operation %s (%s): %w", op.Entry.ID, op.Entry.Kind, ctx.Err())
		}
	}
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
		return fmt.Errorf("listening events: %w", err)
	}

	br.rejectSubID, err = br.bus.SubscribeP(ctx, registry.System, registry.Reject, br.rejectChan)
	if err != nil {
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
