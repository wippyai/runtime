// Package http implements wasi:http@0.2.0 for wippy.
// Maps WASI HTTP requests to wippy's dispatcher http.RequestCmd.
package http

import (
	"context"
	"net/url"
	"time"

	"github.com/tetratelabs/wazero/api"

	"github.com/wippyai/runtime/api/dispatcher"
	httpapi "github.com/wippyai/runtime/api/dispatcher/http"
	"github.com/wippyai/runtime/api/registry"
	apiresource "github.com/wippyai/runtime/api/resource"
	wasmapi "github.com/wippyai/runtime/api/runtime/wasm"
	"github.com/wippyai/runtime/api/security"
	"github.com/wippyai/runtime/runtime/wasm/host"
	"github.com/wippyai/runtime/runtime/wasm/resource"
)

const (
	// OutgoingNamespace is the WASI namespace for outgoing HTTP handler.
	OutgoingNamespace = "wasi:http/outgoing-handler@0.2.0"

	// ActionHTTPRequest is the security action for outgoing HTTP requests.
	ActionHTTPRequest = "wasi.http.request"
)

// Resource type IDs for HTTP-specific resources
const (
	TypeOutgoingRequest  = resource.Handle(100)
	TypeIncomingResponse = resource.Handle(101)
	TypeHTTPBody         = resource.Handle(102)
)

// OutgoingHost implements wasi:http/outgoing-handler@0.2.0.
type OutgoingHost struct {
	resources        *resource.InstanceResources
	outgoingRequests *apiresource.TypedTable[*OutgoingRequest]
	responses        *apiresource.TypedTable[*IncomingResponse]
	bodies           *apiresource.TypedTable[*Body]
}

// NewOutgoingHost creates a new HTTP outgoing handler host with shared resources.
func NewOutgoingHost(resources *resource.InstanceResources) *OutgoingHost {
	return &OutgoingHost{
		resources:        resources,
		outgoingRequests: apiresource.NewTypedTable[*OutgoingRequest](resources.Table(), uint32(TypeOutgoingRequest)),
		responses:        apiresource.NewTypedTable[*IncomingResponse](resources.Table(), uint32(TypeIncomingResponse)),
		bodies:           apiresource.NewTypedTable[*Body](resources.Table(), uint32(TypeHTTPBody)),
	}
}

// Info returns host metadata.
func (h *OutgoingHost) Info() wasmapi.HostInfo {
	return wasmapi.HostInfo{
		Namespace:   OutgoingNamespace,
		Description: "WASI HTTP outgoing request handler",
		Class:       []string{wasmapi.ClassNetwork, wasmapi.ClassIO},
	}
}

// Namespace implements wasmrt.Host interface.
func (h *OutgoingHost) Namespace() string {
	return OutgoingNamespace
}

// Register returns the host registration.
func (h *OutgoingHost) Register() *wasmapi.HostRegistration {
	return &wasmapi.HostRegistration{
		Functions: map[string]any{
			"handle": h.secureHandle,
		},
		YieldTypes: []wasmapi.YieldType{
			{CmdID: httpapi.CmdRequest},
		},
	}
}

// secureHandle wraps the async handler with security checks.
func (h *OutgoingHost) secureHandle(ctx context.Context, mod api.Module, stack []uint64) {
	if len(stack) == 0 {
		return
	}

	handle := resource.Handle(stack[0])
	req, ok := h.outgoingRequests.Get(handle)
	if !ok {
		stack[0] = 0 // error: invalid handle
		return
	}

	// Extract host from URL for security check
	targetHost := ""
	if parsed, err := url.Parse(req.URL); err == nil {
		targetHost = parsed.Host
	}

	meta := registry.Metadata{"url": req.URL, "host": targetHost, "method": req.Method}
	if !security.IsAllowed(ctx, ActionHTTPRequest, targetHost, meta) {
		stack[0] = 0 // error: access denied
		return
	}

	// Delegate to async handler
	handler := host.MakeAsyncHandler(h.makeHandleCmd)
	handler(ctx, mod, stack)
}

// Resources returns the shared resource table.
func (h *OutgoingHost) Resources() *resource.InstanceResources {
	return h.resources
}

// OutgoingRequests returns the typed table for outgoing requests.
func (h *OutgoingHost) OutgoingRequests() *apiresource.TypedTable[*OutgoingRequest] {
	return h.outgoingRequests
}

// Responses returns the typed table for incoming responses.
func (h *OutgoingHost) Responses() *apiresource.TypedTable[*IncomingResponse] {
	return h.responses
}

// Bodies returns the typed table for bodies.
func (h *OutgoingHost) Bodies() *apiresource.TypedTable[*Body] {
	return h.bodies
}

// makeHandleCmd creates an HTTP RequestCmd from WASI handle call.
func (h *OutgoingHost) makeHandleCmd(stack []uint64) dispatcher.Command {
	cmd := httpapi.AcquireRequestCmd()

	if len(stack) > 0 {
		handle := resource.Handle(stack[0])
		if req, ok := h.outgoingRequests.Get(handle); ok {
			cmd.Method = req.Method
			cmd.URL = req.URL
			cmd.Headers = req.Headers
			cmd.Body = req.Body
			cmd.Timeout = req.Timeout
		}
	}

	return cmd
}

// HandleHandler is a raw wazero handler for the handle function.
func (h *OutgoingHost) HandleHandler(ctx context.Context, mod api.Module, stack []uint64) {
	handler := host.MakeAsyncHandler(h.makeHandleCmd)
	handler(ctx, mod, stack)
}

// OutgoingRequest represents a WASI outgoing-request resource.
type OutgoingRequest struct {
	Method  string
	URL     string
	Headers map[string]string
	Body    []byte
	Timeout time.Duration
}

// Drop implements resource.Dropper.
func (r *OutgoingRequest) Drop() {
	r.Headers = nil
	r.Body = nil
}

// IncomingResponse represents a WASI incoming-response resource.
type IncomingResponse struct {
	Status  uint16
	Headers map[string]string
	Body    []byte
}

// Drop implements resource.Dropper.
func (r *IncomingResponse) Drop() {
	r.Headers = nil
	r.Body = nil
}

// Body represents a WASI body resource (incoming or outgoing).
type Body struct {
	Data   []byte
	Offset int
}

// Drop implements resource.Dropper.
func (b *Body) Drop() {
	b.Data = nil
}

// Compile-time check
var _ wasmapi.Host = (*OutgoingHost)(nil)
