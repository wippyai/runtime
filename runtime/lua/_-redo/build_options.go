package __redo

import (
	"fmt"
	runtime "github.com/ponyruntime/pony/api/runtime/lua"
)

type ModuleAccessMode int

const (
	// AllowAll allows all modules except those explicitly denied
	AllowAll ModuleAccessMode = iota

	// AllowListed only allows explicitly listed modules
	AllowListed

	// DenyAll denies all modules except those explicitly required
	DenyAll

	// StrictListed like AllowListed but denies unlisted required modules
	StrictListed
)

// BuildOptions defines constraints and configuration for a single build operation
type BuildOptions struct {
	// AccessMode determines the default module access behavior
	AccessMode ModuleAccessMode

	// AllowedModules defines which modules are allowed to be used (when in AllowListed or StrictListed mode)
	AllowedModules map[string]bool

	// DeniedModules defines which modules are not allowed to be used
	// Takes precedence over AllowedModules and RequiredModules
	DeniedModules map[string]bool

	// RequiredModules defines modules that must be present in the build
	// In StrictListed mode, these must also be in AllowedModules
	RequiredModules map[string]bool
}

// NewBuildOptions creates a new BuildOptions with default settings
func NewBuildOptions() *BuildOptions {
	return &BuildOptions{
		AccessMode:      AllowAll,
		AllowedModules:  make(map[string]bool),
		DeniedModules:   make(map[string]bool),
		RequiredModules: make(map[string]bool),
	}
}

// WithAccessMode sets the module access mode
func (o *BuildOptions) WithAccessMode(mode ModuleAccessMode) *BuildOptions {
	o.AccessMode = mode
	return o
}

// WithAllowedModules adds modules to the allowed list
func (o *BuildOptions) WithAllowedModules(modules ...string) *BuildOptions {
	for _, m := range modules {
		o.AllowedModules[m] = true
	}
	return o
}

// WithDeniedModules adds modules to the denied list
func (o *BuildOptions) WithDeniedModules(modules ...string) *BuildOptions {
	for _, m := range modules {
		o.DeniedModules[m] = true
	}
	return o
}

// WithRequiredModules adds modules to the required list
func (o *BuildOptions) WithRequiredModules(modules ...string) *BuildOptions {
	for _, m := range modules {
		o.RequiredModules[m] = true
	}
	return o
}

// validateModules checks if the given modules comply with the build constraints
func (o *BuildOptions) validateModules(modules []runtime.Module) error {
	// In StrictListed mode, verify all required modules are also in allowed list
	if o.AccessMode == StrictListed {
		for required := range o.RequiredModules {
			if !o.AllowedModules[required] {
				return fmt.Errorf("required module %s must also be in allowed list (StrictListed mode)", required)
			}
		}
	}

	// Track required modules
	foundRequired := make(map[string]bool)
	for required := range o.RequiredModules {
		foundRequired[required] = false
	}

	for _, mod := range modules {
		name := mod.Name()

		// Check denied modules first (highest precedence)
		if o.DeniedModules[name] {
			return fmt.Errorf("module %s is not allowed in this build", name)
		}

		// Required modules handling varies by mode
		if o.RequiredModules[name] {
			foundRequired[name] = true
			// In StrictListed mode, required modules must still be explicitly allowed
			if o.AccessMode == StrictListed && !o.AllowedModules[name] {
				return fmt.Errorf("module %s is required but not allowed (StrictListed mode)", name)
			}
			continue
		}

		// Apply access mode checks
		switch o.AccessMode {
		case AllowAll:
			// Allow anything not explicitly denied (already checked above)
		case AllowListed, StrictListed:
			if !o.AllowedModules[name] {
				return fmt.Errorf("module %s is not in the allowed modules list", name)
			}
		case DenyAll:
			return fmt.Errorf("module %s is not allowed (DenyAll mode)", name)
		}
	}

	// Verify all required modules were found
	for mod, found := range foundRequired {
		if !found {
			return fmt.Errorf("required module %s was not found", mod)
		}
	}

	return nil
}
