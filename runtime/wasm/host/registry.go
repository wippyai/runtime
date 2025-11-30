// Package host provides WASM host function registry for wippy.
package host

import (
	"context"
	"fmt"
	"sync"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"

	"github.com/wippyai/runtime/api/dispatcher"
	wasmapi "github.com/wippyai/runtime/api/runtime/wasm"
)

// Registry manages WASM host modules.
// Uses wazero directly for tight integration with wippy's scheduler.
type Registry struct {
	mu         sync.RWMutex
	hosts      map[string]wasmapi.Host             // namespace -> host
	yieldTypes map[dispatcher.CommandID]struct{}   // registered yield command IDs
	builders   map[string]wazero.HostModuleBuilder // namespace -> builder (during bind)
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
		return fmt.Errorf("host namespace cannot be empty")
	}

	if _, exists := r.hosts[info.Namespace]; exists {
		return fmt.Errorf("host %q already registered", info.Namespace)
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
			// Wrap function to support asyncify
			wrapped := wrapHostFunc(fn)
			builder = builder.NewFunctionBuilder().
				WithGoModuleFunction(wrapped, nil, nil).
				Export(name)
		}

		if _, err := builder.Instantiate(ctx); err != nil {
			return fmt.Errorf("instantiate host %s: %w", namespace, err)
		}
	}
	return nil
}

// wrapHostFunc wraps a host function to inject asyncify context.
func wrapHostFunc(fn any) api.GoModuleFunc {
	switch f := fn.(type) {
	case func(ctx context.Context, mod api.Module, stack []uint64):
		return f
	case api.GoModuleFunc:
		return f
	case func(ctx context.Context) int64:
		// Simple sync function returning int64
		return func(ctx context.Context, mod api.Module, stack []uint64) {
			result := f(ctx)
			if len(stack) > 0 {
				stack[0] = uint64(result)
			}
		}
	case func(ctx context.Context, duration int64):
		// Async function with int64 param (like sleep)
		return func(ctx context.Context, mod api.Module, stack []uint64) {
			var duration int64
			if len(stack) > 0 {
				duration = int64(stack[0])
			}
			f(ctx, duration)
		}
	case func(ctx context.Context, duration int64) int64:
		// Async function with int64 param returning int64
		return func(ctx context.Context, mod api.Module, stack []uint64) {
			var duration int64
			if len(stack) > 0 {
				duration = int64(stack[0])
			}
			result := f(ctx, duration)
			if len(stack) > 0 {
				stack[0] = uint64(result)
			}
		}
	default:
		// Fallback - use reflection (slow path)
		return func(ctx context.Context, mod api.Module, stack []uint64) {
			// Not implemented for complex signatures
		}
	}
}

// MakeAsyncHandler creates a wazero host function that supports asyncify.
// The createCmd function creates a dispatcher command from the stack args.
// Uses wippy's native scheduler - no PendingOp wrapping.
// T must implement dispatcher.Command.
func MakeAsyncHandler[T dispatcher.Command](createCmd func(stack []uint64) T) api.GoModuleFunc {
	return func(ctx context.Context, mod api.Module, stack []uint64) {
		frame := wasmapi.GetAsyncFrame(ctx)
		if frame == nil || frame.Asyncify == nil || frame.Scheduler == nil {
			return
		}

		// Check if rewinding (resuming from suspend)
		if frame.Asyncify.IsRewinding(ctx) {
			result, _ := frame.Scheduler.GetResult()
			if len(stack) > 0 {
				stack[0] = result
			}
			frame.Asyncify.StopRewind(ctx)
			frame.Scheduler.ClearPending()
			return
		}

		// Create command and suspend
		cmd := createCmd(stack)
		frame.Scheduler.SetPending(cmd)
		frame.Asyncify.StartUnwind(ctx)
	}
}
