// SPDX-License-Identifier: MPL-2.0

package sharded

import (
	"fmt"
	"sync/atomic"

	hraft "github.com/hashicorp/raft"
	globalregapi "github.com/wippyai/runtime/api/globalreg"
	"github.com/wippyai/runtime/api/pid"
	syspg "github.com/wippyai/runtime/system/globalreg"
	"go.uber.org/zap"
)

// Shard represents a single shard in the sharded registry.
// Optimized for lock-free reads - writes are serialized through Apply.
type Shard struct {
	fsm       *syspg.FSM
	logger    *zap.Logger
	nameCount atomic.Int64
	id        globalregapi.ShardID
}

// NewShard creates a new shard with the given ID and FSM.
func NewShard(id globalregapi.ShardID, fsm *syspg.FSM, logger *zap.Logger) *Shard {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Shard{
		id:     id,
		fsm:    fsm,
		logger: logger.Named(fmt.Sprintf("shard-%d", id)),
	}
}

// Apply applies a command to this shard's FSM.
// Writes are serialized through the FSM's Apply method.
func (s *Shard) Apply(cmd *syspg.Command) (any, error) {
	nameExisted := false
	if cmd.Type == syspg.CmdRegister {
		_, nameExisted = s.Lookup(cmd.Name)
	}

	// Encode command and wrap in Raft log for FSM
	data, err := syspg.EncodeCommand(cmd)
	if err != nil {
		return nil, err
	}

	log := &hraft.Log{Data: data, Index: 1}
	result := s.fsm.Apply(log)

	// Update atomic counter on successful register
	switch cmd.Type {
	case syspg.CmdRegister:
		if regResult, ok := result.(*syspg.RegisterResult); ok && regResult.Err == nil && !nameExisted {
			s.nameCount.Add(1)
		}
	case syspg.CmdUnregister:
		if unregResult, ok := result.(*syspg.UnregisterResult); ok && unregResult.Removed {
			s.nameCount.Add(-1)
		}
	}

	return result, nil
}

// Lookup finds a name in this shard - LOCK FREE.
// The underlying FSM state uses RCU pattern for lock-free reads.
func (s *Shard) Lookup(name string) (pid.PID, bool) {
	// Direct access to FSM state - no locks needed for reads
	// The shardedState internally uses RCU for safe concurrent access
	return s.fsm.State().Lookup(name)
}

// LookupByPID finds all names for a PID in this shard - LOCK FREE.
func (s *Shard) LookupByPID(p pid.PID) []string {
	return s.fsm.State().LookupByPID(p)
}

// NameCount returns the number of names in this shard - LOCK FREE (atomic).
func (s *Shard) NameCount() int {
	return int(s.nameCount.Load())
}

// IsHealthy reports whether this shard is healthy - LOCK FREE.
func (s *Shard) IsHealthy() bool {
	// In real implementation with Raft, check atomic health flag
	return true
}

// ID returns the shard's ID.
func (s *Shard) ID() globalregapi.ShardID {
	return s.id
}
