// SPDX-License-Identifier: MPL-2.0

package globalreg

import (
	"context"
	"errors"
	"time"

	"github.com/wippyai/runtime/api/pid"
)

// ShardID identifies a shard in the sharded registry.
type ShardID uint32

// ShardedRegistry provides cluster-wide name registration with horizontal
// scalability via sharding. Each shard is an independent Raft group.
// Cross-shard operations use distributed transactions (2PC).
type ShardedRegistry interface {
	// Register associates a name with a PID globally across all shards.
	// The name is hashed to determine which shard owns it.
	// For single-shard names: uses local Raft operation.
	// For multi-shard atomic operations: uses 2PC transaction.
	Register(ctx context.Context, name string, p pid.PID) (pid.PID, error)

	// RegisterMulti atomically registers multiple names.
	// If names hash to different shards, uses 2PC for atomicity.
	RegisterMulti(ctx context.Context, names []string, p pid.PID) error

	// Unregister removes a global name registration from its shard.
	Unregister(ctx context.Context, name string) (bool, error)

	// UnregisterMulti atomically unregisters multiple names.
	UnregisterMulti(ctx context.Context, names []string) error

	// Lookup reads the name from the appropriate shard.
	Lookup(name string) (pid.PID, bool)

	// LookupByPID returns all global names registered to a PID across all shards.
	LookupByPID(p pid.PID) []string

	// Remove removes all global names for a PID across all shards.
	Remove(ctx context.Context, p pid.PID) error

	// GetShardInfo returns information about all shards.
	GetShardInfo() []ShardInfo
}

// ShardInfo provides metadata about a shard.
type ShardInfo struct {
	ID        ShardID
	Leader    string
	Members   int
	NameCount int
	IsHealthy bool
}

// TransactionStatus represents the state of a distributed transaction.
type TransactionStatus int

const (
	TxPending TransactionStatus = iota
	TxPrepared
	TxCommitted
	TxAborted
)

// Transaction represents a cross-shard operation.
type Transaction struct {
	ID        string
	Status    TransactionStatus
	Shards    []ShardID
	StartTime time.Time
}

var (
	// ErrShardNotFound indicates the target shard doesn't exist.
	ErrShardNotFound = errors.New("shard not found")

	// ErrTransactionConflict indicates a transaction conflict (2PC abort).
	ErrTransactionConflict = errors.New("transaction conflict")

	// ErrTransactionTimeout indicates a transaction timed out.
	ErrTransactionTimeout = errors.New("transaction timeout")

	// ErrInvalidShardCount indicates invalid shard configuration.
	ErrInvalidShardCount = errors.New("invalid shard count")
)
