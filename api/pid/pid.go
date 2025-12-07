// Package pid provides process identifier types used across the relay system.
package pid

import (
	"strings"
	"sync"
)

type (
	// NodeID uniquely identifies a node in the relay network.
	NodeID = string

	// HostID uniquely identifies a host within a node.
	HostID = string
)

var builderPool = sync.Pool{
	New: func() interface{} {
		return &strings.Builder{}
	},
}

// PID represents a Process Identifier that uniquely identifies a process.
type PID struct {
	Node         NodeID `json:"node"`
	Host         HostID `json:"host"`
	UniqID       string `json:"uniq_id"`
	cachedString string
}

// String formats the PID as a pipe-delimited string wrapped in curly braces.
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

// Precomputed returns a new PID with a cached string representation.
func (p PID) Precomputed() PID {
	return PID{
		Node:         p.Node,
		Host:         p.Host,
		UniqID:       p.UniqID,
		cachedString: p.toString(),
	}
}

// ParsePID parses a pipe-delimited string into a PID.
// Format: "{host|uniqid}" or "{node@host|uniqid}"
func ParsePID(s string) (PID, error) {
	var pid PID

	if len(s) < 3 || s[0] != '{' || s[len(s)-1] != '}' {
		return pid, ErrInvalidPIDFormat.WithMessage("invalid pid format: missing braces")
	}
	s = s[1 : len(s)-1]

	pipe := strings.IndexByte(s, '|')
	if pipe == -1 {
		return pid, ErrInvalidPIDFormat.WithMessage("invalid pid format: missing pipe")
	}

	hostPart := s[:pipe]
	if at := strings.IndexByte(hostPart, '@'); at != -1 {
		pid.Node = hostPart[:at]
		pid.Host = hostPart[at+1:]
	} else {
		pid.Host = hostPart
	}

	uniqPart := s[pipe+1:]
	if secondPipe := strings.IndexByte(uniqPart, '|'); secondPipe != -1 {
		pid.UniqID = uniqPart[secondPipe+1:]
	} else {
		pid.UniqID = uniqPart
	}

	return pid.Precomputed(), nil
}

// MarshalJSON implements the json.Marshaler interface.
func (p PID) MarshalJSON() ([]byte, error) {
	b := builderPool.Get().(*strings.Builder)
	b.Reset()
	defer builderPool.Put(b)

	b.Grow(len(p.Host) + len(p.UniqID) + len(p.Node) + 10)
	b.WriteByte('"')
	b.WriteString(p.String())
	b.WriteByte('"')

	str := b.String()
	result := make([]byte, len(str))
	copy(result, str)

	return result, nil
}

// UnmarshalJSON implements the json.Unmarshaler interface.
func (p *PID) UnmarshalJSON(data []byte) error {
	if len(data) < 4 {
		return ErrInvalidPIDFormat.WithMessage("invalid PID JSON: too short")
	}

	if data[0] != '"' || data[len(data)-1] != '"' {
		return ErrInvalidPIDFormat.WithMessage("invalid PID JSON: missing quotes")
	}

	s := string(data[1 : len(data)-1])
	parsed, err := ParsePID(s)
	if err != nil {
		return err
	}

	*p = parsed
	return nil
}
