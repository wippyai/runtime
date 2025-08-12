package version

import (
	"fmt"

	"github.com/ponyruntime/pony/api/registry"
)

// version represents a version with simple double-linked list structure
type version struct {
	id       uint
	previous *version
	next     *version
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

// Next returns the next version if available.
func (v *version) Next() (registry.Version, bool) {
	if v.next == nil {
		return nil, false
	}
	return v.next, true
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
		// Fallback: create new isolated version
		return New(id)
	}

	child := &version{
		id:       id,
		previous: parentVersion,
	}

	// Link the parent to this child as its next
	parentVersion.next = child
	return child
}
