package process

import (
	"context"
	"errors"
	"fmt"
	contextapi "github.com/ponyruntime/pony/api/context"
	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/runtime"
	"strings"
)

// Event system and kind constants for the workflow package
const (
	// PrototypeSystem identifies the workflow system in the event bus.
	PrototypeSystem events.System = "prototype"

	// RegisterPrototype is the event kind for registering a new process prototype.
	RegisterPrototype events.Kind = "prototype.register"

	// DeletePrototype is the event kind for removing an existing process prototype.
	DeletePrototype events.Kind = "prototype.remove"

	// AcceptPrototype is the event kind for accepting a new process prototype.
	AcceptPrototype events.Kind = "prototype.accept"

	// RejectPrototype is the event kind for rejecting a new process prototype.
	RejectPrototype events.Kind = "prototype.reject"

	HostSystem   events.System = "hosts"
	RegisterHost events.Kind   = "hosts.register"
	DeleteHost   events.Kind   = "hosts.remove"
	AcceptHost   events.Kind   = "hosts.accept"
	RejectHost   events.Kind   = "hosts.reject"

	TopicCancel Topic = "@cancel"
	TopicSystem Topic = "@system"
)

var (
	ErrNoProcess  = errors.New("no process running")
	ErrHostBusy   = errors.New("process host is busy")
	ErrTerminated = errors.New("process terminated")
)

type (
	NodeID = string
	HostID = string
	Topic  = string

	PID struct {
		Node NodeID
		Host HostID
		ID   registry.ID
		Name string
	}

	// Prototype is a function that creates a new process instance.
	Prototype func() (Process, error)

	// Factory manages process prototypes and handles process creation
	Factory interface {
		Create(registry.ID) (Process, error)
	}

	Message struct {
		Topic   Topic
		Payload payload.Payloads
	}

	StartProcess struct {
		PID        PID
		Input      payload.Payloads
		OnComplete []OnComplete
	}

	Process interface {
		// Start begins process execution with given task
		Start(context.Context, StartProcess) error

		// Step advances process state by one iteration
		Step() error

		// Send delivers a message to the process instance
		Send(msg *Message) error
	}

	Start struct {
		HostID   HostID
		ID       registry.ID
		Name     string
		Payloads payload.Payloads
	}

	Manager interface {
		Start(ctx context.Context, start Start) (PID, error)
		Send(ctx context.Context, pid PID, msg *Message) error
		Terminate(ctx context.Context, pid PID) error
	}

	Host interface {
		Send(ctx context.Context, pid PID, msg *Message) error
		Terminate(ctx context.Context, pid PID) error
	}

	OnComplete func(PID, *runtime.Result)

	Launch struct {
		PID        PID
		Process    Process
		Input      payload.Payloads
		OnComplete []OnComplete
	}

	Managed interface {
		Host
		Launch(ctx context.Context, launch Launch) (PID, error)
	}

	Delegated interface {
		Host
		Launch(ctx context.Context, pid PID, input payload.Payloads) (PID, error)
	}
)

func GetProcesses(ctx context.Context) Manager {
	return ctx.Value(contextapi.ProcessesCtx).(Manager)
}

// String formats PID as a string in the format: "[node@]host:id:name"
// If node is empty, the node@ part is omitted
func (p PID) String() string {
	if p.Node == "" {
		return fmt.Sprintf("%s:%s:%s", p.Host, p.ID.String(), p.Name)
	}
	return fmt.Sprintf("%s@%s:%s:%s", p.Node, p.Host, p.ID.String(), p.Name)
}

// ParsePID parses a string representation back into a PID
// Accepts formats:
// - "node@host:ns:name:procname"
// - "host:ns:name:procname"
func ParsePID(s string) (PID, error) {
	var node, host string
	var rest string

	// Check if we have a node part (contains @)
	if idx := strings.Index(s, "@"); idx >= 0 {
		node = s[:idx]
		rest = s[idx+1:]
	} else {
		rest = s
	}

	// Split the remaining parts
	parts := strings.SplitN(rest, ":", 4)
	if len(parts) != 3 {
		return PID{}, fmt.Errorf("invalid PID format: expected 3 or 4 parts, got %d", len(parts))
	}

	host = parts[0]
	id := registry.ParseID(fmt.Sprintf("%s:%s", parts[1], parts[2]))

	return PID{
		Node: node,
		Host: host,
		ID:   id,
		Name: parts[3],
	}, nil
}
