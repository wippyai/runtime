package code

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
	Allowed []registry.ID

	// Denied defines which IDs are not allowed to be used
	// Takes precedence over Allowed and Required
	Denied []registry.ID

	// Required defines IDs that must be present in the build
	// In StrictListed mode, these must also be in Allowed
	Required []registry.ID

	// Preloaded contains dependencies that will be automatically included
	Preloaded []Preload
}

// NewBuildOptions creates a new BuildOptions with default settings
func NewBuildOptions() *BuildOptions {
	return &BuildOptions{
		Mode:      AllowAll,
		Allowed:   make([]registry.ID, 0),
		Denied:    make([]registry.ID, 0),
		Required:  make([]registry.ID, 0),
		Preloaded: make([]Preload, 0),
	}
}

// WithMode sets the access mode
func (o *BuildOptions) WithMode(mode AccessMode) *BuildOptions {
	o.Mode = mode
	return o
}

// WithAllowed adds IDs to the allowed list
func (o *BuildOptions) WithAllowed(ids ...registry.ID) *BuildOptions {
	o.Allowed = append(o.Allowed, ids...)
	return o
}

// WithDenied adds IDs to the denied list
func (o *BuildOptions) WithDenied(ids ...registry.ID) *BuildOptions {
	o.Denied = append(o.Denied, ids...)
	return o
}

// WithRequired adds IDs to the required list
func (o *BuildOptions) WithRequired(ids ...registry.ID) *BuildOptions {
	o.Required = append(o.Required, ids...)
	return o
}

// WithPreloaded adds dependencies to the preloaded list
func (o *BuildOptions) WithPreloaded(deps ...Preload) *BuildOptions {
	o.Preloaded = append(o.Preloaded, deps...)
	return o
}

// contains is a helper function to check if a slice contains an Process
func contains(slice []registry.ID, item registry.ID) bool {
	for _, id := range slice {
		if id == item {
			return true
		}
	}
	return false
}

// Validate checks if the given nodes comply with the build constraints
func (o *BuildOptions) Validate(nodes map[registry.ID]*Node) error {
	// In StrictListed mode, verify all required IDs are also in allowed list
	if o.Mode == StrictListed {
		for _, required := range o.Required {
			if !contains(o.Allowed, required) {
				return fmt.Errorf("required Process `%v` must also be in allowed list (StrictListed mode)", required)
			}
		}
	}

	// Track required IDs
	foundRequired := make(map[registry.ID]bool)
	for _, required := range o.Required {
		foundRequired[required] = false
	}

	// Validate nodes
	for id := range nodes {
		// Check denied IDs first (highest precedence)
		if contains(o.Denied, id) {
			return fmt.Errorf("process `%v` is not allowed in this build", id)
		}

		// Mark required IDs as found
		if contains(o.Required, id) {
			foundRequired[id] = true
			// In StrictListed mode, required IDs must still be explicitly allowed
			if o.Mode == StrictListed && !contains(o.Allowed, id) {
				return fmt.Errorf("process `%v` is required but not allowed (StrictListed mode)", id)
			}
			continue
		}

		// Apply access mode checks for IDs
		switch o.Mode {
		case AllowAll:
			// Allow anything not explicitly denied (already checked above)
		case AllowListed, StrictListed:
			if !contains(o.Allowed, id) {
				return fmt.Errorf("process `%v` is not in the allowed IDs list", id)
			}
		case DenyAll:
			return fmt.Errorf("process `%v` is not allowed (DenyAll mode)", id)
		}
	}

	// Verify all required IDs were found
	for id, found := range foundRequired {
		if !found {
			return fmt.Errorf("required Process `%v` was not found", id)
		}
	}

	return nil
}
