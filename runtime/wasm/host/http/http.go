// Package http provides the HTTP client host module for WASM.
package http

import (
	"context"
	"time"
	"unsafe"

	"github.com/tetratelabs/wazero/api"

	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/runtime/resource"
	wasmapi "github.com/wippyai/runtime/api/runtime/wasm"
	httpapi "github.com/wippyai/runtime/api/service/http"
	"github.com/wippyai/runtime/runtime/wasm/host"
)

// Namespace is the WIT namespace for HTTP functions.
const Namespace = "wippy:http"

// Resource type IDs for the resource table.
const (
	TypeRequest  uint32 = 1
	TypeResponse uint32 = 2
)

// Host implements the HTTP client host module.
type Host struct{}

// New creates a new HTTP host.
func New() *Host {
	return &Host{}
}

// Info returns host metadata.
func (h *Host) Info() wasmapi.HostInfo {
	return wasmapi.HostInfo{
		Namespace:   Namespace,
		Description: "HTTP client: request/response operations",
		Class:       []string{wasmapi.ClassNetwork, wasmapi.ClassNondeterministic},
	}
}

// Register returns the host registration.
func (h *Host) Register() *wasmapi.HostRegistration {
	return &wasmapi.HostRegistration{
		Functions: map[string]any{
			// Request builder (sync)
			"request-new":         requestNew,
			"request-set-method":  requestSetMethod,
			"request-set-url":     requestSetURL,
			"request-add-header":  requestAddHeader,
			"request-set-body":    requestSetBody,
			"request-set-timeout": requestSetTimeout,
			"request-drop":        requestDrop,

			// Send request (async)
			"request-send": host.MakeAsyncHandler(makeRequestSendCmd),

			// Response accessors (sync)
			"response-status":     responseStatus,
			"response-body-len":   responseBodyLen,
			"response-body-read":  responseBodyRead,
			"response-header-get": responseHeaderGet,
			"response-header-len": responseHeaderLen,
			"response-drop":       responseDrop,
		},
		YieldTypes: []wasmapi.YieldType{
			{CmdID: httpapi.Request},
		},
	}
}

// requestNew creates a new request builder, returns handle.
func requestNew(ctx context.Context, mod api.Module, stack []uint64) {
	store := resource.GetStore(ctx)
	if store == nil {
		stack[0] = 0
		return
	}

	req := httpapi.AcquireRequestCmd()
	req.Method = "GET"
	handle := store.Table().Insert(TypeRequest, req)
	stack[0] = uint64(handle)
}

// requestSetMethod sets request method.
func requestSetMethod(ctx context.Context, mod api.Module, stack []uint64) {
	handle := resource.Handle(stack[0])
	ptr := uint32(stack[1])
	length := uint32(stack[2])

	store := resource.GetStore(ctx)
	if store == nil {
		return
	}

	val, ok := store.Table().Get(handle)
	if !ok {
		return
	}
	req, ok := val.(*httpapi.RequestCmd)
	if !ok {
		return
	}

	data, ok := mod.Memory().Read(ptr, length)
	if !ok {
		return
	}
	req.Method = string(data)
}

// requestSetURL sets request URL.
func requestSetURL(ctx context.Context, mod api.Module, stack []uint64) {
	handle := resource.Handle(stack[0])
	ptr := uint32(stack[1])
	length := uint32(stack[2])

	store := resource.GetStore(ctx)
	if store == nil {
		return
	}

	val, ok := store.Table().Get(handle)
	if !ok {
		return
	}
	req, ok := val.(*httpapi.RequestCmd)
	if !ok {
		return
	}

	data, ok := mod.Memory().Read(ptr, length)
	if !ok {
		return
	}
	req.URL = string(data)
}

// requestAddHeader adds a header to the request.
func requestAddHeader(ctx context.Context, mod api.Module, stack []uint64) {
	handle := resource.Handle(stack[0])
	keyPtr := uint32(stack[1])
	keyLen := uint32(stack[2])
	valPtr := uint32(stack[3])
	valLen := uint32(stack[4])

	store := resource.GetStore(ctx)
	if store == nil {
		return
	}

	val, ok := store.Table().Get(handle)
	if !ok {
		return
	}
	req, ok := val.(*httpapi.RequestCmd)
	if !ok {
		return
	}

	keyData, ok := mod.Memory().Read(keyPtr, keyLen)
	if !ok {
		return
	}
	valData, ok := mod.Memory().Read(valPtr, valLen)
	if !ok {
		return
	}

	if req.Headers == nil {
		req.Headers = make(map[string]string)
	}
	req.Headers[string(keyData)] = string(valData)
}

// requestSetBody sets request body.
func requestSetBody(ctx context.Context, mod api.Module, stack []uint64) {
	handle := resource.Handle(stack[0])
	ptr := uint32(stack[1])
	length := uint32(stack[2])

	store := resource.GetStore(ctx)
	if store == nil {
		return
	}

	val, ok := store.Table().Get(handle)
	if !ok {
		return
	}
	req, ok := val.(*httpapi.RequestCmd)
	if !ok {
		return
	}

	data, ok := mod.Memory().Read(ptr, length)
	if !ok {
		return
	}
	req.Body = make([]byte, len(data))
	copy(req.Body, data)
}

// requestSetTimeout sets request timeout in nanoseconds.
func requestSetTimeout(ctx context.Context, mod api.Module, stack []uint64) {
	handle := resource.Handle(stack[0])
	timeoutNs := int64(stack[1])

	store := resource.GetStore(ctx)
	if store == nil {
		return
	}

	val, ok := store.Table().Get(handle)
	if !ok {
		return
	}
	req, ok := val.(*httpapi.RequestCmd)
	if !ok {
		return
	}

	req.Timeout = time.Duration(timeoutNs)
}

// requestDrop releases a request handle.
func requestDrop(ctx context.Context, mod api.Module, stack []uint64) {
	handle := resource.Handle(stack[0])

	store := resource.GetStore(ctx)
	if store == nil {
		return
	}

	val, ok := store.Table().Remove(handle)
	if ok {
		if req, ok := val.(*httpapi.RequestCmd); ok {
			req.Release()
		}
	}
}

// RequestSendCmd wraps request handle for async send.
type RequestSendCmd struct {
	Request *httpapi.RequestCmd
	Handle  resource.Handle
}

func (c *RequestSendCmd) CmdID() dispatcher.CommandID {
	return httpapi.Request
}

// makeRequestSendCmd creates command from stack (request handle).
func makeRequestSendCmd(stack []uint64) dispatcher.Command {
	// Note: actual request extraction happens in handler
	// We pass the handle, handler will look it up
	return &RequestSendCmd{
		Handle: resource.Handle(stack[0]),
	}
}

// responseStatus returns HTTP status code.
func responseStatus(ctx context.Context, mod api.Module, stack []uint64) {
	handle := resource.Handle(stack[0])

	store := resource.GetStore(ctx)
	if store == nil {
		stack[0] = 0
		return
	}

	val, ok := store.Table().Get(handle)
	if !ok {
		stack[0] = 0
		return
	}
	resp, ok := val.(*httpapi.Response)
	if !ok {
		stack[0] = 0
		return
	}

	stack[0] = uint64(resp.StatusCode)
}

// responseBodyLen returns response body length.
func responseBodyLen(ctx context.Context, mod api.Module, stack []uint64) {
	handle := resource.Handle(stack[0])

	store := resource.GetStore(ctx)
	if store == nil {
		stack[0] = 0
		return
	}

	val, ok := store.Table().Get(handle)
	if !ok {
		stack[0] = 0
		return
	}
	resp, ok := val.(*httpapi.Response)
	if !ok {
		stack[0] = 0
		return
	}

	stack[0] = uint64(len(resp.Body))
}

// responseBodyRead reads response body into buffer, returns bytes read.
func responseBodyRead(ctx context.Context, mod api.Module, stack []uint64) {
	handle := resource.Handle(stack[0])
	ptr := uint32(stack[1])
	maxLen := uint32(stack[2])

	store := resource.GetStore(ctx)
	if store == nil {
		stack[0] = 0
		return
	}

	val, ok := store.Table().Get(handle)
	if !ok {
		stack[0] = 0
		return
	}
	resp, ok := val.(*httpapi.Response)
	if !ok {
		stack[0] = 0
		return
	}

	toRead := uint32(len(resp.Body))
	if toRead > maxLen {
		toRead = maxLen
	}

	if toRead > 0 {
		if !mod.Memory().Write(ptr, resp.Body[:toRead]) {
			stack[0] = 0
			return
		}
	}

	stack[0] = uint64(toRead)
}

// responseHeaderGet reads a header value, returns length or 0 if not found.
func responseHeaderGet(ctx context.Context, mod api.Module, stack []uint64) {
	handle := resource.Handle(stack[0])
	keyPtr := uint32(stack[1])
	keyLen := uint32(stack[2])
	valPtr := uint32(stack[3])
	maxLen := uint32(stack[4])

	store := resource.GetStore(ctx)
	if store == nil {
		stack[0] = 0
		return
	}

	val, ok := store.Table().Get(handle)
	if !ok {
		stack[0] = 0
		return
	}
	resp, ok := val.(*httpapi.Response)
	if !ok {
		stack[0] = 0
		return
	}

	keyData, ok := mod.Memory().Read(keyPtr, keyLen)
	if !ok {
		stack[0] = 0
		return
	}

	headerVal, exists := resp.Headers[unsafeString(keyData)]
	if !exists {
		stack[0] = 0
		return
	}

	toWrite := uint32(len(headerVal))
	if toWrite > maxLen {
		toWrite = maxLen
	}

	if toWrite > 0 {
		if !mod.Memory().WriteString(valPtr, headerVal[:toWrite]) {
			stack[0] = 0
			return
		}
	}

	stack[0] = uint64(len(headerVal))
}

// responseHeaderLen returns number of headers.
func responseHeaderLen(ctx context.Context, mod api.Module, stack []uint64) {
	handle := resource.Handle(stack[0])

	store := resource.GetStore(ctx)
	if store == nil {
		stack[0] = 0
		return
	}

	val, ok := store.Table().Get(handle)
	if !ok {
		stack[0] = 0
		return
	}
	resp, ok := val.(*httpapi.Response)
	if !ok {
		stack[0] = 0
		return
	}

	stack[0] = uint64(len(resp.Headers))
}

// responseDrop releases a response handle.
func responseDrop(ctx context.Context, mod api.Module, stack []uint64) {
	handle := resource.Handle(stack[0])

	store := resource.GetStore(ctx)
	if store == nil {
		return
	}

	store.Table().Remove(handle)
}

// unsafeString converts bytes to string without allocation.
func unsafeString(b []byte) string {
	return *(*string)(unsafe.Pointer(&b))
}

// WIT definition for the HTTP interface.
const WIT = `package wippy:http@0.1.0;

interface http-client {
    // Request builder
    request-new: func() -> u64;
    request-set-method: func(handle: u64, method-ptr: u32, method-len: u32);
    request-set-url: func(handle: u64, url-ptr: u32, url-len: u32);
    request-add-header: func(handle: u64, key-ptr: u32, key-len: u32, val-ptr: u32, val-len: u32);
    request-set-body: func(handle: u64, body-ptr: u32, body-len: u32);
    request-set-timeout: func(handle: u64, timeout-ns: s64);
    request-drop: func(handle: u64);

    // Send request (async, returns response handle)
    request-send: func(handle: u64) -> u64;

    // Response accessors
    response-status: func(handle: u64) -> u32;
    response-body-len: func(handle: u64) -> u32;
    response-body-read: func(handle: u64, buf-ptr: u32, buf-len: u32) -> u32;
    response-header-get: func(handle: u64, key-ptr: u32, key-len: u32, val-ptr: u32, val-len: u32) -> u32;
    response-header-len: func(handle: u64) -> u32;
    response-drop: func(handle: u64);
}

world with-http {
    import http-client;
}
`

// compile-time check
var _ wasmapi.Host = (*Host)(nil)
