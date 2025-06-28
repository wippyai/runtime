package pubsub

import (
	"strings"
	"sync"

	"github.com/ponyruntime/pony/api/registry"
)

type pidError struct {
	s string
}

func (e *pidError) Error() string {
	return e.s
}

func newError(msg string) error {
	return &pidError{s: msg}
}

var builderPool = sync.Pool{
	New: func() interface{} {
		return &strings.Builder{}
	},
}

// PID represents a Process Identifier that uniquely identifies a process in the system.
// It contains node, host, process ID, and a unique identifier components.
type PID struct {
	// Node identifies which node the process belongs to
	Node NodeID `json:"node"`
	// Host identifies which host the process belongs to
	Host HostID `json:"host"`
	// ID contains the process's registry identifier
	ID registry.ID `json:"id"`
	// UniqID contains a unique instance identifier
	UniqID string `json:"uniq_id"`
}

// String formats the PID as a pipe-delimited string wrapped in curly braces.
// Without a node it looks like: "{host|ns:name|procname}"
// With a node it looks like: "{node@host|ns:name|procname}"
func (p PID) String() string {
	b := builderPool.Get().(*strings.Builder)
	b.Reset()
	defer builderPool.Put(b)

	// Pre-allocate rough capacity to avoid reallocations
	b.Grow(len(p.Host) + len(p.UniqID) + len(p.Node) + len(string(p.ID.NS)) + len(string(p.ID.Name)) + 32)

	b.WriteByte('{')

	if p.Node != "" {
		b.WriteString(p.Node)
		b.WriteByte('@')
	}

	b.WriteString(p.Host)
	b.WriteByte('|')

	b.WriteString(string(p.ID.NS))
	b.WriteByte(':')
	b.WriteString(string(p.ID.Name))

	b.WriteByte('|')
	b.WriteString(string(p.UniqID))
	b.WriteByte('}')

	return b.String()
}

// ParsePID parses a pipe-delimited string wrapped in curly braces into a PID.
// Optimized version using manual parsing instead of strings.Split
func ParsePID(s string) (PID, error) {
	var pid PID

	// Fast path: remove braces without allocation
	if len(s) < 3 || s[0] != '{' || s[len(s)-1] != '}' {
		return pid, newError("invalid pid format: missing braces")
	}
	s = s[1 : len(s)-1] // Remove braces

	// Find first pipe
	pipe1 := strings.IndexByte(s, '|')
	if pipe1 == -1 {
		return pid, newError("invalid pid format: missing first pipe")
	}

	// Find second pipe
	pipe2 := strings.IndexByte(s[pipe1+1:], '|')
	if pipe2 == -1 {
		return pid, newError("invalid pid format: missing second pipe")
	}
	pipe2 += pipe1 + 1

	// Parse host part (may contain node@host)
	hostPart := s[:pipe1]
	if at := strings.IndexByte(hostPart, '@'); at != -1 {
		pid.Node = hostPart[:at]
		pid.Host = hostPart[at+1:]
	} else {
		pid.Host = hostPart
	}

	// Parse registry ID and unique ID
	pid.ID = registry.ParseID(s[pipe1+1 : pipe2])
	pid.UniqID = s[pipe2+1:]

	return pid, nil
}

// MarshalJSON implements the json.Marshaler interface
func (p PID) MarshalJSON() ([]byte, error) {
	b := builderPool.Get().(*strings.Builder)
	b.Reset()
	defer builderPool.Put(b)

	// Pre-allocate capacity
	b.Grow(len(p.Host) + len(p.UniqID) + len(p.Node) + len(string(p.ID.NS)) + len(string(p.ID.Name)) + 34)

	b.WriteByte('"')
	b.WriteByte('{')

	if p.Node != "" {
		b.WriteString(p.Node)
		b.WriteByte('@')
	}

	b.WriteString(p.Host)
	b.WriteByte('|')
	b.WriteString(string(p.ID.NS))
	b.WriteByte(':')
	b.WriteString(string(p.ID.Name))

	b.WriteByte('|')
	b.WriteString(p.UniqID)
	b.WriteByte('}')
	b.WriteByte('"')

	// Safe conversion to []byte
	str := b.String()
	result := make([]byte, len(str))
	copy(result, str)
	return result, nil
}

// UnmarshalJSON implements the json.Unmarshaler interface
func (p *PID) UnmarshalJSON(data []byte) error {
	// Fast path: check minimum length
	if len(data) < 4 { // minimum: "{}" wrapped in quotes
		return newError("invalid PID JSON: too short")
	}

	// Remove quotes
	if data[0] != '"' || data[len(data)-1] != '"' {
		return newError("invalid PID JSON: missing quotes")
	}

	// Convert to string (this does allocate, but safer than unsafe)
	s := string(data[1 : len(data)-1])

	parsed, err := ParsePID(s)
	if err != nil {
		return err
	}

	*p = parsed
	return nil
}
