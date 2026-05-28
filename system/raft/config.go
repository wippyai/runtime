// SPDX-License-Identifier: MPL-2.0

package raft

import (
	hraft "github.com/hashicorp/raft"

	raftapi "github.com/wippyai/runtime/api/raft"
)

// toHashicorpConfig converts our config into a hashicorp/raft Config.
func toHashicorpConfig(localID string, cfg raftapi.Config) *hraft.Config {
	rc := hraft.DefaultConfig()
	rc.LocalID = hraft.ServerID(localID)
	rc.HeartbeatTimeout = cfg.HeartbeatTimeout
	rc.ElectionTimeout = cfg.ElectionTimeout
	rc.CommitTimeout = cfg.CommitTimeout
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
