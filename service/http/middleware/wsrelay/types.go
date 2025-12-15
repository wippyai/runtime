package wsrelay

import (
	"time"

	"github.com/wippyai/runtime/api/relay"
)

// Constants for the WebSocket relay
const (
	// RelayHeader is the header that indicates a WebSocket connection request
	RelayHeader = "X-WS-Relay"

	// Topic constants
	MessageTopic   relay.Topic = "ws.message"
	JoinTopic      relay.Topic = "ws.join"
	LeaveTopic     relay.Topic = "ws.leave"
	ControlTopic   relay.Topic = "ws.control"
	CloseTopic     relay.Topic = "ws.close"
	HeartbeatTopic relay.Topic = "ws.heartbeat"

	// Default heartbeat interval
	DefaultHeartbeatInterval = 30 * time.Second
)

// RelayCommand holds the configuration for a WebSocket relay request
type RelayCommand struct {
	TargetPID         string         `json:"target_pid"`
	MessageTopic      string         `json:"message_topic,omitempty"`
	HeartbeatInterval string         `json:"heartbeat_interval,omitempty"`
	Metadata          map[string]any `json:"metadata,omitempty"` // Metadata to send in join message
}

// JoinInfo represents the information sent in a join message
type JoinInfo struct {
	ClientPID string         `json:"client_pid"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// HeartbeatInfo represents the information sent in a heartbeat message
type HeartbeatInfo struct {
	ClientPID    string         `json:"client_pid"`
	Uptime       string         `json:"uptime"`
	MessageCount int64          `json:"message_count"`
	Metadata     map[string]any `json:"metadata,omitempty"`
}
