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
	NS Namespace `json:"ns" msgpack:"ns"`
	// Name is the unique identifier within the namespace
	Name Name `json:"name" msgpack:"name"`
	// str is the cached string representation (interned with the ID)
	str string
}

// NewID creates a new interned ID with the given namespace and name.
// Use this instead of direct ID{} construction to ensure string deduplication.
func NewID(ns, name string) ID {
	id := ID{
		NS:   ns,
		Name: name,
		str:  ns + ":" + name,
	}
	return unique.Make(id).Value()
}

func (t *ID) String() string {
	if t.str != "" {
		return t.str
	}
	return t.NS + ":" + t.Name
}

// Equal returns true if both IDs have the same namespace and name.
func (t *ID) Equal(other ID) bool {
	return t.NS == other.NS && t.Name == other.Name
}

// MarshalJSON implements the json.Marshaler interface.
func (t *ID) MarshalJSON() ([]byte, error) {
	if t.NS == "" {
		return json.Marshal(t.Name)
	}
	return json.Marshal(t.NS + ":" + t.Name)
}

// WithDefaultNS returns a new ID with the given default namespace if one is not already set.
// If the ID already has a namespace, it returns the ID unchanged.
func (t *ID) WithDefaultNS(defaultNS Namespace) ID {
	if t.NS != "" {
		return *t
	}
	id := ID{
		NS:   defaultNS,
		Name: t.Name,
		str:  defaultNS + ":" + t.Name,
	}
	return unique.Make(id).Value()
}

// parseIDString parses a string in "namespace:name" or "name-only" format.
func parseIDString(s string) ID {
	if idx := strings.IndexByte(s, ':'); idx != -1 {
		return ID{NS: s[:idx], Name: s[idx+1:], str: s}
	}
	return ID{NS: "", Name: s, str: ":" + s}
}

// ParseID parses a string in "namespace:name" or "name-only" format into an ID.
func ParseID(s string) ID {
	return unique.Make(parseIDString(s)).Value()
}

// UnmarshalJSON deserializes an ID from JSON, supporting both string and object formats.
func (t *ID) UnmarshalJSON(data []byte) error {
	var id ID

	if len(data) > 0 && data[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		id = parseIDString(s)
	} else {
		var obj struct {
			NS   Namespace `json:"ns"`
			Name Name      `json:"name"`
		}
		if err := json.Unmarshal(data, &obj); err != nil {
			return err
		}
		id = ID{NS: obj.NS, Name: obj.Name, str: obj.NS + ":" + obj.Name}
	}

	*t = unique.Make(id).Value()
	return nil
}
