// SPDX-License-Identifier: MPL-2.0

package raft

import "time"

// Config holds configuration for a Raft node.
//
// Raft runs with a diskless control plane (in-memory stores): cluster state
// is ephemeral, restarts rejoin from peer state, persistence-vs-quorum
// failure modes are removed by construction. There is no data_dir.
type Config struct {
	// Deprecated: mesh transport ignores this field. Kept only so legacy
	// TCP test fallbacks still compile; production runs over the mesh
	// transport which addresses peers by NodeID over the internode layer.
	AdvertiseAddr string `json:"advertise_addr,omitempty"`
	// Deprecated: mesh transport ignores this field. See AdvertiseAddr.
	BindAddr          string        `json:"bind_addr"`
	CommitTimeout     time.Duration `json:"commit_timeout"`
	SnapshotInterval  time.Duration `json:"snapshot_interval"`
	ElectionTimeout   time.Duration `json:"election_timeout"`
	HeartbeatTimeout  time.Duration `json:"heartbeat_timeout"`
	SnapshotThreshold uint64        `json:"snapshot_threshold"`
	// TrailingLogs caps how many log entries are retained after a snapshot.
	// hashicorp/raft default is 10240 which keeps a lot of memory under
	// partition; lower this for memory-constrained nodes. Zero means use
	// the hashicorp/raft library default.
	TrailingLogs uint64 `json:"trailing_logs"`
	// Deprecated: mesh transport ignores this field. See AdvertiseAddr.
	BindPort       int `json:"bind_port"`
	SnapshotRetain int `json:"snapshot_retain"`
	MaxPool        int `json:"max_pool"`
	// MaxAppendEntries caps how many log entries the leader packs into a
	// single AppendEntries RPC. The hashicorp/raft default is 64 which,
	// when a follower restarts with an empty log and needs to catch up
	// hundreds of entries, lets the leader queue large batches in
	// memory simultaneously. Setting this to 16 throttles the catch-up
	// throughput so under chaos a returning pod doesn't OOM the leader
	// while it ships history. Zero means use the library default.
	MaxAppendEntries int  `json:"max_append_entries"`
	Bootstrap        bool `json:"bootstrap"`
	// Deprecated: mesh transport ignores this field. See AdvertiseAddr.
	AutoPort bool `json:"auto_port"`
}

// InitDefaults fills zero-valued fields with sensible defaults.
func (c *Config) InitDefaults() {
	if c.BindAddr == "" {
		c.BindAddr = "0.0.0.0"
	}
	if c.BindPort == 0 {
		c.BindPort = 7960
	}
	if c.SnapshotRetain == 0 {
		c.SnapshotRetain = 3
	}
	if c.SnapshotInterval == 0 {
		c.SnapshotInterval = 2 * time.Minute
	}
	if c.SnapshotThreshold == 0 {
		c.SnapshotThreshold = 8192
	}
	if c.HeartbeatTimeout == 0 {
		// Idle clusters fan a heartbeat AppendEntries out to every follower
		// on this cadence; at 3s the leader's idle RPC volume is a third of
		// the old 1s default. Trade-off: leader-failure detection takes ~3s.
		c.HeartbeatTimeout = 3 * time.Second
	}
	if c.ElectionTimeout == 0 {
		// Must be >= HeartbeatTimeout (hashicorp/raft requirement).
		c.ElectionTimeout = 3 * time.Second
	}
	if c.CommitTimeout == 0 {
		// The leader sends an empty AppendEntries every CommitTimeout to
		// propagate the commit index even with no new log entries. 500ms
		// (vs the old 50ms) cuts that idle fan-out 10x; real entries still
		// replicate immediately via the trigger path.
		c.CommitTimeout = 500 * time.Millisecond
	}
	if c.MaxPool == 0 {
		c.MaxPool = 3
	}
	if c.MaxAppendEntries == 0 {
		// Default below hashicorp's 64 to cap leader memory during
		// chaos catch-up. Followers that need 500+ entries get them
		// in 16-entry chunks instead of 8 huge batches.
		c.MaxAppendEntries = 16
	}
	// TrailingLogs left at zero -> hashicorp/raft default (10240). Operators
	// can tune via the boot config; we don't impose a different default
	// because the tradeoff is replication performance vs replay memory.
}
