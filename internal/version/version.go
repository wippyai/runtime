package version

import (
	"fmt"

	"github.com/wippyai/runtime/api/registry"
)

// version represents a version with parent pointer
type version struct {
	id       uint
	previous *version
}

// ID returns the version ID.
func (v *version) ID() uint {
	return v.id
}

// String returns the formatted version string.
func (v *version) String() string {
	return fmt.Sprintf("v%d", v.id)
}

// Previous returns the previous version.
func (v *version) Previous() registry.Version {
	if v.previous == nil {
		return nil
	}
	return v.previous
}

// New creates a new version struct.
func New(id uint) registry.Version {
	return &version{
		id: id,
	}
}

// FromParent creates a new version struct from a parent version.
func FromParent(parent registry.Version, id uint) registry.Version {
	if parent == nil {
		return New(id)
	}

	parentVersion, ok := parent.(*version)
	if !ok {
		return New(id)
	}

	return &version{
		id:       id,
		previous: parentVersion,
	}
}
