package filesystem

import (
	"context"

	"github.com/tetratelabs/wazero/api"
	ctxapi "github.com/wippyai/runtime/api/context"
	fsapi "github.com/wippyai/runtime/api/fs"
	apiresource "github.com/wippyai/runtime/api/resource"
	wasmapi "github.com/wippyai/runtime/api/runtime/wasm"
	"github.com/wippyai/runtime/runtime/wasm/resource"
)

const (
	// PreopensNamespace is the WASI namespace for filesystem preopens.
	PreopensNamespace = "wasi:filesystem/preopens@0.2.0"
)

// PreopensHost implements wasi:filesystem/preopens@0.2.0.
// It provides pre-opened directory handles based on context configuration.
type PreopensHost struct {
	resources   *resource.InstanceResources
	descriptors *apiresource.TypedTable[*Descriptor]
}

// NewPreopensHost creates a new preopens host.
func NewPreopensHost(resources *resource.InstanceResources) *PreopensHost {
	return &PreopensHost{
		resources:   resources,
		descriptors: apiresource.NewTypedTable[*Descriptor](resources.Table(), uint32(TypeDescriptor)),
	}
}

// Info returns host metadata.
func (h *PreopensHost) Info() wasmapi.HostInfo {
	return wasmapi.HostInfo{
		Namespace:   PreopensNamespace,
		Description: "WASI filesystem preopens",
		Class:       []string{wasmapi.ClassStorage},
	}
}

// Namespace implements wasmrt.Host interface.
func (h *PreopensHost) Namespace() string {
	return PreopensNamespace
}

// Register returns the host registration.
func (h *PreopensHost) Register() *wasmapi.HostRegistration {
	return &wasmapi.HostRegistration{
		Functions: map[string]any{
			"get-directories": h.getDirectories,
		},
	}
}

// Resources returns the shared resource table.
func (h *PreopensHost) Resources() *resource.InstanceResources {
	return h.resources
}

// getDirectories returns list of pre-opened directories.
// Stack: [] -> [list_ptr: u32, list_len: u32]
// Each entry is (descriptor_handle: u32, path_ptr: u32, path_len: u32)
func (h *PreopensHost) getDirectories(ctx context.Context, mod api.Module, stack []uint64) {
	mem := mod.Memory()
	if mem == nil {
		if len(stack) > 0 {
			stack[0] = 0
		}
		if len(stack) > 1 {
			stack[1] = 0
		}
		return
	}

	// Get filesystem config from frame context
	var config *Config
	if fc := ctxapi.FrameFromContext(ctx); fc != nil {
		if v, ok := fc.Get(DefaultFSKey); ok {
			config, _ = v.(*Config)
		}
	}

	// Get filesystem registry
	reg := fsapi.GetRegistry(ctx)
	if reg == nil {
		if len(stack) > 0 {
			stack[0] = 0
		}
		if len(stack) > 1 {
			stack[1] = 0
		}
		return
	}

	// Determine preopened directories
	type preopen struct {
		fsID string
		path string
	}
	var preopens []preopen

	if config != nil && config.DefaultFS != "" {
		// Use configured default filesystem
		if _, ok := reg.GetFS(config.DefaultFS); ok {
			rootPath := config.RootPath
			if rootPath == "" {
				rootPath = "/"
			}
			preopens = append(preopens, preopen{
				fsID: config.DefaultFS,
				path: rootPath,
			})
		}
	}

	if len(preopens) == 0 {
		if len(stack) > 0 {
			stack[0] = 0
		}
		if len(stack) > 1 {
			stack[1] = 0
		}
		return
	}

	realloc := mod.ExportedFunction("cabi_realloc")
	if realloc == nil {
		realloc = mod.ExportedFunction("canonical_abi_realloc")
	}
	if realloc == nil {
		return
	}

	// Calculate size needed:
	// - list header: count * 12 bytes (handle u32 + ptr u32 + len u32)
	// - string data
	totalSize := uint64(len(preopens) * 12)
	for _, p := range preopens {
		totalSize += uint64(len(p.path))
	}

	results, err := realloc.Call(ctx, 0, 0, 4, totalSize)
	if err != nil || len(results) == 0 {
		return
	}

	ptr := uint32(results[0])
	dataPtr := ptr + uint32(len(preopens)*12)

	for i, p := range preopens {
		// Create descriptor for this preopen
		desc := &Descriptor{
			FSID:  p.fsID,
			Path:  p.path,
			IsDir: true,
			Flags: FlagRead | FlagMutateDirectory,
		}
		handle := h.descriptors.Insert(desc)

		// Write entry: handle, path_ptr, path_len
		entryPtr := ptr + uint32(i*12)
		mem.WriteUint32Le(entryPtr, uint32(handle))
		mem.WriteUint32Le(entryPtr+4, dataPtr)
		mem.WriteUint32Le(entryPtr+8, uint32(len(p.path)))

		// Write path string
		mem.Write(dataPtr, []byte(p.path))
		dataPtr += uint32(len(p.path))
	}

	if len(stack) > 0 {
		stack[0] = uint64(ptr)
	}
	if len(stack) > 1 {
		stack[1] = uint64(len(preopens))
	}
}

// Compile-time check
var _ wasmapi.Host = (*PreopensHost)(nil)
