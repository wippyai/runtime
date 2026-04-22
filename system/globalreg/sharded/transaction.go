// SPDX-License-Identifier: MPL-2.0

package sharded

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	globalregapi "github.com/wippyai/runtime/api/globalreg"
	"github.com/wippyai/runtime/api/pid"
	syspg "github.com/wippyai/runtime/system/globalreg"
	"go.uber.org/zap"
)

// TransactionType indicates the type of cross-shard operation.
type TransactionType int

const (
	// TxnTypeRegister is a multi-name registration transaction.
	TxnTypeRegister TransactionType = iota
	// TxnTypeUnregister is a multi-name unregistration transaction.
	TxnTypeUnregister
)

// TransactionPhase represents the current phase of a 2PC transaction.
type TransactionPhase int32

const (
	// TxnPhaseInit is the initial state.
	TxnPhaseInit TransactionPhase = iota
	// TxnPhasePreparing is when prepare is being sent to all shards.
	TxnPhasePreparing
	// TxnPhasePrepared indicates all shards have prepared successfully.
	TxnPhasePrepared
	// TxnPhaseCommitting is when commit is being sent.
	TxnPhaseCommitting
	// TxnPhaseCommitted indicates transaction is complete.
	TxnPhaseCommitted
	// TxnPhaseAborting is when abort is being sent.
	TxnPhaseAborting
	// TxnPhaseAborted indicates transaction was aborted.
	TxnPhaseAborted
)

// ShardTransaction represents a shard's participation in a transaction.
// Uses atomic operations for lock-free state transitions.
type ShardTransaction struct {
	PrepareError error
	Names        []string
	ShardID      globalregapi.ShardID
	phase        atomic.Int32
	prepared     atomic.Bool
}

// SetPhase atomically sets the transaction phase.
func (st *ShardTransaction) SetPhase(phase TransactionPhase) {
	st.phase.Store(int32(phase))
}

// GetPhase atomically gets the transaction phase.
func (st *ShardTransaction) GetPhase() TransactionPhase {
	return TransactionPhase(st.phase.Load())
}

// SetPrepared atomically sets the prepared flag.
func (st *ShardTransaction) SetPrepared(v bool) {
	st.prepared.Store(v)
}

// IsPrepared atomically checks the prepared flag.
func (st *ShardTransaction) IsPrepared() bool {
	return st.prepared.Load()
}

// Transaction represents a distributed transaction across multiple shards.
// Uses atomic operations for thread-safe state management.
type Transaction struct {
	StartTime time.Time
	Shards    map[globalregapi.ShardID]*ShardTransaction
	PID       pid.PID
	ID        string
	Type      TransactionType
	phase     atomic.Int32
}

// SetPhase atomically sets the transaction phase.
func (t *Transaction) SetPhase(phase TransactionPhase) {
	t.phase.Store(int32(phase))
}

// GetPhase atomically gets the transaction phase.
func (t *Transaction) GetPhase() TransactionPhase {
	return TransactionPhase(t.phase.Load())
}

// TransactionManager coordinates 2PC distributed transactions with lock-free optimizations.
type TransactionManager struct {
	coordinator    *ShardCoordinator
	logger         *zap.Logger
	activeTxns     sync.Map
	timeout        time.Duration
	prepareTimeout time.Duration
	txnCounter     atomic.Uint64
}

// NewTransactionManager creates a new transaction manager.
func NewTransactionManager(logger *zap.Logger, coordinator *ShardCoordinator, timeout, prepareTimeout time.Duration) *TransactionManager {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &TransactionManager{
		coordinator:    coordinator,
		timeout:        timeout,
		prepareTimeout: prepareTimeout,
		logger:         logger.Named("txn-manager"),
	}
}

// ExecuteTransaction executes a cross-shard transaction using 2PC.
func (tm *TransactionManager) ExecuteTransaction(ctx context.Context, names []string, p pid.PID, txnType TransactionType) error {
	// Create transaction
	txn := &Transaction{
		ID:        uuid.New().String(),
		Type:      txnType,
		PID:       p,
		Shards:    make(map[globalregapi.ShardID]*ShardTransaction),
		StartTime: time.Now(),
	}
	txn.SetPhase(TxnPhaseInit)

	// Group names by shard
	shardGroups := tm.coordinator.groupNamesByShard(names)

	// Create shard transactions
	for shardID, shardNames := range shardGroups {
		txn.Shards[shardID] = &ShardTransaction{
			ShardID: shardID,
			Names:   shardNames,
		}
		txn.Shards[shardID].SetPhase(TxnPhaseInit)
	}

	// Register active transaction (lock-free)
	tm.activeTxns.Store(txn.ID, txn)
	tm.txnCounter.Add(1)

	defer func() {
		tm.activeTxns.Delete(txn.ID)
	}()

	tm.logger.Info("starting 2PC transaction",
		zap.String("txn_id", txn.ID),
		zap.Int("shard_count", len(txn.Shards)),
		zap.Int("name_count", len(names)),
	)

	// Phase 1: Prepare
	if err := tm.preparePhase(ctx, txn); err != nil {
		tm.logger.Warn("prepare phase failed, aborting",
			zap.String("txn_id", txn.ID),
			zap.Error(err),
		)
		// Abort all shards
		tm.abortPhase(ctx, txn)
		return fmt.Errorf("transaction %s aborted: %w", txn.ID, err)
	}

	// Phase 2: Commit
	if err := tm.commitPhase(ctx, txn); err != nil {
		tm.logger.Error("commit phase failed",
			zap.String("txn_id", txn.ID),
			zap.Error(err),
		)
		return fmt.Errorf("transaction %s commit failed: %w", txn.ID, err)
	}

	tm.logger.Info("transaction committed successfully",
		zap.String("txn_id", txn.ID),
		zap.Duration("duration", time.Since(txn.StartTime)),
	)

	return nil
}

// preparePhase executes the prepare phase of 2PC.
func (tm *TransactionManager) preparePhase(ctx context.Context, txn *Transaction) error {
	txn.SetPhase(TxnPhasePreparing)
	tm.logger.Debug("prepare phase started", zap.String("txn_id", txn.ID))

	// Prepare on each shard concurrently
	var wg sync.WaitGroup
	errorChan := make(chan error, len(txn.Shards))

	for _, shardTxn := range txn.Shards {
		wg.Add(1)
		go func(st *ShardTransaction) {
			defer wg.Done()
			if err := tm.prepareShard(ctx, txn, st); err != nil {
				errorChan <- err
			}
		}(shardTxn)
	}

	wg.Wait()
	close(errorChan)

	// Check for errors
	var firstErr error
	for err := range errorChan {
		if firstErr == nil {
			firstErr = err
		}
	}

	if firstErr != nil {
		return firstErr
	}

	// All shards prepared successfully
	txn.SetPhase(TxnPhasePrepared)
	tm.logger.Debug("all shards prepared", zap.String("txn_id", txn.ID))
	return nil
}

// prepareShard prepares a single shard for the transaction.
func (tm *TransactionManager) prepareShard(_ context.Context, txn *Transaction, shardTxn *ShardTransaction) error {
	shard, ok := tm.coordinator.getShard(shardTxn.ShardID)
	if !ok {
		return fmt.Errorf("shard %d not found", shardTxn.ShardID)
	}

	shardTxn.SetPhase(TxnPhasePreparing)

	// Check preconditions and acquire reservations based on transaction type
	switch txn.Type {
	case TxnTypeRegister:
		// For registration: names should NOT exist and not be reserved by others
		for i, name := range shardTxn.Names {
			_, exists := shard.Lookup(name)
			if exists {
				// Release reservations acquired so far in this prepare
				for j := 0; j < i; j++ {
					shard.ReleaseReservation(shardTxn.Names[j], txn.ID)
				}
				return fmt.Errorf("name %s already registered", name)
			}
			// Reserve name to prevent concurrent transactions from preparing it
			if err := shard.Reserve(name, txn.ID); err != nil {
				// Release reservations acquired so far
				for j := 0; j < i; j++ {
					shard.ReleaseReservation(shardTxn.Names[j], txn.ID)
				}
				return err
			}
		}

	case TxnTypeUnregister:
		// For unregistration: names SHOULD exist
		for _, name := range shardTxn.Names {
			_, exists := shard.Lookup(name)
			if !exists {
				return fmt.Errorf("name %s not found", name)
			}
		}
	}

	// Mark as prepared (atomic)
	shardTxn.SetPrepared(true)
	shardTxn.SetPhase(TxnPhasePrepared)

	return nil
}

// commitPhase executes the commit phase of 2PC.
func (tm *TransactionManager) commitPhase(ctx context.Context, txn *Transaction) error {
	txn.SetPhase(TxnPhaseCommitting)
	tm.logger.Debug("commit phase started", zap.String("txn_id", txn.ID))

	var wg sync.WaitGroup
	errorChan := make(chan error, len(txn.Shards))

	for _, shardTxn := range txn.Shards {
		wg.Add(1)
		go func(st *ShardTransaction) {
			defer wg.Done()
			if err := tm.commitShard(ctx, txn, st); err != nil {
				errorChan <- err
			}
		}(shardTxn)
	}

	wg.Wait()
	close(errorChan)

	var firstErr error
	for err := range errorChan {
		if firstErr == nil {
			firstErr = err
		}
	}

	if firstErr != nil {
		return firstErr
	}

	txn.SetPhase(TxnPhaseCommitted)
	return nil
}

// commitShard commits a single shard.
func (tm *TransactionManager) commitShard(_ context.Context, txn *Transaction, shardTxn *ShardTransaction) error {
	shard, ok := tm.coordinator.getShard(shardTxn.ShardID)
	if !ok {
		return fmt.Errorf("shard %d not found", shardTxn.ShardID)
	}

	// Execute the actual operation based on transaction type
	switch txn.Type {
	case TxnTypeRegister:
		// Register all names and release reservations
		for _, name := range shardTxn.Names {
			cmd := &syspg.Command{
				Type:   syspg.CmdRegister,
				Name:   name,
				PID:    txn.PID,
				NodeID: txn.PID.Node,
			}
			if _, err := shard.Apply(cmd); err != nil {
				// Release remaining reservations on failure
				shard.ReleaseAllReservations(txn.ID)
				return err
			}
			shard.ReleaseReservation(name, txn.ID)
		}

	case TxnTypeUnregister:
		// Unregister all names
		for _, name := range shardTxn.Names {
			cmd := &syspg.Command{
				Type: syspg.CmdUnregister,
				Name: name,
			}
			if _, err := shard.Apply(cmd); err != nil {
				return err
			}
		}
	}

	shardTxn.SetPhase(TxnPhaseCommitted)
	return nil
}

// abortPhase executes the abort phase of 2PC.
func (tm *TransactionManager) abortPhase(_ context.Context, txn *Transaction) {
	txn.SetPhase(TxnPhaseAborting)
	tm.logger.Debug("abort phase started", zap.String("txn_id", txn.ID))

	// Release reservations and reset prepared flags for all shards
	for _, shardTxn := range txn.Shards {
		if shardTxn.IsPrepared() {
			// Release all reservations held by this transaction on this shard
			if shard, ok := tm.coordinator.getShard(shardTxn.ShardID); ok {
				shard.ReleaseAllReservations(txn.ID)
			}
			shardTxn.SetPrepared(false)
			shardTxn.SetPhase(TxnPhaseAborted)
		}
	}

	txn.SetPhase(TxnPhaseAborted)
}

// GetActiveTransactions returns all active transactions (lock-free iteration).
func (tm *TransactionManager) GetActiveTransactions() []*Transaction {
	txns := make([]*Transaction, 0)
	tm.activeTxns.Range(func(_, value any) bool {
		txns = append(txns, value.(*Transaction))
		return true
	})
	return txns
}

// GetTransactionCount returns the total number of transactions executed (atomic).
func (tm *TransactionManager) GetTransactionCount() uint64 {
	return tm.txnCounter.Load()
}
