// SPDX-License-Identifier: MPL-2.0

package raft

import (
	hraft "github.com/hashicorp/raft"

	raftapi "github.com/wippyai/runtime/api/cluster/raft"
)

// toHashicorpConfig converts our config into a hashicorp/raft Config.
func toHashicorpConfig(localID string, cfg raftapi.Config) *hraft.Config {
	rc := hraft.DefaultConfig()
	rc.LocalID = hraft.ServerID(localID)
	rc.HeartbeatTimeout = cfg.HeartbeatTimeout
	rc.ElectionTimeout = cfg.ElectionTimeout
	rc.CommitTimeout = cfg.CommitTimeout
	// Couple the leader lease to the heartbeat timeout. hashicorp's default
	// lease is 500ms regardless of HeartbeatTimeout; with our 3s heartbeat
	// that asymmetry makes the leader self-demote after only 500ms of not
	// reaching a quorum while followers don't challenge until 3s — so a
	// sub-second stall (GC pause, CPU pressure, a network microburst) trips
	// a needless failover and bumps the term. Setting the lease equal to
	// the heartbeat (the maximum hraft permits: lease <= heartbeat) gives
	// the leader the same tolerance window followers use before electing.
	// Safety is unaffected: a leader without quorum cannot commit, and the
	// new term's leader needs the same quorum, so two leaders can never
	// both commit.
	rc.LeaderLeaseTimeout = cfg.HeartbeatTimeout
	rc.SnapshotInterval = cfg.SnapshotInterval
	rc.SnapshotThreshold = cfg.SnapshotThreshold
	if cfg.TrailingLogs > 0 {
		rc.TrailingLogs = cfg.TrailingLogs
	}
	if cfg.MaxAppendEntries > 0 {
		rc.MaxAppendEntries = cfg.MaxAppendEntries
	}
	// Suppress raft internal logging by using a discard logger.
	// Leadership changes are published via our event bus instead.
	rc.LogLevel = "WARN"
	return rc
}
