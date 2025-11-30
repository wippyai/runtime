// Package cli implements wasi:cli@0.2.8 for wippy.
// Provides CLI environment, arguments, and standard streams.
package cli

import (
	"context"

	"github.com/tetratelabs/wazero/api"

	"github.com/wippyai/runtime/api/env"
	wasmapi "github.com/wippyai/runtime/api/runtime/wasm"
	"github.com/wippyai/runtime/api/security"
)

const (
	// EnvironmentNamespace is the WASI namespace for CLI environment.
	EnvironmentNamespace = "wasi:cli/environment@0.2.8"
)

// Environment holds CLI environment configuration.
type Environment struct {
	Args []string
	Env  map[string]string
	Cwd  string
}

// EnvironmentHost implements wasi:cli/environment@0.2.8.
type EnvironmentHost struct {
	env *Environment
}

// NewEnvironmentHost creates a new CLI environment host.
func NewEnvironmentHost(env *Environment) *EnvironmentHost {
	if env == nil {
		env = &Environment{
			Args: []string{},
			Env:  map[string]string{},
			Cwd:  "/",
		}
	}
	return &EnvironmentHost{env: env}
}

// Info returns host metadata.
func (h *EnvironmentHost) Info() wasmapi.HostInfo {
	return wasmapi.HostInfo{
		Namespace:   EnvironmentNamespace,
		Description: "WASI CLI environment (args, env vars, cwd)",
		Class:       []string{wasmapi.ClassNondeterministic},
	}
}

// Namespace implements wasmrt.Host interface.
func (h *EnvironmentHost) Namespace() string {
	return EnvironmentNamespace
}

// Register returns the host registration.
func (h *EnvironmentHost) Register() *wasmapi.HostRegistration {
	return &wasmapi.HostRegistration{
		Functions: map[string]any{
			"get-environment": h.getEnvironment,
			"get-arguments":   h.getArguments,
			"initial-cwd":     h.initialCwd,
		},
	}
}

// getEnvironment returns environment variables as list of (key, value) pairs.
// Stack: [] -> [list_ptr: u32, list_len: u32]
func (h *EnvironmentHost) getEnvironment(ctx context.Context, mod api.Module, stack []uint64) {
	mem := mod.Memory()
	if mem == nil {
		return
	}

	realloc := mod.ExportedFunction("cabi_realloc")
	if realloc == nil {
		realloc = mod.ExportedFunction("canonical_abi_realloc")
	}
	if realloc == nil {
		return
	}

	// Collect environment variables from registry with security checks
	envVars := h.collectEnvironment(ctx)
	if len(envVars) == 0 {
		stack[0] = 0
		if len(stack) > 1 {
			stack[1] = 0
		}
		return
	}

	// Build pairs of (key, value) strings
	pairs := make([][]byte, 0, len(envVars)*2)
	for k, v := range envVars {
		pairs = append(pairs, []byte(k), []byte(v))
	}

	// Calculate total size needed
	totalSize := uint64(len(pairs) * 8) // ptr+len for each string
	for _, p := range pairs {
		totalSize += uint64(len(p))
	}

	results, err := realloc.Call(ctx, 0, 0, 4, totalSize)
	if err != nil || len(results) == 0 {
		return
	}

	ptr := uint32(results[0])
	dataPtr := ptr + uint32(len(pairs)*8)

	// Write strings and their descriptors
	for i, p := range pairs {
		mem.Write(dataPtr, p)
		mem.WriteUint32Le(ptr+uint32(i*8), dataPtr)
		mem.WriteUint32Le(ptr+uint32(i*8+4), uint32(len(p)))
		dataPtr += uint32(len(p))
	}

	stack[0] = uint64(ptr)
	if len(stack) > 1 {
		stack[1] = uint64(len(envVars))
	}
}

// collectEnvironment gathers environment variables from registry with security filtering.
// Falls back to static env if no registry is available.
func (h *EnvironmentHost) collectEnvironment(ctx context.Context) map[string]string {
	registry := env.GetRegistry(ctx)
	if registry == nil {
		// Fallback to static environment with security filtering
		result := make(map[string]string, len(h.env.Env))
		for k, v := range h.env.Env {
			if security.IsAllowed(ctx, "env.get", k, nil) {
				result[k] = v
			}
		}
		return result
	}

	// Get all variables from registry
	allVars, err := registry.All(ctx)
	if err != nil {
		return nil
	}

	// Filter by security permissions
	result := make(map[string]string, len(allVars))
	for k, v := range allVars {
		if security.IsAllowed(ctx, "env.get", k, nil) {
			result[k] = v
		}
	}
	return result
}

// getArguments returns command line arguments.
// Stack: [] -> [list_ptr: u32, list_len: u32]
func (h *EnvironmentHost) getArguments(ctx context.Context, mod api.Module, stack []uint64) {
	mem := mod.Memory()
	if mem == nil {
		return
	}

	realloc := mod.ExportedFunction("cabi_realloc")
	if realloc == nil {
		realloc = mod.ExportedFunction("canonical_abi_realloc")
	}

	if len(h.env.Args) == 0 || realloc == nil {
		stack[0] = 0
		if len(stack) > 1 {
			stack[1] = 0
		}
		return
	}

	totalSize := uint64(len(h.env.Args) * 8)
	for _, arg := range h.env.Args {
		totalSize += uint64(len(arg))
	}

	results, err := realloc.Call(ctx, 0, 0, 4, totalSize)
	if err != nil || len(results) == 0 {
		return
	}

	ptr := uint32(results[0])
	dataPtr := ptr + uint32(len(h.env.Args)*8)

	for i, arg := range h.env.Args {
		argBytes := []byte(arg)
		mem.Write(dataPtr, argBytes)
		mem.WriteUint32Le(ptr+uint32(i*8), dataPtr)
		mem.WriteUint32Le(ptr+uint32(i*8+4), uint32(len(argBytes)))
		dataPtr += uint32(len(argBytes))
	}

	stack[0] = uint64(ptr)
	if len(stack) > 1 {
		stack[1] = uint64(len(h.env.Args))
	}
}

// initialCwd returns the initial working directory.
// Stack: [] -> [option<string>: is_some: u32, ptr: u32, len: u32]
func (h *EnvironmentHost) initialCwd(ctx context.Context, mod api.Module, stack []uint64) {
	if h.env.Cwd == "" {
		stack[0] = 0 // none
		return
	}

	mem := mod.Memory()
	if mem == nil {
		stack[0] = 0
		return
	}

	realloc := mod.ExportedFunction("cabi_realloc")
	if realloc == nil {
		realloc = mod.ExportedFunction("canonical_abi_realloc")
	}
	if realloc == nil {
		stack[0] = 0
		return
	}

	cwdBytes := []byte(h.env.Cwd)
	results, err := realloc.Call(ctx, 0, 0, 1, uint64(len(cwdBytes)))
	if err != nil || len(results) == 0 {
		stack[0] = 0
		return
	}

	ptr := uint32(results[0])
	mem.Write(ptr, cwdBytes)

	stack[0] = 1 // some
	if len(stack) > 1 {
		stack[1] = uint64(ptr)
	}
	if len(stack) > 2 {
		stack[2] = uint64(len(cwdBytes))
	}
}

// Compile-time check
var _ wasmapi.Host = (*EnvironmentHost)(nil)
