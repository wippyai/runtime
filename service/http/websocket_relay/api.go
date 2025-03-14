package websocket_relay

import (
	"net/http"
	"time"

	"github.com/ponyruntime/pony/api/pubsub"
)

// Constants for the WebSocket relay
const (
	// WSRelayHeader is the header that indicates a WebSocket connection request
	WSRelayHeader = "X-WS-Relay"

	// Topic constants
	WSMessageTopic   pubsub.Topic = "ws.message"
	WSJoinTopic      pubsub.Topic = "ws.join"
	WSLeaveTopic     pubsub.Topic = "ws.leave"
	WSControlTopic   pubsub.Topic = "ws.control"
	WSCloseTopic     pubsub.Topic = "ws.close"
	WSHeartbeatTopic pubsub.Topic = "ws.heartbeat"

	// Default heartbeat interval
	DefaultHeartbeatInterval = 30 * time.Second
)

// RelayCommand holds the configuration for a WebSocket relay request
type RelayCommand struct {
	TargetPID         string            `json:"target_pid"`
	MessageTopic      string            `json:"message_topic,omitempty"`
	HeartbeatInterval string            `json:"heartbeat_interval,omitempty"`
	Metadata          map[string]string `json:"metadata,omitempty"` // Metadata to send in join message
}

// JoinInfo represents the information sent in a join message
type JoinInfo struct {
	ClientPID string            `json:"client_pid"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

// HeartbeatInfo represents the information sent in a heartbeat message
type HeartbeatInfo struct {
	ClientPID    string            `json:"client_pid"`
	Uptime       string            `json:"uptime"`
	MessageCount int64             `json:"message_count"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

// responseWrapper wraps the ResponseWriter to capture headers
type responseWrapper struct {
	http.ResponseWriter
	headers http.Header
}

func newResponseWrapper(w http.ResponseWriter) *responseWrapper {
	return &responseWrapper{
		ResponseWriter: w,
		headers:        w.Header(),
	}
}

func (rw *responseWrapper) Header() http.Header {
	return rw.headers
}

func (rw *responseWrapper) Write(data []byte) (int, error) {
	// Capture the response body if needed
	return rw.ResponseWriter.Write(data)
}

func (rw *responseWrapper) WriteHeader(statusCode int) {
	rw.ResponseWriter.WriteHeader(statusCode)
}

func (rw *responseWrapper) Flush() {
	if flusher, ok := rw.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}
