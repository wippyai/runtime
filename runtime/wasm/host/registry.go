// Package host provides WASM host function registry for wippy.
package host

import (
	"context"
	"sync"

	"github.com/tetratelabs/wazero"

	"github.com/wippyai/runtime/api/dispatcher"
	wasmapi "github.com/wippyai/runtime/api/runtime/wasm"
)

// Registry manages WASM host modules.
type Registry struct {
	mu         sync.RWMutex
	hosts      map[string]wasmapi.Host
	yieldTypes map[dispatcher.CommandID]struct{}
}

// NewRegistry creates a new host registry.
func NewRegistry() *Registry {
	return &Registry{
		hosts:      make(map[string]wasmapi.Host),
		yieldTypes: make(map[dispatcher.CommandID]struct{}),
	}
}

// Register adds a host to the registry.
func (r *Registry) Register(host wasmapi.Host) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	info := host.Info()
	if info.Namespace == "" {
		return ErrEmptyNamespace
	}

	if _, exists := r.hosts[info.Namespace]; exists {
		return NewHostAlreadyRegisteredError(info.Namespace)
	}

	// Register yield types
	reg := host.Register()
	for _, yt := range reg.YieldTypes {
		r.yieldTypes[yt.CmdID] = struct{}{}
	}

	r.hosts[info.Namespace] = host
	return nil
}

// Get returns a host by namespace.
func (r *Registry) Get(namespace string) (wasmapi.Host, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	h, ok := r.hosts[namespace]
	return h, ok
}

// All returns all registered hosts.
func (r *Registry) All() []wasmapi.Host {
	r.mu.RLock()
	defer r.mu.RUnlock()

	hosts := make([]wasmapi.Host, 0, len(r.hosts))
	for _, h := range r.hosts {
		hosts = append(hosts, h)
	}
	return hosts
}

// Namespaces returns all registered host namespaces.
func (r *Registry) Namespaces() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	ns := make([]string, 0, len(r.hosts))
	for n := range r.hosts {
		ns = append(ns, n)
	}
	return ns
}

// YieldTypes returns all registered yield command IDs.
func (r *Registry) YieldTypes() []dispatcher.CommandID {
	r.mu.RLock()
	defer r.mu.RUnlock()

	ids := make([]dispatcher.CommandID, 0, len(r.yieldTypes))
	for id := range r.yieldTypes {
		ids = append(ids, id)
	}
	return ids
}

// HasYieldType checks if a command ID is a registered yield type.
func (r *Registry) HasYieldType(id dispatcher.CommandID) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.yieldTypes[id]
	return ok
}

// InstantiateHosts instantiates all host modules into the wazero runtime.
// Call this once after registering all hosts, before loading guest modules.
func (r *Registry) InstantiateHosts(ctx context.Context, rt wazero.Runtime) error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for namespace, host := range r.hosts {
		reg := host.Register()
		if len(reg.Functions) == 0 {
			continue
		}

		builder := rt.NewHostModuleBuilder(namespace)

		for name, fn := range reg.Functions {
			wrapped := wrapHostFunc(fn)
			builder = builder.NewFunctionBuilder().
				WithGoModuleFunction(wrapped, nil, nil).
				Export(name)
		}

		if _, err := builder.Instantiate(ctx); err != nil {
			return NewInstantiateHostError(namespace, err)
		}
	}
	return nil
}
