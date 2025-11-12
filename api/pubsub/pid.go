package pubsub

import (
	"strings"
	"sync"
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
// It contains node, host, and a unique identifier components.
type PID struct {
	// Node identifies which node the process belongs to
	Node NodeID `json:"node"`
	// Host identifies which host the process belongs to
	Host HostID `json:"host"`
	// UniqID contains a unique instance identifier
	UniqID string `json:"uniq_id"`

	// Internal cached string representation
	cachedString string
}

// String formats the PID as a pipe-delimited string wrapped in curly braces.
// Without a node it looks like: "{host|uniqid}"
// With a node it looks like: "{node@host|uniqid}"
func (p PID) String() string {
	if p.cachedString != "" {
		return p.cachedString
	}

	return p.toString()
}

func (p PID) toString() string {
	b := builderPool.Get().(*strings.Builder)
	b.Reset()
	defer builderPool.Put(b)

	// Pre-allocate rough capacity to avoid reallocations
	b.Grow(len(p.Host) + len(p.UniqID) + len(p.Node) + 8)

	b.WriteByte('{')

	if p.Node != "" {
		b.WriteString(p.Node)
		b.WriteByte('@')
	}

	b.WriteString(p.Host)
	b.WriteByte('|')
	b.WriteString(p.UniqID)
	b.WriteByte('}')

	return b.String()
}

// Precomputed returns a new PID with a cached string representation for faster lookups.
func (p PID) Precomputed() PID {
	return PID{
		Node:         p.Node,
		Host:         p.Host,
		UniqID:       p.UniqID,
		cachedString: p.toString(),
	}
}

// ParsePID parses a pipe-delimited string wrapped in curly braces into a PID.
// Format: "{host|uniqid}" or "{node@host|uniqid}"
func ParsePID(s string) (PID, error) {
	var pid PID

	// Fast path: remove braces without allocation
	if len(s) < 3 || s[0] != '{' || s[len(s)-1] != '}' {
		return pid, newError("invalid pid format: missing braces")
	}
	s = s[1 : len(s)-1] // Remove braces

	// Find pipe separator
	pipe := strings.IndexByte(s, '|')
	if pipe == -1 {
		return pid, newError("invalid pid format: missing pipe")
	}

	// Parse host part (may contain node@host)
	hostPart := s[:pipe]
	if at := strings.IndexByte(hostPart, '@'); at != -1 {
		pid.Node = hostPart[:at]
		pid.Host = hostPart[at+1:]
	} else {
		pid.Host = hostPart
	}

	// Parse unique ID (handle old 3-part format during migration)
	uniqPart := s[pipe+1:]
	// Check if there's a second pipe (old format with ID in middle)
	if secondPipe := strings.IndexByte(uniqPart, '|'); secondPipe != -1 {
		// Old format: {node@host|ns:name|uniqid} - skip the ID part
		pid.UniqID = uniqPart[secondPipe+1:]
	} else {
		// New format: {node@host|uniqid}
		pid.UniqID = uniqPart
	}

	return pid.Precomputed(), nil
}

// MarshalJSON implements the json.Marshaler interface
func (p PID) MarshalJSON() ([]byte, error) {
	b := builderPool.Get().(*strings.Builder)
	b.Reset()
	defer builderPool.Put(b)

	// Pre-allocate capacity
	b.Grow(len(p.Host) + len(p.UniqID) + len(p.Node) + 10)
	b.WriteByte('"')
	b.WriteString(p.String())
	b.WriteByte('"')

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
