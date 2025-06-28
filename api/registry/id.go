package registry

import (
	"encoding/json"
	"strings"
)

// ID uniquely identifies a registry entry by namespace and name
type ID struct {
	// NS is the namespace of the entry
	NS Namespace `json:"ns"`
	// Name is the unique identifier within the namespace
	Name Name `json:"name"`
}

func (t ID) String() string {
	return string(t.NS) + ":" + string(t.Name)
}

// MarshalJSON implements the json.Marshaler interface.
// Fixed to use simple concatenation instead of pool
func (t ID) MarshalJSON() ([]byte, error) {
	if t.NS == "" {
		return json.Marshal(string(t.Name))
	}

	// Simple string concatenation - much faster than pool
	str := string(t.NS) + ":" + string(t.Name)
	return json.Marshal(str)
}

// WithDefaultNS returns a new ID with the given default namespace if one is not already set.
// If the ID already has a namespace, it returns the ID unchanged.
func (t ID) WithDefaultNS(defaultNS Namespace) ID {
	if t.NS != "" {
		return t
	}
	return ID{
		NS:   defaultNS,
		Name: t.Name,
	}
}

// ParseID creates an ID from a string in either "namespace:name" or "name-only" format.
// For "namespace:name" format, the first colon is used as the separator.
// For "name-only" format, an empty namespace is used.
func ParseID(s string) ID {
	// Fast path: find first colon using IndexByte (faster than strings.SplitN)
	if idx := strings.IndexByte(s, ':'); idx != -1 {
		// Has colon - parse as ns:name
		return ID{
			NS:   Namespace(s[:idx]),
			Name: Name(s[idx+1:]),
		}
	}

	// Name-only format
	return ID{
		NS:   "",
		Name: Name(s),
	}
}

func (t *ID) UnmarshalJSON(data []byte) error {
	// Check if the data is a JSON string
	if len(data) > 0 && data[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}

		// Fast path: find first colon using IndexByte
		if idx := strings.IndexByte(s, ':'); idx != -1 {
			// Has colon - parse as ns:name
			t.NS = Namespace(s[:idx])
			t.Name = Name(s[idx+1:])
			return nil
		}

		// Name-only format
		t.NS = ""
		t.Name = Name(s)
		return nil
	}

	// Handle object format
	var obj struct {
		NS   Namespace `json:"ns"`
		Name Name      `json:"name"`
	}
	if err := json.Unmarshal(data, &obj); err != nil {
		return err
	}
	t.NS = obj.NS
	t.Name = obj.Name
	return nil
}
