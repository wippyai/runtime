package version

import (
	"fmt"
	"github.com/ponyruntime/pony/api/registry"
)

// version represents a version with major and minor components.
type version struct {
	id         uint
	previousID *uint
}

// ID returns the formatted version ID string (e.g., "v00001.001").
func (v version) ID() uint {
	return v.id
}

// String returns the formatted version string (e.g., "v00001").
func (v version) String() string {
	return fmt.Sprintf("v%d", v.id)
}

// PreviousID returns the ID of the previous version.
func (v version) Previous() registry.Version {
	if v.previousID == nil {
		return nil
	}

	return version{id: *v.previousID}
}

// New creates a new version struct.
func New(id uint) registry.Version {
	return version{id: id}
}

// FromParent creates a new version struct from a parent version.
func FromParent(parent registry.Version, id uint) registry.Version {
	if parent == nil {
		return version{id: id}
	}

	parentID := parent.ID()
	return version{id: id, previousID: &parentID}
}
