package pubsub

import (
	"fmt"
	"github.com/ponyruntime/pony/api/registry"
	"strings"
)

type (
	NodeID = string
	HostID = string

	PID struct {
		Node   NodeID
		Host   HostID
		ID     registry.ID
		UniqID string
	}
)

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
