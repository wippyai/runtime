// SPDX-License-Identifier: MPL-2.0

package component

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/wippyai/runtime/api/registry"
	runtimewasm "github.com/wippyai/runtime/runtime/wasm"
	wasmrt "github.com/wippyai/wasm-runtime/runtime"
)

const (
	HostProfileFuncs = "funcs"
	HostProfileWASI1 = "wasi1"

	HostProfileWASIIO         = "wasi:io"
	HostProfileWASIPoll       = "wasi:poll"
	HostProfileWASIClocks     = "wasi:clocks"
	HostProfileWASICLI        = "wasi:cli"
	HostProfileWASIFilesystem = "wasi:filesystem"
	HostProfileWASIRandom     = "wasi:random"
	HostProfileWASISockets    = "wasi:sockets"
	HostProfileWASIHTTP       = "wasi:http"
)

// HostProfile defines a pluggable wasm host import profile.
// Profiles are resolved from configured imports (aliases/canonical names).
type HostProfile struct {
	Register      func(ctx context.Context, rt *wasmrt.Runtime) error
	Name          string
	Aliases       []string
	ComponentOnly bool
}

type hostRegistryKey struct{}

// WithHostRegistry attaches a HostRegistry to the context.
func WithHostRegistry(ctx context.Context, reg *HostRegistry) context.Context {
	return context.WithValue(ctx, hostRegistryKey{}, reg)
}

// GetHostRegistry extracts the HostRegistry from context.
func GetHostRegistry(ctx context.Context) *HostRegistry {
	reg, _ := ctx.Value(hostRegistryKey{}).(*HostRegistry)
	return reg
}

// HostRegistry resolves import IDs to host profiles and registers them once.
type HostRegistry struct {
	sharedResources any
	profiles        map[string]HostProfile
	aliases         map[string]string
	loaded          map[string]bool
	mu              sync.RWMutex
	registerMu      sync.Mutex
}

// NewHostRegistry creates an empty host registry.
func NewHostRegistry() *HostRegistry {
	return &HostRegistry{
		profiles: make(map[string]HostProfile),
		aliases:  make(map[string]string),
		loaded:   make(map[string]bool),
	}
}

// SharedResources returns the shared resource state for this registry.
func (r *HostRegistry) SharedResources() any {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.sharedResources
}

// SetSharedResources stores shared resource state in the registry.
func (r *HostRegistry) SetSharedResources(v any) {
	r.mu.Lock()
	r.sharedResources = v
	r.mu.Unlock()
}

// ResetLoaded clears per-runtime loaded profile state and shared resources.
// Call this when the runtime instance is recreated.
func (r *HostRegistry) ResetLoaded() {
	r.mu.Lock()
	r.loaded = make(map[string]bool)
	r.sharedResources = nil
	r.mu.Unlock()
}

// RegisterProfiles registers one or more host profiles.
// Aliases are normalized and version-agnostic.
func (r *HostRegistry) RegisterProfiles(profiles ...HostProfile) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, profile := range profiles {
		name := normalizeImportToken(profile.Name)
		if name == "" {
			return fmt.Errorf("host profile name cannot be empty")
		}
		if _, exists := r.profiles[name]; exists {
			return fmt.Errorf("host profile already registered: %s", name)
		}

		profile.Name = name
		r.profiles[name] = profile

		for _, aliasRaw := range append(profile.Aliases, name) {
			alias := normalizeImportToken(aliasRaw)
			if alias == "" {
				continue
			}
			if existing, exists := r.aliases[alias]; exists && existing != name {
				return fmt.Errorf("host profile alias conflict: %s already mapped to %s", alias, existing)
			}
			r.aliases[alias] = name
		}
	}

	return nil
}

// Resolve maps an import ID to a registered host profile.
func (r *HostRegistry) Resolve(id registry.ID) (HostProfile, bool) {
	tokens := []string{
		normalizeImportToken(id.Name),
		normalizeImportToken(id.String()),
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, token := range tokens {
		if token == "" {
			continue
		}
		name, ok := r.aliases[token]
		if !ok {
			continue
		}
		profile, ok := r.profiles[name]
		if ok {
			return profile, true
		}
	}

	return HostProfile{}, false
}

// EnsureImports validates configured imports and registers required component hosts.
func (r *HostRegistry) EnsureImports(
	ctx context.Context,
	rt *wasmrt.Runtime,
	imports []registry.ID,
	component bool,
) error {
	if len(imports) == 0 {
		return nil
	}

	required := make(map[string]HostProfile, len(imports))
	for _, id := range imports {
		profile, ok := r.Resolve(id)
		if !ok {
			return runtimewasm.NewUnsupportedHostImportError(id.String())
		}

		if profile.ComponentOnly && !component {
			return runtimewasm.NewComponentHostImportError(id.String())
		}
		required[profile.Name] = profile
	}

	for _, profile := range required {
		if err := r.ensureLoaded(ctx, rt, profile); err != nil {
			return err
		}
	}

	return nil
}

func (r *HostRegistry) ensureLoaded(ctx context.Context, rt *wasmrt.Runtime, profile HostProfile) error {
	if profile.Register == nil {
		return nil
	}
	if rt == nil {
		return runtimewasm.ErrRuntimeNotStarted
	}

	name := normalizeImportToken(profile.Name)
	r.mu.RLock()
	loaded := r.loaded[name]
	r.mu.RUnlock()
	if loaded {
		return nil
	}

	r.registerMu.Lock()
	defer r.registerMu.Unlock()

	r.mu.RLock()
	loaded = r.loaded[name]
	r.mu.RUnlock()
	if loaded {
		return nil
	}

	regCtx := WithHostRegistry(ctx, r)
	if err := profile.Register(regCtx, rt); err != nil {
		return err
	}

	r.mu.Lock()
	r.loaded[name] = true
	r.mu.Unlock()
	return nil
}

func normalizeImportToken(raw string) string {
	name := strings.ToLower(strings.TrimSpace(raw))
	if idx := strings.IndexByte(name, '@'); idx >= 0 {
		name = name[:idx]
	}
	return name
}
