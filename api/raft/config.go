// SPDX-License-Identifier: MPL-2.0

package raft

import "time"

// Config holds configuration for a Raft node.
type Config struct {
	DataDir           string        `json:"data_dir"`
	AdvertiseAddr     string        `json:"advertise_addr"`
	BindAddr          string        `json:"bind_addr"`
	CommitTimeout     time.Duration `json:"commit_timeout"`
	SnapshotInterval  time.Duration `json:"snapshot_interval"`
	ElectionTimeout   time.Duration `json:"election_timeout"`
	HeartbeatTimeout  time.Duration `json:"heartbeat_timeout"`
	SnapshotThreshold uint64        `json:"snapshot_threshold"`
	BindPort          int           `json:"bind_port"`
	SnapshotRetain    int           `json:"snapshot_retain"`
	MaxPool           int           `json:"max_pool"`
	Bootstrap         bool          `json:"bootstrap"`
	AutoPort          bool          `json:"auto_port"`
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
		c.HeartbeatTimeout = 1 * time.Second
	}
	if c.ElectionTimeout == 0 {
		c.ElectionTimeout = 1 * time.Second
	}
	if c.CommitTimeout == 0 {
		c.CommitTimeout = 50 * time.Millisecond
	}
	if c.MaxPool == 0 {
		c.MaxPool = 3
	}
}
