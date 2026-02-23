// SPDX-License-Identifier: MPL-2.0

package io

import (
	"context"

	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

const (
	// ErrorNamespace is the WASI IO error namespace.
	ErrorNamespace = "wasi:io/error@0.2.8"
)

// ErrorHost exposes wasi:io/error APIs backed by preview2 resource table.
type ErrorHost struct {
	resources *preview2.ResourceTable
}

// NewErrorHost creates an error host.
func NewErrorHost(resources *preview2.ResourceTable) *ErrorHost {
	return &ErrorHost{resources: resources}
}

// Namespace implements wasm-runtime Host.
func (h *ErrorHost) Namespace() string {
	return ErrorNamespace
}

// MethodErrorToDebugString returns the debug string for an error resource.
func (h *ErrorHost) MethodErrorToDebugString(_ context.Context, self uint32) string {
	r, ok := h.resources.Get(self)
	if !ok {
		return "unknown error"
	}
	if err, ok := r.(*preview2.ErrorResource); ok {
		return err.ToDebugString()
	}
	return "unknown error"
}

// ResourceDropError drops an error resource.
func (h *ErrorHost) ResourceDropError(_ context.Context, self uint32) {
	h.resources.Remove(self)
}

// Register returns explicit WIT function mappings.
func (h *ErrorHost) Register() map[string]any {
	return map[string]any{
		"[method]error.to-debug-string": h.MethodErrorToDebugString,
		"[resource-drop]error":          h.ResourceDropError,
	}
}
