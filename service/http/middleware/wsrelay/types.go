package wsrelay

import (
	"time"

	"github.com/wippyai/runtime/api/relay"
)

const (
	// RelayHeader is the header that indicates a WebSocket connection request
	RelayHeader = "X-WS-Relay"

	// MessageTopic is a topic constant for the WebSocket relay
	MessageTopic   relay.Topic = "ws.message"
	JoinTopic      relay.Topic = "ws.join"
	LeaveTopic     relay.Topic = "ws.leave"
	ControlTopic   relay.Topic = "ws.control"
	CloseTopic     relay.Topic = "ws.close"
	HeartbeatTopic relay.Topic = "ws.heartbeat"

	// DefaultHeartbeatInterval is the default heartbeat interval
	DefaultHeartbeatInterval = 30 * time.Second
)

// RelayCommand holds the configuration for a WebSocket relay request
type RelayCommand struct {
	Metadata          map[string]any `json:"metadata,omitempty"`
	TargetPID         string         `json:"target_pid"`
	MessageTopic      string         `json:"message_topic,omitempty"`
	HeartbeatInterval string         `json:"heartbeat_interval,omitempty"`
}

// JoinInfo represents the information sent in a join message
type JoinInfo struct {
	Metadata  map[string]any `json:"metadata,omitempty"`
	ClientPID string         `json:"client_pid"`
}

// HeartbeatInfo represents the information sent in a heartbeat message
type HeartbeatInfo struct {
	Metadata     map[string]any `json:"metadata,omitempty"`
	ClientPID    string         `json:"client_pid"`
	Uptime       string         `json:"uptime"`
	MessageCount int64          `json:"message_count"`
}
