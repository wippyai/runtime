// SPDX-License-Identifier: MPL-2.0

package version

import (
	"fmt"

	"github.com/wippyai/runtime/api/registry"
)

// version represents a version with parent/next pointers
type version struct {
	previous *version
	next     *version
	id       uint
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

// Next returns the next version.
func (v *version) Next() registry.Version {
	if v.next == nil {
		return nil
	}
	return v.next
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

	child := &version{
		id:       id,
		previous: parentVersion,
	}
	parentVersion.next = child
	return child
}
