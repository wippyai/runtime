package io

import (
	"context"

	"github.com/tetratelabs/wazero/api"

	apiresource "github.com/wippyai/runtime/api/resource"
	wasmapi "github.com/wippyai/runtime/api/runtime/wasm"
	"github.com/wippyai/runtime/runtime/wasm/resource"
)

const (
	// ErrorNamespace is the WASI namespace for I/O errors.
	ErrorNamespace = "wasi:io/error@0.2.8"
)

// Resource type IDs for error resources
const (
	TypeIOError = resource.Handle(200)
)

// ErrorHost implements wasi:io/error@0.2.8.
type ErrorHost struct {
	resources *resource.InstanceResources
	errors    *apiresource.TypedTable[*IOError]
}

// NewErrorHost creates a new error host with shared resources.
func NewErrorHost(resources *resource.InstanceResources) *ErrorHost {
	return &ErrorHost{
		resources: resources,
		errors:    apiresource.NewTypedTable[*IOError](resources.Table(), uint32(TypeIOError)),
	}
}

// Info returns host metadata.
func (h *ErrorHost) Info() wasmapi.HostInfo {
	return wasmapi.HostInfo{
		Namespace:   ErrorNamespace,
		Description: "WASI I/O error handling",
		Class:       []string{wasmapi.ClassIO},
	}
}

// Namespace implements wasmrt.Host interface.
func (h *ErrorHost) Namespace() string {
	return ErrorNamespace
}

// Register returns the host registration.
func (h *ErrorHost) Register() *wasmapi.HostRegistration {
	return &wasmapi.HostRegistration{
		Functions: map[string]any{
			"[method]error.to-debug-string": h.toDebugString,
			"[resource-drop]error":          h.dropError,
		},
	}
}

// Resources returns the shared resource table.
func (h *ErrorHost) Resources() *resource.InstanceResources {
	return h.resources
}

// Errors returns the typed table for errors.
func (h *ErrorHost) Errors() *apiresource.TypedTable[*IOError] {
	return h.errors
}

// CreateError creates a new error resource and returns its handle.
func (h *ErrorHost) CreateError(code ErrorCode, message string) resource.Handle {
	err := &IOError{
		Code:    code,
		Message: message,
	}
	return h.errors.Insert(err)
}

// toDebugString returns a debug string for an error.
// Stack: [handle: u32] -> [ptr: u32, len: u32]
func (h *ErrorHost) toDebugString(ctx context.Context, mod api.Module, stack []uint64) {
	if len(stack) == 0 {
		return
	}

	handle := resource.Handle(stack[0])
	ioErr, ok := h.errors.Get(handle)
	if !ok {
		stack[0] = 0
		if len(stack) > 1 {
			stack[1] = 0
		}
		return
	}

	msg := ioErr.DebugString()

	mem := mod.Memory()
	if mem == nil {
		return
	}

	realloc := mod.ExportedFunction("cabi_realloc")
	if realloc == nil {
		realloc = mod.ExportedFunction("canonical_abi_realloc")
	}

	var ptr uint32
	if realloc != nil {
		results, err := realloc.Call(ctx, 0, 0, 1, uint64(len(msg)))
		if err != nil || len(results) == 0 {
			return
		}
		ptr = uint32(results[0])
	} else {
		ptr = 65536
	}

	if !mem.Write(ptr, []byte(msg)) {
		return
	}

	stack[0] = uint64(ptr)
	if len(stack) > 1 {
		stack[1] = uint64(len(msg))
	}
}

// dropError removes an error resource.
func (h *ErrorHost) dropError(_ context.Context, _ api.Module, stack []uint64) {
	if len(stack) > 0 {
		handle := resource.Handle(stack[0])
		h.resources.Table().Remove(handle)
	}
}

// ErrorCode represents WASI I/O error codes.
type ErrorCode uint8

const (
	ErrorCodeUnknown ErrorCode = iota
	ErrorCodeAccess
	ErrorCodeWouldBlock
	ErrorCodeInvalidSeek
	ErrorCodeBrokenPipe
	ErrorCodeConnectionReset
	ErrorCodeConnectionRefused
	ErrorCodeNotConnected
	ErrorCodeTimeout
	ErrorCodeClosed
)

// IOError represents a WASI I/O error resource.
type IOError struct {
	Code    ErrorCode
	Message string
}

// Drop implements resource.Dropper.
func (e *IOError) Drop() {
	e.Message = ""
}

// DebugString returns a debug representation of the error.
func (e *IOError) DebugString() string {
	if e.Message != "" {
		return e.Message
	}
	switch e.Code {
	case ErrorCodeAccess:
		return "access denied"
	case ErrorCodeWouldBlock:
		return "operation would block"
	case ErrorCodeInvalidSeek:
		return "invalid seek"
	case ErrorCodeBrokenPipe:
		return "broken pipe"
	case ErrorCodeConnectionReset:
		return "connection reset"
	case ErrorCodeConnectionRefused:
		return "connection refused"
	case ErrorCodeNotConnected:
		return "not connected"
	case ErrorCodeTimeout:
		return "timeout"
	case ErrorCodeClosed:
		return "closed"
	default:
		return "unknown error"
	}
}

// Compile-time check
var _ wasmapi.Host = (*ErrorHost)(nil)
