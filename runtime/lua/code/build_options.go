package lua

import (
	"fmt"
	"github.com/ponyruntime/pony/api/registry"
)

type AccessMode int

const (
	// AllowAll allows all IDs except those explicitly denied
	AllowAll AccessMode = iota

	// AllowListed only allows explicitly listed IDs
	AllowListed

	// DenyAll denies all IDs except those explicitly required
	DenyAll

	// StrictListed like AllowListed but denies unlisted required IDs
	StrictListed
)

// BuildOptions defines constraints and configuration for a single build operation
type BuildOptions struct {
	// Mode determines the default access behavior
	Mode AccessMode

	// Allowed defines which IDs are allowed to be used (when in AllowListed or StrictListed mode)
	Allowed map[registry.ID]bool

	// Denied defines which IDs are not allowed to be used
	// Takes precedence over Allowed and Required
	Denied map[registry.ID]bool

	// Required defines IDs that must be present in the build
	// In StrictListed mode, these must also be in Allowed
	Required map[registry.ID]bool

	// Preloaded contains dependencies that will be automatically included
	Preloaded []Dependency
}

// NewBuildOptions creates a new BuildOptions with default settings
func NewBuildOptions() *BuildOptions {
	return &BuildOptions{
		Mode:      AllowAll,
		Allowed:   make(map[registry.ID]bool),
		Denied:    make(map[registry.ID]bool),
		Required:  make(map[registry.ID]bool),
		Preloaded: make([]Dependency, 0),
	}
}

// WithMode sets the access mode
func (o *BuildOptions) WithMode(mode AccessMode) *BuildOptions {
	o.Mode = mode
	return o
}

// WithAllowed adds IDs to the allowed list
func (o *BuildOptions) WithAllowed(ids ...registry.ID) *BuildOptions {
	for _, id := range ids {
		o.Allowed[id] = true
	}
	return o
}

// WithDenied adds IDs to the denied list
func (o *BuildOptions) WithDenied(ids ...registry.ID) *BuildOptions {
	for _, id := range ids {
		o.Denied[id] = true
	}
	return o
}

// WithRequired adds IDs to the required list
func (o *BuildOptions) WithRequired(ids ...registry.ID) *BuildOptions {
	for _, id := range ids {
		o.Required[id] = true
	}
	return o
}

// WithPreloaded adds dependencies to the preloaded list
func (o *BuildOptions) WithPreloaded(deps ...Dependency) *BuildOptions {
	o.Preloaded = append(o.Preloaded, deps...)
	return o
}

// Validate checks if the given nodes comply with the build constraints
func (o *BuildOptions) Validate(nodes map[registry.ID]*Node) error {
	// In StrictListed mode, verify all required IDs are also in allowed list
	if o.Mode == StrictListed {
		for required := range o.Required {
			if !o.Allowed[required] {
				return fmt.Errorf("required ID %v must also be in allowed list (StrictListed mode)", required)
			}
		}
	}

	// Track required IDs
	foundRequired := make(map[registry.ID]bool)
	for required := range o.Required {
		foundRequired[required] = false
	}

	// Validate nodes
	for id := range nodes {
		// Check denied IDs first (highest precedence)
		if o.Denied[id] {
			return fmt.Errorf("ID %v is not allowed in this build", id)
		}

		// Mark required IDs as found
		if o.Required[id] {
			foundRequired[id] = true
			// In StrictListed mode, required IDs must still be explicitly allowed
			if o.Mode == StrictListed && !o.Allowed[id] {
				return fmt.Errorf("ID %v is required but not allowed (StrictListed mode)", id)
			}
			continue
		}

		// Apply access mode checks for IDs
		switch o.Mode {
		case AllowAll:
			// Allow anything not explicitly denied (already checked above)
		case AllowListed, StrictListed:
			if !o.Allowed[id] {
				return fmt.Errorf("ID %v is not in the allowed IDs list", id)
			}
		case DenyAll:
			return fmt.Errorf("ID %v is not allowed (DenyAll mode)", id)
		}
	}

	// Verify all required IDs were found
	for id, found := range foundRequired {
		if !found {
			return fmt.Errorf("required ID %v was not found", id)
		}
	}

	return nil
}
