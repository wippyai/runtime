package registry

import (
	"encoding/json"
	"fmt"
	"strings"
)

func (t ID) String() string {
	return fmt.Sprintf("%s:%s", t.NS, t.Name)
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
	// Split on first colon
	parts := strings.SplitN(s, ":", 2)
	if len(parts) == 1 {
		// Alias-only format
		return ID{
			NS:   "",
			Name: Name(parts[0]),
		}
	}
	// Has colon - parse as ns:name
	return ID{
		NS:   Namespace(parts[0]),
		Name: Name(parts[1]),
	}
}

func (t *ID) UnmarshalJSON(data []byte) error {
	// Check if the data is a JSON string
	if len(data) > 0 && data[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}

		// Handle string format - split on first colon
		parts := strings.SplitN(s, ":", 2)
		if len(parts) == 1 {
			// Alias-only format
			t.NS = ""
			t.Name = Name(parts[0])
			return nil
		}
		// Has colon - parse as ns:name
		t.NS = Namespace(parts[0])
		t.Name = Name(parts[1])
		return nil
	}

	// Handle object format
	var obj struct {
		NS Namespace `json:"ns"`
		ID Name      `json:"id"`
	}
	if err := json.Unmarshal(data, &obj); err != nil {
		return err
	}
	t.NS = obj.NS
	t.Name = obj.ID
	return nil
}
