// SPDX-License-Identifier: MPL-2.0

// Package runner provides implementations for running registry operations
package runner

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"sync/atomic"
	"time"

	apierror "github.com/wippyai/runtime/api/error"
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
	bus                     event.Bus
	builder                 runnerBuilder
	dispatch                registry.DispatchPolicy
	transactionParticipants func() []string
	log                     *zap.Logger
	txSeq                   atomic.Uint64
	waitTimeout             time.Duration
}

const defaultEventWaitTimeout = event.DefaultAwaitTimeout

// Option configures BusRunner behavior.
type Option func(*BusRunner)

// WithDispatchPolicy sets the dispatch policy for operations.
func WithDispatchPolicy(policy registry.DispatchPolicy) Option {
	return func(br *BusRunner) {
		br.dispatch = policy
	}
}

// WithEventWaitTimeout sets how long the runner waits for accept/reject callbacks
// from registry listeners before timing out an operation.
func WithEventWaitTimeout(timeout time.Duration) Option {
	return func(br *BusRunner) {
		if timeout > 0 {
			br.waitTimeout = timeout
		}
	}
}

// WithTransactionParticipants configures the handlers that must acknowledge
// registry.begin/commit/discard before a transition can continue.
func WithTransactionParticipants(fn func() []string) Option {
	return func(br *BusRunner) {
		br.transactionParticipants = fn
	}
}

// NewBusRunner creates a new BusRunner. This is a sequential bus, order of operations matter.
func NewBusRunner(bus event.Bus, log *zap.Logger, builder runnerBuilder, opts ...Option) *BusRunner {
	br := &BusRunner{
		bus:         bus,
		log:         log,
		builder:     builder,
		waitTimeout: defaultEventWaitTimeout,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(br)
		}
	}
	return br
}

// Transition applies a series of operations to transform the registry from an
// initial state to a new state. The changeset is dispatched in the order
// provided by the upstream topological sort. Operations that reject with a
// NotFound-class error (indicating their listener could not find a sibling
// entry the listener depends on) are deferred and retried after the rest of
// the pass completes; the loop iterates until every operation has either been
// accepted, failed non-deferrably, or made zero progress in a full pass.
//
// This self-healing apply path makes boot ordering correct regardless of
// whether every cross-entry dependency is declared via a topology resolver
// pattern: declared deps short-circuit the loop to a single pass; undeclared
// deps converge in O(dependency depth) passes. The deterministic sort fixes
// in PR #270 are preserved upstream, so the order operations are tried in is
// stable across runs.
//
// If any operation fails non-deferrably, or no pass makes progress while
// rejections remain, every accepted operation is rolled back to the initial
// state and the transaction is discarded.
func (br *BusRunner) Transition(
	ctx context.Context,
	initialState registry.State,
	cs registry.ChangeSet,
) (registry.State, error) {
	currentState := newStateMap(initialState)
	originalState := newStateMap(initialState) // Keep a copy of the original state for rollbacks

	txPath := br.nextTransactionPath()
	txParticipants, err := br.registryTransactionParticipants()
	if err != nil {
		return stateMapToSlice(currentState), err
	}
	if err := br.dispatchTransaction(ctx, txParticipants, registry.TxBegin, txPath, nil); err != nil {
		return stateMapToSlice(currentState), err
	}

	remaining := append(registry.ChangeSet(nil), cs...)
	pass := 0

	for len(remaining) > 0 {
		pass++
		var (
			deferred       registry.ChangeSet
			lastDeferErr   error
			fatalErr       error
			fatalState     registry.StateMap
			progressed     bool
			retriedCount   int
			deferredFirstP = pass == 1
		)

		for _, op := range remaining {
			newState, opErr := br.applyOperation(ctx, currentState, op)
			if opErr == nil {
				currentState = newState
				progressed = true
				if !deferredFirstP {
					retriedCount++
					br.log.Info("operation accepted on retry",
						zap.String("entry_id", op.Entry.ID.String()),
						zap.String("op_kind", op.Kind),
						zap.Int("pass", pass))
				}
				continue
			}
			if ctx.Err() != nil {
				return nil, opErr
			}
			if isDeferrable(opErr) {
				lastDeferErr = opErr
				deferred = append(deferred, op)
				br.log.Info("operation deferred for retry",
					zap.String("entry_id", op.Entry.ID.String()),
					zap.String("op_kind", op.Kind),
					zap.Int("pass", pass),
					zap.Error(opErr))
				continue
			}
			fatalErr = opErr
			fatalState = newState
			break
		}

		if fatalErr != nil {
			br.log.Error("operation failed, initiating rollback", zap.Error(fatalErr))
			rolled := br.rollback(ctx, originalState, fatalState)
			if discardErr := br.dispatchTransaction(ctx, txParticipants, registry.TxDiscard, txPath, fatalErr); discardErr != nil {
				br.log.Error("failed to discard transaction", zap.Error(discardErr))
			}
			return stateMapToSlice(rolled), fatalErr
		}

		if len(deferred) == 0 {
			if retriedCount > 0 {
				br.log.Info("apply converged after deferral",
					zap.Int("passes", pass),
					zap.Int("retried_ops", retriedCount))
			}
			break
		}

		if !progressed {
			unresolved := make([]registry.ID, 0, len(deferred))
			for _, op := range deferred {
				unresolved = append(unresolved, op.Entry.ID)
			}
			finalErr := NewUnresolvedDependenciesError(unresolved, lastDeferErr)
			br.log.Warn("operations rejected after retries",
				zap.Int("passes", pass),
				zap.Strings("unresolved", idStrings(unresolved)),
				zap.Error(finalErr))
			rolled := br.rollback(ctx, originalState, currentState)
			if discardErr := br.dispatchTransaction(ctx, txParticipants, registry.TxDiscard, txPath, finalErr); discardErr != nil {
				br.log.Error("failed to discard transaction", zap.Error(discardErr))
			}
			return stateMapToSlice(rolled), finalErr
		}

		remaining = deferred
	}

	if err := br.dispatchTransaction(ctx, txParticipants, registry.TxCommit, txPath, nil); err != nil {
		br.log.Error("transaction commit failed, initiating rollback", zap.Error(err))
		newState := br.rollback(ctx, originalState, currentState)
		if discardErr := br.dispatchTransaction(ctx, txParticipants, registry.TxDiscard, txPath, err); discardErr != nil {
			br.log.Error("failed to discard transaction after commit failure", zap.Error(discardErr))
		}
		return stateMapToSlice(newState), err
	}

	return stateMapToSlice(currentState), nil
}

// isDeferrable reports whether a failed operation can be retried after the
// rest of the changeset has been dispatched. Listener errors are wrapped by
// NewOperationRejectedError as apierror.Invalid with the original error
// chained as Cause, so the unwrap walk inspects every layer and returns true
// if any of them carries apierror.NotFound.
func isDeferrable(err error) bool {
	cur := err
	for cur != nil {
		var apiErr apierror.Error
		if errors.As(cur, &apiErr) {
			if apiErr.Kind() == apierror.NotFound {
				return true
			}
			cur = errors.Unwrap(apiErr)
			continue
		}
		cur = errors.Unwrap(cur)
	}
	return false
}

func idStrings(ids []registry.ID) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = id.String()
	}
	return out
}

func (br *BusRunner) nextTransactionPath() event.Path {
	return "registry.tx/" + strconv.FormatUint(br.txSeq.Add(1), 10)
}

func (br *BusRunner) registryTransactionParticipants() ([]string, error) {
	if br.transactionParticipants == nil {
		return nil, nil
	}
	raw := br.transactionParticipants()
	seen := make(map[string]struct{}, len(raw))
	participants := make([]string, 0, len(raw))
	for _, id := range raw {
		if id == "" {
			continue
		}
		if _, exists := seen[id]; exists {
			return nil, NewDuplicateTransactionParticipantError(id)
		}
		seen[id] = struct{}{}
		participants = append(participants, id)
	}
	sort.Strings(participants)
	return participants, nil
}

type transactionWaiter struct {
	waiter event.AwaitWaiter
	id     string
}

func (br *BusRunner) dispatchTransaction(ctx context.Context, participants []string, kind event.Kind, txPath event.Path, data any) error {
	path := transactionEventPath(txPath, kind)
	waiters, err := br.prepareTransactionWaiters(ctx, participants, path)
	if err != nil {
		return err
	}
	defer closeTransactionWaiters(waiters)

	br.bus.Send(ctx, event.Event{
		System: registry.System,
		Kind:   kind,
		Path:   path,
		Data:   data,
	})

	accepted := 0
	rejected := false
	var rejectErr error
	for _, prepared := range waiters {
		result := prepared.waiter.Wait()
		if result.Accepted {
			accepted++
			br.log.Debug("received transaction accept event",
				zap.String("kind", kind),
				zap.String("path", path),
				zap.String("participant", prepared.id),
				zap.Int("accepted", accepted),
				zap.Int("expected", len(participants)))
			continue
		}

		if result.Event.Kind == "" {
			if ctx.Err() != nil {
				return NewTransactionRejectedError(kind, ctx.Err())
			}
			return NewTransactionTimeoutError(kind, br.waitTimeout, len(participants), accepted)
		}

		rejected = true
		if rejectErr == nil {
			rejectErr = result.Error
		} else if result.Error != nil {
			rejectErr = errors.Join(rejectErr, result.Error)
		}
	}
	if rejected {
		return NewTransactionRejectedError(kind, rejectErr)
	}
	return nil
}

func (br *BusRunner) prepareTransactionWaiters(ctx context.Context, participants []string, path event.Path) ([]transactionWaiter, error) {
	waiters := make([]transactionWaiter, 0, len(participants))
	for _, id := range participants {
		waiter, err := br.prepareWaiter(ctx, registry.TxResult, participantReplyPath(path, id))
		if err != nil {
			closeTransactionWaiters(waiters)
			return nil, err
		}
		waiters = append(waiters, transactionWaiter{id: id, waiter: waiter})
	}
	return waiters, nil
}

func closeTransactionWaiters(waiters []transactionWaiter) {
	for _, prepared := range waiters {
		prepared.waiter.Close()
	}
}

func transactionEventPath(txPath event.Path, kind event.Kind) event.Path {
	return txPath + "/" + kind
}

func participantReplyPath(path event.Path, participantID string) event.Path {
	return path + "/" + participantID
}

func (br *BusRunner) prepareWaiter(ctx context.Context, kind event.Kind, path event.Path) (event.AwaitWaiter, error) {
	awaitSvc := event.GetAwaitService(ctx)
	if awaitSvc == nil {
		return nil, NewAwaitServiceMissingError()
	}
	return awaitSvc.Prepare(ctx, registry.System, kind, path, br.waitTimeout)
}

func (br *BusRunner) applyOperation(
	ctx context.Context,
	state registry.StateMap,
	op registry.Operation,
) (registry.StateMap, error) {
	if err := br.builder.ValidateOperation(state, op); err != nil {
		existing, exists := state[op.Entry.ID]
		existingKind := ""
		if exists {
			existingKind = existing.Kind
		}
		br.log.Warn("invalid operation",
			zap.String("op_kind", op.Kind),
			zap.String("entry_id", op.Entry.ID.String()),
			zap.String("entry_kind", op.Entry.Kind),
			zap.Bool("exists", exists),
			zap.String("existing_kind", existingKind),
			zap.Error(err))
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

	mode := registry.DispatchEvents
	if br.dispatch != nil {
		mode = br.dispatch.Mode(op)
	}
	if mode == registry.DispatchInternal {
		// with entry events we dont propagate any events and handle them internally
		// use registry.entry for dynamic configs
		newState, err := br.builder.ApplyOperation(state, op)
		if err != nil {
			return state, NewApplyChangeError(err)
		}

		return newState, nil
	}

	waiter, err := br.prepareWaiter(ctx, registry.EntryResult, op.Entry.ID.String())
	if err != nil {
		return state, err
	}
	defer waiter.Close()

	// send the operation event
	br.bus.Send(ctx, event.Event{
		System: registry.System,
		Kind:   op.Kind,
		Path:   op.Entry.ID.String(),
		Data:   op.Entry,
	})

	result := waiter.Wait()
	if result.Accepted {
		br.log.Debug("received accept event",
			zap.String("id", op.Entry.ID.String()),
			zap.String("system", result.Event.System),
			zap.String("kind", result.Event.Kind))

		newState, err := br.builder.ApplyOperation(state, op)
		if err != nil {
			return state, NewApplyChangeError(err)
		}
		return newState, nil
	}

	if result.Event.Kind != "" {
		br.log.Debug("received reject event",
			zap.String("id", op.Entry.ID.String()))
		return state, NewOperationRejectedError(op.Entry.ID, result.Error)
	}

	if ctx.Err() != nil {
		return state, NewOperationCanceledError(op.Entry.ID, op.Entry.Kind, ctx.Err())
	}
	br.log.Error("event handler timeout - no listener responded",
		zap.String("id", op.Entry.ID.String()),
		zap.String("kind", op.Entry.Kind),
		zap.String("operation", op.Kind),
		zap.Duration("timeout", br.waitTimeout),
		zap.String("hint", "check if a listener is registered for this entry kind"))
	return state, NewEventHandlerTimeoutError(br.waitTimeout, op.Entry.ID, op.Entry.Kind)
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
