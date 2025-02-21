package pubsub

import (
	"context"
	"errors"
	"fmt"
	contextApi "github.com/ponyruntime/pony/api/context"
	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	"strings"
)

// System constants for node management
const (
	// System identifies the node management system in the event context
	System       events.System = "node"
	RegisterHost events.Kind   = "node.register_host"
	DeleteHost   events.Kind   = "node.remove_host"
	AcceptHost   events.Kind   = "node.accept_host"
	RejectHost   events.Kind   = "node.reject_host"
)

var (
	// ErrAlreadyAttached indicates that a receiver is already attached to the specified PID
	ErrAlreadyAttached = errors.New("receiver already attached")
	// ErrHostNotFound indicates that the requested host could not be found
	ErrHostNotFound = errors.New("host not found")
	// ErrHostAlreadyExists indicates that a host with the given ID is already registered
	ErrHostAlreadyExists = errors.New("host already exists")
	// ErrUpstreamNotFound indicates that the requested upstream connection is not available
	ErrUpstreamNotFound = errors.New("upstream not found")
)

type (
	NodeID = string
	HostID = string

	PID struct {
		Node   NodeID      `json:"node"`
		Host   HostID      `json:"host"`
		ID     registry.ID `json:"id"`
		UniqID string      `json:"uniq_id"`
	}

	// Topic represents a string identifier for a message channel or category
	Topic = string

	// Message represents a single message in the pub/sub system containing a topic and payload data
	Message struct {
		Topic    Topic
		Payloads payload.Payloads
	}

	// Package combines a Process ID with a batch of messages for tracking message origin
	Package struct {
		PID      PID
		Messages []*Message
	}

	// Host defines an interface for components that can receive and forward messages
	Host interface {
		Receiver
		Attach(PID, chan *Package) (context.CancelFunc, error)
		Detach(PID)
	}

	// Node represents a messaging node that can host and route messages between multiple hosts
	Node interface {
		Host
		ID() NodeID
		RegisterHost(HostID, Host) error
		UnregisterHost(HostID)
	}

	// Receiver defines the interface for components that can send messages upstream in the pub/sub system
	Receiver interface {
		Send(context.Context, *Package) error
	}
)

// NewPackage creates a new message batch with the specified topic and payload items
func NewPackage(pid PID, topic Topic, payloads ...payload.Payload) *Package {
	return &Package{
		PID:      pid,
		Messages: []*Message{{Topic: topic, Payloads: payloads}},
	}
}

// GetNode retrieves the Node instance from the provided context
func GetNode(ctx context.Context) Node {
	return ctx.Value(contextApi.NodeCtx).(Node)
}

func GetHost(ctx context.Context) Host {
	return ctx.Value(contextApi.HostCtx).(Host)
}

// String formats the PID as a pipe-delimited string wrapped in curly braces.
// Without a node it looks like: "{host|ns:name|procname}"
// With a node it looks like: "{node@host|ns:name|procname}"
func (p PID) String() string {
	var formatted string
	if p.Node == "" {
		formatted = fmt.Sprintf("%s|%s|%s", p.Host, p.ID.String(), p.UniqID)
	} else {
		formatted = fmt.Sprintf("%s@%s|%s|%s", p.Node, p.Host, p.ID.String(), p.UniqID)
	}
	return fmt.Sprintf("{%s}", formatted)
}

// ParsePID parses a pipe-delimited string wrapped in curly braces into a PID.
// It accepts the following formats:
//   - "{host|ns:name|procname}"
//   - "{node@host|ns:name|procname}"
func ParsePID(s string) (PID, error) {
	var pid PID

	// Remove wrapping curly braces, if present.
	s = strings.TrimPrefix(s, "{")
	s = strings.TrimSuffix(s, "}")

	parts := strings.Split(s, "|")
	if len(parts) != 3 {
		return pid, fmt.Errorf("invalid pid format: expected 3 parts separated by '|', got %d", len(parts))
	}

	// Parse the host part which may include a node using the "node@host" format.
	hostPart := parts[0]
	if idx := strings.Index(hostPart, "@"); idx >= 0 {
		pid.Node = hostPart[:idx]
		pid.Host = hostPart[idx+1:]
	} else {
		pid.Host = hostPart
	}

	// Parse the composite Process and process name.
	pid.ID = registry.ParseID(parts[1])
	pid.UniqID = parts[2]

	return pid, nil
}
