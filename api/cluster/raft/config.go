// SPDX-License-Identifier: MPL-2.0

package raft

import "time"

// Config holds configuration for a Raft node.
//
// Raft defaults to a diskless control plane (in-memory stores): cluster state
// is ephemeral, restarts rejoin from peer state, persistence-vs-quorum failure
// modes are removed by construction. Setting DataDir opts into fs-durable
// storage, which a store.kv.raft entry requires.
//
// The transport is the wippy internode mesh exclusively: peers are addressed
// by NodeID over the existing internode connection, so there is no bind
// address, port, or advertise address to configure.
//
// Cluster formation follows the Consul/Nomad gossip-driven pattern. Every
// initial node ships the same single knob, BootstrapExpect, and joins the
// gossip mesh. A small watcher on each node observes the converged gossip
// view; when exactly BootstrapExpect raft-eligible peers (advertising the
// same BootstrapExpect and raft_status="pre") are stably visible, all of
// them deterministically derive the same sorted server list from gossip
// and call BootstrapCluster with it. No InitialCluster list is configured:
// the membership is discovered, not declared. Nodes that come up later
// see existing peers as raft_status="in" and skip bootstrap; the leader's
// reconciler adds them via AddVoter.
type Config struct {
	DataDir           string        `json:"data_dir,omitempty"`
	CommitTimeout     time.Duration `json:"commit_timeout"`
	SnapshotInterval  time.Duration `json:"snapshot_interval"`
	ElectionTimeout   time.Duration `json:"election_timeout"`
	HeartbeatTimeout  time.Duration `json:"heartbeat_timeout"`
	SnapshotThreshold uint64        `json:"snapshot_threshold"`
	TrailingLogs      uint64        `json:"trailing_logs"`
	BootstrapExpect   int           `json:"bootstrap_expect,omitempty"`
	SnapshotRetain    int           `json:"snapshot_retain"`
	MaxPool           int           `json:"max_pool"`
	MaxAppendEntries  int           `json:"max_append_entries"`
}

// InitDefaults fills zero-valued fields with sensible defaults.
func (c *Config) InitDefaults() {
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
		// on this cadence; 3s keeps idle leader RPC volume low at the cost
		// of ~3s leader-failure detection.
		c.HeartbeatTimeout = 3 * time.Second
	}
	if c.ElectionTimeout == 0 {
		c.ElectionTimeout = 3 * time.Second
	}
	// Enforce the hashicorp/raft invariant ElectionTimeout >= HeartbeatTimeout
	// rather than just defaulting each independently. An operator who raises
	// only HeartbeatTimeout (e.g. heartbeat_timeout: 5s, election unset)
	// would otherwise produce Election(3s) < Heartbeat(5s), which makes
	// NewRaft reject the config and the node fail to boot. Clamp Election up
	// so no operator timeout combination can yield a config raft refuses.
	if c.ElectionTimeout < c.HeartbeatTimeout {
		c.ElectionTimeout = c.HeartbeatTimeout
	}
	if c.CommitTimeout == 0 {
		// The leader sends an empty AppendEntries every CommitTimeout to
		// propagate the commit index even with no new log entries; 500ms
		// keeps that idle fan-out light while real entries still replicate
		// immediately via the trigger path.
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
