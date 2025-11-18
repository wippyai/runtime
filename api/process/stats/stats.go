// Package stats provides process and host statistics collection.
package stats

import (
	"time"

	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
)

// Entry contains runtime statistics for a single process instance.
type Entry struct {
	PID            relay.PID   `json:"pid"`
	SourceID       registry.ID `json:"source_id"`
	StartedAt      time.Time   `json:"started_at"`
	StepCount      int64       `json:"step_count"`
	LastActivityAt time.Time   `json:"last_activity_at"`
	Info           attrs.Bag   `json:"info,omitempty"`
}

// Snapshot contains aggregated statistics for all processes on a host.
type Snapshot struct {
	HostID     relay.HostID `json:"host_id"`
	Timestamp  time.Time    `json:"timestamp"`
	Enabled    bool         `json:"enabled"`
	SampleRate int64        `json:"sample_rate"`
	Processes  []Entry      `json:"processes"`
}
