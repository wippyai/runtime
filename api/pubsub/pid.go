package pubsub

import (
	"fmt"
	"strings"

	"github.com/ponyruntime/pony/api/registry"
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
//
// Returns the parsed PID and any error that occurred during parsing.
func ParsePID(s string) (PID, error) {
	var pid PID

	// Done wrapping curly braces, if present.
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

	// Parse the registry ID and process name.
	pid.ID = registry.ParseID(parts[1])
	pid.UniqID = parts[2]

	return pid, nil
}

// MarshalJSON implements the json.Marshaler interface
func (p PID) MarshalJSON() ([]byte, error) {
	return []byte(`"` + p.String() + `"`), nil
}

// UnmarshalJSON implements the json.Unmarshaler interface
func (p *PID) UnmarshalJSON(data []byte) error {
	// Remove quotes from the string
	s := string(data)
	if len(s) < 2 {
		return fmt.Errorf("invalid Target JSON string: %s", s)
	}
	s = s[1 : len(s)-1] // Remove quotes

	// Parse the Target from the string
	parsed, err := ParsePID(s)
	if err != nil {
		return err
	}

	// Update the receiver with parsed values
	*p = parsed
	return nil
}
