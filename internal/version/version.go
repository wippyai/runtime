package version

import (
	"fmt"

	"github.com/ponyruntime/pony/api/registry"
)

// version represents a version with major and minor components.
type version struct {
	id         string
	previousID *string
}

// ID returns the formatted version ID string (e.g., "v00001.001").
func (v version) ID() string {
	return v.id
}

// String returns the formatted version string (e.g., "v00001").
func (v version) String() string {
	if len(v.id) > 8 {
		return fmt.Sprintf("v%s", v.id[len(v.id)-8:])
	}
	return fmt.Sprintf("v%s", v.id)
}

// Previous returns the ID of the previous version.
func (v version) Previous() registry.Version {
	if v.previousID == nil {
		return nil
	}

	return version{id: *v.previousID}
}

// New creates a new version struct.
func New(id string) registry.Version {
	return version{id: id}
}

// FromParent creates a new version struct from a parent version.
func FromParent(parent registry.Version, id string) registry.Version {
	if parent == nil {
		return version{id: id}
	}

	parentID := parent.ID()
	return version{id: id, previousID: &parentID}
}
