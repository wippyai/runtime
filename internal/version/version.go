package version

import (
	"fmt"
	"github.com/ponyruntime/pony/api/registry"
)

// Version represents a version with major and minor components.
type Version struct {
	id         uint
	previousID uint
}

// ID returns the formatted version ID string (e.g., "v00001.001").
func (v Version) ID() uint {
	return v.id
}

// String returns the formatted version string (e.g., "v00001").
func (v Version) String() string {
	return fmt.Sprintf("v%d", v.id)
}

// PreviousID returns the ID of the previous version.
func (v Version) PreviousID() uint {
	return v.previousID
}

// New creates a new Version struct.
func New(id uint) Version {
	return Version{id: id}
}

// FromParent creates a new Version struct from a parent version.
func FromParent(parent registry.Version, id uint) Version {
	return Version{id: id, previousID: parent.ID()}
}
