// Package random implements wasi:random/random@0.2.8 for wippy.
// Provides cryptographically-secure random number generation.
package random

import (
	"context"
	"crypto/rand"
	"encoding/binary"

	"github.com/tetratelabs/wazero/api"

	wasmapi "github.com/wippyai/runtime/api/runtime/wasm"
)

const (
	// Namespace is the WASI namespace for random.
	Namespace = "wasi:random/random@0.2.8"
)

// Host implements wasi:random/random@0.2.8.
type Host struct{}

// New creates a new random host.
func New() *Host {
	return &Host{}
}

// Info returns host metadata.
func (h *Host) Info() wasmapi.HostInfo {
	return wasmapi.HostInfo{
		Namespace:   Namespace,
		Description: "WASI cryptographically-secure random number generation",
		Class:       []string{wasmapi.ClassNondeterministic},
	}
}

// Namespace implements wasmrt.Host interface.
func (h *Host) Namespace() string {
	return Namespace
}

// Register returns the host registration.
func (h *Host) Register() *wasmapi.HostRegistration {
	return &wasmapi.HostRegistration{
		Functions: map[string]any{
			"get-random-bytes": h.getRandomBytes,
			"get-random-u64":   h.getRandomU64,
		},
	}
}

// getRandomBytes returns len cryptographically-secure random bytes.
// Stack: [len: u64] -> [ptr: u32, len: u32]
func (h *Host) getRandomBytes(ctx context.Context, mod api.Module, stack []uint64) {
	if len(stack) < 1 {
		return
	}
	length := stack[0]
	if length == 0 {
		stack[0] = 0
		if len(stack) > 1 {
			stack[1] = 0
		}
		return
	}

	// Cap at reasonable size
	if length > 65536 {
		length = 65536
	}

	buf := make([]byte, length)
	if _, err := rand.Read(buf); err != nil {
		stack[0] = 0
		if len(stack) > 1 {
			stack[1] = 0
		}
		return
	}

	// Allocate in WASM memory and write bytes
	mem := mod.Memory()
	if mem == nil {
		return
	}

	// Find realloc export for allocation
	realloc := mod.ExportedFunction("cabi_realloc")
	if realloc == nil {
		realloc = mod.ExportedFunction("canonical_abi_realloc")
	}

	var ptr uint32
	if realloc != nil {
		results, err := realloc.Call(ctx, 0, 0, 1, length)
		if err != nil || len(results) == 0 {
			return
		}
		ptr = uint32(results[0])
	} else {
		// Fallback: use a fixed scratch area
		ptr = 65536
	}

	if !mem.Write(ptr, buf) {
		return
	}

	stack[0] = uint64(ptr)
	if len(stack) > 1 {
		stack[1] = length
	}
}

// getRandomU64 returns a cryptographically-secure random u64.
// Stack: [] -> [value: u64]
func (h *Host) getRandomU64(_ context.Context, _ api.Module, stack []uint64) {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		if len(stack) > 0 {
			stack[0] = 0
		}
		return
	}

	if len(stack) > 0 {
		stack[0] = binary.LittleEndian.Uint64(buf[:])
	}
}

// Compile-time check
var _ wasmapi.Host = (*Host)(nil)
