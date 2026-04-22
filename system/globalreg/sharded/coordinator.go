// SPDX-License-Identifier: MPL-2.0

package sharded

import (
	"context"
	"fmt"
	"hash/fnv"
	"sync"
	"sync/atomic"
	"time"

	globalregapi "github.com/wippyai/runtime/api/globalreg"
	"github.com/wippyai/runtime/api/pid"
	syspg "github.com/wippyai/runtime/system/globalreg"
	"go.uber.org/zap"
)

// Default configuration constants.
const (
	DefaultShardCount     = 8
	DefaultHashSeed       = 0xdeadbeef
	DefaultTxnTimeout     = 10 * time.Second
	DefaultPrepareTimeout = 5 * time.Second
)

// ShardCoordinator manages multiple shards with lock-free optimizations.
// Uses sync.Map for shard storage and atomic operations for counters.
type ShardCoordinator struct {
	txnMgr     *TransactionManager
	logger     *zap.Logger
	shards     sync.Map
	shardCount uint32
	hashSeed   uint32
}

// Config holds sharded registry configuration.
type Config struct {
	ShardCount     uint32
	HashSeed       uint32
	TxnTimeout     time.Duration
	PrepareTimeout time.Duration
}

// NewShardCoordinator creates a new sharded registry coordinator.
func NewShardCoordinator(logger *zap.Logger, config *Config) (*ShardCoordinator, error) {
	if logger == nil {
		logger = zap.NewNop()
	}

	if config == nil {
		config = &Config{
			ShardCount:     DefaultShardCount,
			HashSeed:       DefaultHashSeed,
			TxnTimeout:     DefaultTxnTimeout,
			PrepareTimeout: DefaultPrepareTimeout,
		}
	}

	if config.ShardCount == 0 {
		return nil, globalregapi.ErrInvalidShardCount
	}

	sc := &ShardCoordinator{
		shardCount: config.ShardCount,
		hashSeed:   config.HashSeed,
		logger:     logger.Named("sharded-coordinator"),
	}

	// Initialize transaction manager
	sc.txnMgr = NewTransactionManager(logger, sc, config.TxnTimeout, config.PrepareTimeout)

	// Initialize shards using sync.Map (lock-free)
	for i := uint32(0); i < config.ShardCount; i++ {
		shardID := globalregapi.ShardID(i)
		shard := NewShard(shardID, syspg.NewFSM(), logger)
		sc.shards.Store(shardID, shard)
	}

	logger.Info("sharded registry coordinator initialized",
		zap.Uint32("shard_count", config.ShardCount),
		zap.Uint32("hash_seed", config.HashSeed),
	)

	return sc, nil
}

// Register associates a name with a PID in the appropriate shard.
func (sc *ShardCoordinator) Register(_ context.Context, name string, p pid.PID) (pid.PID, error) {
	shardID := sc.getShardID(name)
	shard, ok := sc.getShard(shardID)
	if !ok {
		return pid.PID{}, globalregapi.ErrShardNotFound
	}

	cmd := &syspg.Command{
		Type:   syspg.CmdRegister,
		Name:   name,
		PID:    p,
		NodeID: p.Node,
	}

	result, err := shard.Apply(cmd)
	if err != nil {
		return pid.PID{}, err
	}

	regResult, ok := result.(*syspg.RegisterResult)
	if !ok {
		return pid.PID{}, fmt.Errorf("unexpected result type: %T", result)
	}

	if regResult.Err != nil {
		return regResult.ExistingPID, regResult.Err
	}

	return regResult.PID, nil
}

// RegisterMulti atomically registers multiple names using 2PC.
func (sc *ShardCoordinator) RegisterMulti(ctx context.Context, names []string, p pid.PID) error {
	if len(names) == 0 {
		return nil
	}

	// Single name optimization
	if len(names) == 1 {
		_, err := sc.Register(ctx, names[0], p)
		return err
	}

	// Group names by shard
	shardGroups := sc.groupNamesByShard(names)

	// If all names in same shard, use simple local transaction
	if len(shardGroups) == 1 {
		return sc.registerSingleShard(ctx, names, p)
	}

	// Multi-shard: use 2PC
	return sc.txnMgr.ExecuteTransaction(ctx, names, p, TxnTypeRegister)
}

// registerSingleShard registers multiple names within one shard atomically.
// On partial failure, previously registered names are rolled back.
func (sc *ShardCoordinator) registerSingleShard(_ context.Context, names []string, p pid.PID) error {
	shardID := sc.getShardID(names[0])
	shard, ok := sc.getShard(shardID)
	if !ok {
		return globalregapi.ErrShardNotFound
	}

	var registered []string

	// Register each name, tracking successes for rollback
	for _, name := range names {
		cmd := &syspg.Command{
			Type:   syspg.CmdRegister,
			Name:   name,
			PID:    p,
			NodeID: p.Node,
		}

		result, err := shard.Apply(cmd)
		if err != nil {
			sc.rollbackRegistered(shard, registered)
			return err
		}

		regResult, ok := result.(*syspg.RegisterResult)
		if !ok {
			sc.rollbackRegistered(shard, registered)
			return fmt.Errorf("unexpected result type: %T", result)
		}

		if regResult.Err != nil {
			sc.rollbackRegistered(shard, registered)
			return regResult.Err
		}

		registered = append(registered, name)
	}

	return nil
}

// rollbackRegistered unregisters previously registered names as a compensating action.
func (sc *ShardCoordinator) rollbackRegistered(shard *Shard, names []string) {
	for i := len(names) - 1; i >= 0; i-- {
		cmd := &syspg.Command{Type: syspg.CmdUnregister, Name: names[i]}
		if _, err := shard.Apply(cmd); err != nil {
			sc.logger.Error("rollback failed during compensating unregister",
				zap.String("name", names[i]),
				zap.Error(err),
			)
		}
	}
}

// Unregister removes a name from its shard.
func (sc *ShardCoordinator) Unregister(_ context.Context, name string) (bool, error) {
	shardID := sc.getShardID(name)
	shard, ok := sc.getShard(shardID)
	if !ok {
		return false, globalregapi.ErrShardNotFound
	}

	cmd := &syspg.Command{Type: syspg.CmdUnregister, Name: name}
	result, err := shard.Apply(cmd)
	if err != nil {
		return false, err
	}

	unregResult, ok := result.(*syspg.UnregisterResult)
	if !ok {
		return false, fmt.Errorf("unexpected result type: %T", result)
	}

	return unregResult.Removed, nil
}

// UnregisterMulti atomically unregisters multiple names.
func (sc *ShardCoordinator) UnregisterMulti(ctx context.Context, names []string) error {
	if len(names) == 0 {
		return nil
	}

	// Single name optimization
	if len(names) == 1 {
		_, err := sc.Unregister(ctx, names[0])
		return err
	}

	// Group names by shard
	shardGroups := sc.groupNamesByShard(names)

	// If all names in same shard, use simple local operation
	if len(shardGroups) == 1 {
		return sc.unregisterSingleShard(ctx, names)
	}

	// Multi-shard: use 2PC
	return sc.txnMgr.ExecuteTransaction(ctx, names, pid.PID{}, TxnTypeUnregister)
}

// unregisterSingleShard unregisters multiple names within one shard.
func (sc *ShardCoordinator) unregisterSingleShard(_ context.Context, names []string) error {
	shardID := sc.getShardID(names[0])
	shard, ok := sc.getShard(shardID)
	if !ok {
		return globalregapi.ErrShardNotFound
	}

	for _, name := range names {
		cmd := &syspg.Command{Type: syspg.CmdUnregister, Name: name}
		_, err := shard.Apply(cmd)
		if err != nil {
			return err
		}
	}

	return nil
}

// Lookup finds a name in the appropriate shard - LOCK FREE.
func (sc *ShardCoordinator) Lookup(name string) (pid.PID, bool) {
	shardID := sc.getShardID(name)
	shard, ok := sc.getShard(shardID)
	if !ok {
		return pid.PID{}, false
	}

	return shard.Lookup(name)
}

// LookupByPID finds all names registered to a PID across all shards - LOCK FREE.
func (sc *ShardCoordinator) LookupByPID(p pid.PID) []string {
	var allNames []string
	var mu sync.Mutex // Only for appending results

	// Concurrent lookup across all shards
	var wg sync.WaitGroup
	sc.shards.Range(func(_, value any) bool {
		wg.Add(1)
		go func(shard *Shard) {
			defer wg.Done()
			names := shard.LookupByPID(p)
			if len(names) > 0 {
				mu.Lock()
				allNames = append(allNames, names...)
				mu.Unlock()
			}
		}(value.(*Shard))
		return true
	})

	wg.Wait()
	return allNames
}

// Remove removes all names for a PID across all shards.
func (sc *ShardCoordinator) Remove(_ context.Context, p pid.PID) error {
	var errs []error
	var mu sync.Mutex

	// Concurrent remove across all shards
	var wg sync.WaitGroup
	sc.shards.Range(func(_, value any) bool {
		wg.Add(1)
		go func(shard *Shard) {
			defer wg.Done()
			cmd := &syspg.Command{
				Type: syspg.CmdRemovePID,
				PID:  p,
			}
			_, err := shard.Apply(cmd)
			if err != nil {
				mu.Lock()
				errs = append(errs, err)
				mu.Unlock()
			}
		}(value.(*Shard))
		return true
	})

	wg.Wait()

	if len(errs) > 0 {
		return fmt.Errorf("remove errors: %v", errs)
	}

	return nil
}

// GetShardInfo returns information about all shards - LOCK FREE.
func (sc *ShardCoordinator) GetShardInfo() []globalregapi.ShardInfo {
	info := make([]globalregapi.ShardInfo, 0, int(sc.shardCount))

	sc.shards.Range(func(_, value any) bool {
		shard := value.(*Shard)
		info = append(info, globalregapi.ShardInfo{
			ID:        shard.ID(),
			Members:   1, // Single-node shard for now
			NameCount: shard.NameCount(),
			IsHealthy: shard.IsHealthy(),
		})
		return true
	})

	return info
}

// getShardID determines which shard owns a name using consistent hashing.
// Uses atomic reads for immutable shardCount and hashSeed.
func (sc *ShardCoordinator) getShardID(name string) globalregapi.ShardID {
	h := fnv.New32a()
	h.Write([]byte(name))
	hash := h.Sum32() ^ atomic.LoadUint32(&sc.hashSeed)
	return globalregapi.ShardID(hash % atomic.LoadUint32(&sc.shardCount))
}

// getShard returns a shard by ID - LOCK FREE using sync.Map.
func (sc *ShardCoordinator) getShard(id globalregapi.ShardID) (*Shard, bool) {
	if val, ok := sc.shards.Load(id); ok {
		return val.(*Shard), true
	}
	return nil, false
}

// groupNamesByShard groups names by their target shard.
func (sc *ShardCoordinator) groupNamesByShard(names []string) map[globalregapi.ShardID][]string {
	groups := make(map[globalregapi.ShardID][]string)
	for _, name := range names {
		shardID := sc.getShardID(name)
		groups[shardID] = append(groups[shardID], name)
	}
	return groups
}

var _ globalregapi.ShardedRegistry = (*ShardCoordinator)(nil)
