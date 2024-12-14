package runner

import (
	"context"
	"errors"
	"fmt"

	"go.uber.org/zap"

	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/registry"
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
	var finalErr error

	if err := br.subscribeToEvents(ctx); err != nil {
		return nil, err
	}
	defer br.unsubscribeFromEvents(ctx)

	for _, op := range cs {
		newState, err := br.applyOperation(ctx, currentState, op)
		if err != nil {
			br.log.Warn("Operation failed, initiating rollback", zap.Any("operation", op))
			newState = br.rollback(originalState, newState, appliedOperations)
			return br.stateHelper.toSlice(newState), fmt.Errorf("operation failed: %w", err)
		}

		currentState = newState
		appliedOperations = append(appliedOperations, op)
	}

	return br.stateHelper.toSlice(currentState), finalErr
}

func (br *BusRunner) applyOperation(ctx context.Context, state stateMap, op registry.Operation) (stateMap, error) {
	// Send the operation event
	br.bus.Send(ctx, events.Event{
		System: registry.System,
		Kind:   op.Kind,
		Data:   op.Entry,
	})

	for {
		select {
		case confirmation := <-br.acceptChan:
			entry, ok := confirmation.Data.(registry.Entry)
			if !ok {
				br.log.Warn("Received event with unexpected data type",
					zap.String("expected_type", "registry.Entry"),
					zap.Any("got_type", fmt.Sprintf("%T", confirmation.Data)), // Log the actual type
					zap.String("event_kind", string(confirmation.Kind)),
				)
				continue // Skip to the next iteration of the select loop
			}

			if entry.Path != op.Entry.Path {
				return state, errors.New("unrelated accept event")
			}

			// Apply the change to the state
			var err error
			newState, err := br.stateHelper.applyChangeToState(state, op)
			if err != nil {
				// Even if applyChangeToState fails, we return the original state to maintain consistency
				return state, fmt.Errorf("applying change to state: %w", err)
			}
			state = newState

		case rejection := <-br.rejectChan:
			// Type assertion: Check if rejection.Data is of type registry.Entry
			entry, ok := rejection.Data.(registry.Entry)
			if !ok {
				br.log.Error("Received event with unexpected data type",
					zap.String("expected_type", "registry.Entry"),
					zap.Any("got_type", fmt.Sprintf("%T", rejection.Data)), // Log the actual type
					zap.String("event_kind", string(rejection.Kind)))
				continue // Skip to the next iteration of the select loop
			}

			if entry.Path != op.Entry.Path {
				return state, errors.New("unrelated accept event")
			}

			return state, errors.New("operation rejected") // todo: propagate entity level error

		case <-ctx.Done():
			// Return the original state in case of timeout/cancellation to maintain consistency
			return state, ctx.Err()
		}
	}
}

func (br *BusRunner) rollback(originalState, currentState stateMap, appliedOperations []registry.Operation) stateMap {
	// Iterate in reverse order
	for i := len(appliedOperations) - 1; i >= 0; i-- {
		op := appliedOperations[i]
		inverseOp, err := br.stateHelper.getInverseOperation(originalState, op)
		if err != nil {
			br.log.Error("Error getting inverse operation", zap.Error(err))
			continue
		}

		// Apply the inverse operation to the state
		currentState, err = br.stateHelper.applyChangeToState(currentState, inverseOp)
		if err != nil {
			br.log.Error("Error applying rollback operation", zap.Error(err))
		}
	}
	return currentState
}

// subscribeToEvents subscribes to Accept and Reject events.
func (br *BusRunner) subscribeToEvents(ctx context.Context) error {
	var err error
	br.acceptSubID, err = br.bus.SubscribeP(ctx, registry.System, registry.Accept, br.acceptChan)
	if err != nil {
		return fmt.Errorf("subscribing to accept events: %w", err)
	}

	br.rejectSubID, err = br.bus.SubscribeP(ctx, registry.System, registry.Reject, br.rejectChan)
	if err != nil {
		br.bus.Unsubscribe(ctx, br.acceptSubID) // Clean up accept subscription if reject subscription fails
		return fmt.Errorf("subscribing to reject events: %w", err)
	}

	return nil
}

// unsubscribeFromEvents unsubscribes from Accept and Reject events.
func (br *BusRunner) unsubscribeFromEvents(ctx context.Context) {
	br.bus.Unsubscribe(ctx, br.acceptSubID)
	br.bus.Unsubscribe(ctx, br.rejectSubID)
}
