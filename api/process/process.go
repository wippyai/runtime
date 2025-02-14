package process

import (
	"context"
	"errors"
	"fmt"
	contextapi "github.com/ponyruntime/pony/api/context"
	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
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

	Receiver interface {
		send(context.Context, ...*Message) error
	}

	Process interface {
		Receiver

		Start(context.Context, PID, payload.Payloads) error

		// Step advances process state by one iteration
		Step() error
	}

	StartProcess struct {
		HostID   HostID
		ID       registry.ID
		Name     string
		Payloads payload.Payloads
	}

	Manager interface {
		Start(ctx context.Context, start *StartProcess) (PID, error)
		Send(ctx context.Context, pid PID, msg ...*Message) error
		Terminate(ctx context.Context, pid PID) error
	}

	Host interface {
		Send(ctx context.Context, pid PID, msg ...*Message) error
		Terminate(ctx context.Context, pid PID) error
	}

	LaunchProcess struct {
		PID     PID
		Process Process
		Input   payload.Payloads
	}

	Managed interface {
		Host
		Launch(ctx context.Context, launch *LaunchProcess) (PID, error)
	}

	Delegated interface {
		Host
		Launch(ctx context.Context, pid PID, input payload.Payloads) (PID, error)
	}
)

func GetProcesses(ctx context.Context) Manager {
	return ctx.Value(contextapi.ProcessesCtx).(Manager)
}

// String formats the PID as a pipe-delimited string wrapped in curly braces.
// Without a node it looks like: "{host|ns:name|procname}"
// With a node it looks like: "{node@host|ns:name|procname}"
func (p PID) String() string {
	var formatted string
	if p.Node == "" {
		formatted = fmt.Sprintf("%s|%s|%s", p.Host, p.ID.String(), p.Name)
	} else {
		formatted = fmt.Sprintf("%s@%s|%s|%s", p.Node, p.Host, p.ID.String(), p.Name)
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
		return pid, fmt.Errorf("invalid PID format: expected 3 parts separated by '|', got %d", len(parts))
	}

	// Parse the host part which may include a node using the "node@host" format.
	hostPart := parts[0]
	if idx := strings.Index(hostPart, "@"); idx >= 0 {
		pid.Node = hostPart[:idx]
		pid.Host = hostPart[idx+1:]
	} else {
		pid.Host = hostPart
	}

	// Parse the composite ID and process name.
	pid.ID = registry.ParseID(parts[1])
	pid.Name = parts[2]

	return pid, nil
}
