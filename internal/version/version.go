package version

import (
	"fmt"
	"github.com/ponyruntime/pony/api/registry"
)

// Version represents a version with major and minor components.
type Version struct {
	id         string
	major      uint
	minor      uint
	previousID string
}

// Config
const (
	majorFormatWidth = 5 // Number of digits for major version (e.g., v00001)
	minorFormatWidth = 3 // Number of digits for minor version (e.g., .001)
)

// ID returns the formatted version ID string (e.g., "v00001.001").
func (v Version) ID() string {
	return v.id
}

// Major returns the major version number.
func (v Version) Major() uint {
	return v.major
}

// Minor returns the minor version number.
func (v Version) Minor() uint {
	return v.minor
}

// String returns the formatted version string (e.g., "v00001.001").
func (v Version) String() string {
	return v.id
}

// PreviousID returns the ID of the previous version.
func (v Version) PreviousID() string {
	return v.previousID
}

// New creates a new Version struct.
func New(major, minor uint) Version {
	id := generateID(major, minor)
	return Version{major: major, minor: minor, id: id}
}

// FromParent creates a new Version struct from a parent version.
func FromParent(parent registry.Version, major, minor uint) Version {
	id := generateID(major, minor)
	return Version{major: major, minor: minor, id: id, previousID: parent.ID()}
}

// generateID generates a formatted version ID string.
func generateID(major, minor uint) string {
	return fmt.Sprintf("v%0*d.%0*d", majorFormatWidth, major, minorFormatWidth, minor)
}
