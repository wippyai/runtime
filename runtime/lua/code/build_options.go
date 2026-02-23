// SPDX-License-Identifier: MPL-2.0

package code

import (
	"github.com/wippyai/runtime/api/registry"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
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
	allowedSet      map[registry.ID]struct{}
	allowedClassSet map[string]struct{}
	deniedClassSet  map[string]struct{}
	requiredSet     map[registry.ID]struct{}
	deniedSet       map[registry.ID]struct{}
	Required        []registry.ID
	AllowedClasses  []string
	DeniedClasses   []string
	Preloaded       []Preload
	Denied          []registry.ID
	Allowed         []registry.ID
	Mode            AccessMode
	setsInitialized bool
}

// NewBuildOptions creates a new BuildOptions with default settings
func NewBuildOptions() *BuildOptions {
	return &BuildOptions{
		Mode:           AllowAll,
		Allowed:        make([]registry.ID, 0),
		Denied:         make([]registry.ID, 0),
		Required:       make([]registry.ID, 0),
		Preloaded:      make([]Preload, 0),
		DeniedClasses:  make([]string, 0),
		AllowedClasses: make([]string, 0),
	}
}

// invalidateSets marks the lookup sets as needing recomputation
func (o *BuildOptions) invalidateSets() {
	o.setsInitialized = false
}

// WithMode sets the access mode
func (o *BuildOptions) WithMode(mode AccessMode) *BuildOptions {
	o.Mode = mode
	return o
}

// WithAllowed adds IDs to the allowed list
func (o *BuildOptions) WithAllowed(ids ...registry.ID) *BuildOptions {
	o.Allowed = append(o.Allowed, ids...)
	o.invalidateSets()
	return o
}

// WithDenied adds IDs to the denied list
func (o *BuildOptions) WithDenied(ids ...registry.ID) *BuildOptions {
	o.Denied = append(o.Denied, ids...)
	o.invalidateSets()
	return o
}

// WithRequired adds IDs to the required list
func (o *BuildOptions) WithRequired(ids ...registry.ID) *BuildOptions {
	o.Required = append(o.Required, ids...)
	o.invalidateSets()
	return o
}

// WithPreloaded adds dependencies to the preloaded list
func (o *BuildOptions) WithPreloaded(deps ...Preload) *BuildOptions {
	o.Preloaded = append(o.Preloaded, deps...)
	return o
}

// WithDeniedClasses adds classes to the denied classes list
func (o *BuildOptions) WithDeniedClasses(classes ...string) *BuildOptions {
	o.DeniedClasses = append(o.DeniedClasses, classes...)
	o.invalidateSets()
	return o
}

// WithAllowedClasses adds classes to the allowed classes list
func (o *BuildOptions) WithAllowedClasses(classes ...string) *BuildOptions {
	o.AllowedClasses = append(o.AllowedClasses, classes...)
	o.invalidateSets()
	return o
}

// initSets lazily initializes the lookup sets for O(1) access
func (o *BuildOptions) initSets() {
	if o.setsInitialized {
		return
	}

	o.allowedSet = make(map[registry.ID]struct{}, len(o.Allowed))
	for _, id := range o.Allowed {
		o.allowedSet[id] = struct{}{}
	}

	o.deniedSet = make(map[registry.ID]struct{}, len(o.Denied))
	for _, id := range o.Denied {
		o.deniedSet[id] = struct{}{}
	}

	o.requiredSet = make(map[registry.ID]struct{}, len(o.Required))
	for _, id := range o.Required {
		o.requiredSet[id] = struct{}{}
	}

	o.deniedClassSet = make(map[string]struct{}, len(o.DeniedClasses))
	for _, c := range o.DeniedClasses {
		o.deniedClassSet[c] = struct{}{}
	}

	o.allowedClassSet = make(map[string]struct{}, len(o.AllowedClasses))
	for _, c := range o.AllowedClasses {
		o.allowedClassSet[c] = struct{}{}
	}

	o.setsInitialized = true
}

// isAllowed checks if an ID is in the allowed set (O(1))
func (o *BuildOptions) isAllowed(id registry.ID) bool {
	_, ok := o.allowedSet[id]
	return ok
}

// isDenied checks if an ID is in the denied set (O(1))
func (o *BuildOptions) isDenied(id registry.ID) bool {
	_, ok := o.deniedSet[id]
	return ok
}

// isRequired checks if an ID is in the required set (O(1))
func (o *BuildOptions) isRequired(id registry.ID) bool {
	_, ok := o.requiredSet[id]
	return ok
}

// hasAnyDeniedClass checks if any of the module's classes are denied (O(n) where n = module classes)
func (o *BuildOptions) hasAnyDeniedClass(classes []string) bool {
	for _, c := range classes {
		if _, ok := o.deniedClassSet[c]; ok {
			return true
		}
	}
	return false
}

// hasAnyAllowedClass checks if any of the module's classes are allowed (O(n) where n = module classes)
func (o *BuildOptions) hasAnyAllowedClass(classes []string) bool {
	for _, c := range classes {
		if _, ok := o.allowedClassSet[c]; ok {
			return true
		}
	}
	return false
}

// getModuleClasses returns the classes for a node's module, or nil if not a module
func getModuleClasses(node *Node) []string {
	if node.Module == nil {
		return nil
	}
	return node.Module.Info().Class
}

// Validate checks if the given nodes comply with the build constraints
func (o *BuildOptions) Validate(nodes map[registry.ID]*Node) error {
	// Initialize lookup sets for O(1) access
	o.initSets()

	// In StrictListed mode, verify all required IDs are also in allowed list
	if o.Mode == StrictListed {
		for _, required := range o.Required {
			if !o.isAllowed(required) {
				return NewBuildValidationError("required process must also be in allowed list (StrictListed mode)", required)
			}
		}
	}

	// Track required IDs found
	foundRequired := make(map[registry.ID]bool, len(o.Required))
	for _, required := range o.Required {
		foundRequired[required] = false
	}

	// Validate nodes
	for id, node := range nodes {
		// Check denied IDs first (highest precedence)
		if o.isDenied(id) {
			return NewBuildValidationError("process is not allowed in this build", id)
		}

		// Check class-based filtering for modules
		if node.Kind == luaapi.ModuleKind {
			classes := getModuleClasses(node)
			if classes != nil {
				// Check denied classes
				if len(o.DeniedClasses) > 0 && o.hasAnyDeniedClass(classes) {
					return NewBuildValidationError("module has denied class", id)
				}
				// Check allowed classes (if specified, module must have at least one)
				if len(o.AllowedClasses) > 0 && !o.hasAnyAllowedClass(classes) {
					return NewBuildValidationError("module does not have any allowed class", id)
				}
			}
		}

		// Mark required IDs as found
		if o.isRequired(id) {
			foundRequired[id] = true
			// In StrictListed mode, required IDs must still be explicitly allowed
			if o.Mode == StrictListed && !o.isAllowed(id) {
				return NewBuildValidationError("process is required but not allowed (StrictListed mode)", id)
			}
			continue
		}

		// Apply access mode checks for IDs
		switch o.Mode {
		case AllowAll:
			// Allow anything not explicitly denied (already checked above)
		case AllowListed, StrictListed:
			if !o.isAllowed(id) {
				return NewBuildValidationError("process is not in the allowed IDs list", id)
			}
		case DenyAll:
			return NewBuildValidationError("process is not allowed (DenyAll mode)", id)
		}
	}

	// Verify all required IDs were found
	for id, found := range foundRequired {
		if !found {
			return NewBuildValidationError("required process was not found", id)
		}
	}

	return nil
}
