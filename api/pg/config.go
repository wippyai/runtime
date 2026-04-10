// SPDX-License-Identifier: MPL-2.0

package pg

import (
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/supervisor"
)

// Registry kind constant for process group scopes.
const (
	// Scope identifies a process group scope in the registry.
	// Each scope is an independent PG instance with its own state,
	// event loop, and cluster mesh — following Erlang/OTP pg scope semantics.
	Scope registry.Kind = "pg.scope"
)

// Config defines configuration for a process group scope.
type Config struct {
	// ID is the registry entry ID, set automatically by DecodeEntryConfig.
	ID registry.ID `json:"id"`

	// Lifecycle configures supervisor lifecycle management.
	Lifecycle supervisor.LifecycleConfig `json:"lifecycle"`

	// ActionQueueSize is the capacity of the internal event loop action channel.
	// Higher values allow more operations to be buffered before blocking.
	// Default: 256.
	ActionQueueSize int `json:"action_queue_size"`

	// MonitorBuffer is the capacity of the per-monitor delivery channel.
	// Each monitor subscription gets a buffered channel of this size for
	// receiving membership events. If the buffer fills, events are dropped
	// for that subscriber (back-pressure). Default: 64.
	MonitorBuffer int `json:"monitor_buffer"`

	// MaxGroups limits the total number of distinct groups a scope can track.
	// Zero means unlimited (no cap). Attempts to join a new group when at
	// the limit return an error. Default: 0 (unlimited).
	MaxGroups int `json:"max_groups"`

	// MaxMembersPerGroup limits how many member slots a single group can hold.
	// Zero means unlimited (no cap). Because a PID may join the same group
	// multiple times, this counts total join slots, not unique PIDs.
	// Default: 0 (unlimited).
	MaxMembersPerGroup int `json:"max_members_per_group"`
}

// InitDefaults initializes the configuration with sensible defaults.
// Called by DecodeEntryConfig after unmarshaling.
func (c *Config) InitDefaults() {
	if c.ActionQueueSize == 0 {
		c.ActionQueueSize = 256
	}

	if c.MonitorBuffer == 0 {
		c.MonitorBuffer = 64
	}

	c.Lifecycle.InitDefaults()
}

// Validate checks if the configuration is valid.
// Called by DecodeEntryConfig after InitDefaults.
func (c *Config) Validate() error {
	if c.ActionQueueSize < 0 {
		return ErrInvalidActionQueueSize
	}

	if c.MonitorBuffer < 0 {
		return ErrInvalidMonitorBuffer
	}

	if c.MaxGroups < 0 {
		return ErrInvalidMaxGroups
	}

	if c.MaxMembersPerGroup < 0 {
		return ErrInvalidMaxMembersPerGroup
	}

	return nil
}
