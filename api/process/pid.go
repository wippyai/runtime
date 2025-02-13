package process

import (
	"fmt"
	"github.com/ponyruntime/pony/api/registry"
	"strings"
)

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
