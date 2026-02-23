// SPDX-License-Identifier: MPL-2.0

package sserelay

import (
	"time"

	"github.com/wippyai/runtime/api/relay"
)

const (
	// MiddlewareName registers this middleware in the HTTP middleware registry.
	MiddlewareName = "sse_relay"

	// RelayHeader marks HTTP responses that should be detached and relayed as SSE.
	RelayHeader = "X-SSE-Relay"

	// OptionAllowedOrigins is an option key (dot-separated, preferred).
	OptionAllowedOrigins = "sserelay.allowed.origins"

	// Shared option key (can be used across modules).
	sharedAllowOrigins = "allow_origins"

	// Legacy option key kept for compatibility with existing configs.
	legacyAllowedOrigins = "allowed_origins"

	// Relay protocol topics.
	MessageTopic   relay.Topic = "sse.message"
	JoinTopic      relay.Topic = "sse.join"
	LeaveTopic     relay.Topic = "sse.leave"
	ControlTopic   relay.Topic = "sse.control"
	CloseTopic     relay.Topic = "sse.close"
	HeartbeatTopic relay.Topic = "sse.heartbeat"

	// DefaultHeartbeatInterval keeps idle proxies and clients alive.
	DefaultHeartbeatInterval = 30 * time.Second

	// DefaultChannelCapacity caps per-session mailbox buffering.
	DefaultChannelCapacity = 32
)

// RelayCommand configures detached SSE relay behavior.
//
// target_pid is optional:
// - present  => managed mode (monitor target and auto-close on exit).
// - absent   => detached mode (emit ready and wait for later attach via control).
type RelayCommand struct {
	Metadata          map[string]any `json:"metadata,omitempty"`
	TargetPID         string         `json:"target_pid,omitempty"`
	MessageTopic      string         `json:"message_topic,omitempty"`
	HeartbeatInterval string         `json:"heartbeat_interval,omitempty"`
	IdleTimeout       string         `json:"idle_timeout,omitempty"`
	HardTimeout       string         `json:"hard_timeout,omitempty"`
}

// JoinInfo is sent to target_pid when the session attaches.
type JoinInfo struct {
	Metadata  map[string]any `json:"metadata,omitempty"`
	ClientPID string         `json:"client_pid"`
}

// ReadyInfo is sent to the client in detached mode.
type ReadyInfo struct {
	Metadata     map[string]any `json:"metadata,omitempty"`
	StreamPID    string         `json:"stream_pid"`
	MessageTopic string         `json:"message_topic"`
}

// HeartbeatInfo is sent to target_pid periodically.
type HeartbeatInfo struct {
	Metadata     map[string]any `json:"metadata,omitempty"`
	ClientPID    string         `json:"client_pid"`
	Uptime       string         `json:"uptime"`
	MessageCount int64          `json:"message_count"`
}
