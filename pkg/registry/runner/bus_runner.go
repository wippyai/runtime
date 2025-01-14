package runner

import (
	"context"
	"errors"
	"fmt"

	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/registry"
	"go.uber.org/zap"
)

type BusRunner struct {
	bus         events.Bus
	log         *zap.Logger
	acceptChan  chan events.Event
	rejectChan  chan events.Event
	acceptSubID events.SubscriberID
	rejectSubID events.SubscriberID
	stateHelper *stateHelper
}

// NewBusRunner creates a new BusRunner. This is a sequential bus, order of operations matter.
func NewBusRunner(bus events.Bus, log *zap.Logger) *BusRunner {
	return &BusRunner{
		bus:         bus,
		log:         log,
		acceptChan:  make(chan events.Event),
		rejectChan:  make(chan events.Event),
		stateHelper: newStateHelper(log),
	}
}

func (br *BusRunner) Transition(
	ctx context.Context,
	initialState registry.State,
	cs registry.ChangeSet,
) (registry.State, error) {
	currentState := br.stateHelper.toMap(initialState)
	originalState := br.stateHelper.toMap(initialState) // Keep a copy of the original state for rollbacks
	appliedOperations := make([]registry.Operation, 0)

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
			newState = br.rollback(ctx, originalState, newState, appliedOperations)

			// Only send Discard if there was an error, and rollback already happened
			br.bus.Send(ctx, events.Event{
				System: registry.System,
				Kind:   registry.Discard,
				Data:   err,
			})

			return br.stateHelper.toSlice(newState), fmt.Errorf("operation failed: %w", err)
		}

		currentState = newState
		appliedOperations = append(appliedOperations, op)
	}

	br.bus.Send(ctx, events.Event{
		System: registry.System,
		Kind:   registry.Commit,
	})

	return br.stateHelper.toSlice(currentState), nil
}

func (br *BusRunner) applyOperation(ctx context.Context, state stateMap, op registry.Operation) (stateMap, error) {
	// todo: uncomment and properly test
	//if err := br.stateHelper.validateOperation(state, op); err != nil {
	//	return state, fmt.Errorf("invalid operation: %w", err)
	//}

	// send the operation event
	br.bus.Send(ctx, events.Event{
		System: registry.System,
		Kind:   op.Kind,
		Path:   events.Path(op.Entry.ID),
		Data:   op.Entry,
	})

	for {
		select {
		case confirmation := <-br.acceptChan:
			id := registry.ID(confirmation.Path)

			if id != op.Entry.ID {
				return state, errors.New("unrelated accept event")
			}

			// Apply the change to the state
			var err error
			newState, err := br.stateHelper.applyChangeToState(state, op)
			if err != nil {
				// Even if applyChangeToState fails, we return the original state to maintain consistency
				return state, fmt.Errorf("applying change to state: %w", err)
			}

			return newState, nil
		case rejection := <-br.rejectChan:
			id := registry.ID(rejection.Path)

			if id != op.Entry.ID {
				return state, errors.New("unrelated reject event")
			}

			err, ok := rejection.Data.(error)
			if !ok {
				return state, errors.New("operation rejected, no details")
			}
			return state, err

		case <-ctx.Done():
			// Return the original state in case of timeout/cancellation to maintain consistency
			return state, fmt.Errorf("failed to apply operation %s: %w", op.Entry.ID, ctx.Err())
		}
	}
}

func (br *BusRunner) rollback(
	ctx context.Context,
	originalState,
	currentState stateMap,
	appliedOperations []registry.Operation,
) stateMap {
	// Iterate in reverse order
	for i := len(appliedOperations) - 1; i >= 0; i-- {
		op := appliedOperations[i]
		inverseOp, err := br.stateHelper.getInverseOperation(originalState, op)
		if err != nil {
			br.log.Error("error getting inverse operation", zap.Error(err))
			continue
		}

		newState, err := br.applyOperation(ctx, currentState, inverseOp)
		if err != nil {
			br.log.Warn("failed to rollback operation", zap.Any("operation", op))
			return newState
		}

		// Apply the inverse operation to the state
		currentState, err = br.stateHelper.applyChangeToState(currentState, inverseOp)
		if err != nil {
			br.log.Error("error applying rollback operation", zap.Error(err))
		}
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
