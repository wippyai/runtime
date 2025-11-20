// Package registry provides service registry and entry management.
package registry

import (
	"encoding/json"
	"strings"
	"unique"
)

// ID uniquely identifies a registry entry by namespace and name
type ID struct {
	// NS is the namespace of the entry
	NS Namespace `json:"ns"`
	// Name is the unique identifier within the namespace
	Name Name `json:"name"`
}

// NewID creates a new interned ID with the given namespace and name.
// Use this instead of direct ID{} construction to ensure string deduplication.
func NewID(ns, name string) ID {
	id := ID{
		NS:   ns,
		Name: name,
	}
	return unique.Make(id).Value()
}

func (t ID) String() string {
	return t.NS + ":" + t.Name
}

// MarshalJSON implements the json.Marshaler interface.
// Fixed to use simple concatenation instead of pool
func (t ID) MarshalJSON() ([]byte, error) {
	if t.NS == "" {
		return json.Marshal(t.Name)
	}

	// Simple string concatenation - much faster than pool
	str := t.NS + ":" + t.Name
	return json.Marshal(str)
}

// WithDefaultNS returns a new ID with the given default namespace if one is not already set.
// If the ID already has a namespace, it returns the ID unchanged.
func (t ID) WithDefaultNS(defaultNS Namespace) ID {
	if t.NS != "" {
		return t
	}
	id := ID{
		NS:   defaultNS,
		Name: t.Name,
	}
	return unique.Make(id).Value()
}

// ParseID parses a string in "namespace:name" or "name-only" format into an ID.
func ParseID(s string) ID {
	var id ID

	// Fast path: find first colon using IndexByte (faster than strings.SplitN)
	if idx := strings.IndexByte(s, ':'); idx != -1 {
		// Has colon - parse as ns:name
		id = ID{
			NS:   s[:idx],
			Name: s[idx+1:],
		}
	} else {
		// Name-only format
		id = ID{
			NS:   "",
			Name: s,
		}
	}

	// Intern the entire ID struct for deduplication
	return unique.Make(id).Value()
}

// UnmarshalJSON deserializes an ID from JSON, supporting both string and object formats.
func (t *ID) UnmarshalJSON(data []byte) error {
	var id ID

	// Check if the data is a JSON string
	if len(data) > 0 && data[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}

		// Fast path: find first colon using IndexByte
		if idx := strings.IndexByte(s, ':'); idx != -1 {
			// Has colon - parse as ns:name
			id = ID{
				NS:   s[:idx],
				Name: s[idx+1:],
			}
		} else {
			// Name-only format
			id = ID{
				NS:   "",
				Name: s,
			}
		}
	} else {
		// Handle object format
		var obj struct {
			NS   Namespace `json:"ns"`
			Name Name      `json:"name"`
		}
		if err := json.Unmarshal(data, &obj); err != nil {
			return err
		}
		id = ID{
			NS:   obj.NS,
			Name: obj.Name,
		}
	}

	// Intern the entire ID struct for deduplication
	*t = unique.Make(id).Value()
	return nil
}
