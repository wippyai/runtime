// Package http implements wasi:http@0.2.0 for wippy.
package http

import (
	"context"
	"net/http"

	"github.com/tetratelabs/wazero/api"

	wasmapi "github.com/wippyai/runtime/api/runtime/wasm"
	httpservice "github.com/wippyai/runtime/api/service/http"
	"github.com/wippyai/runtime/runtime/wasm/resource"
	streamservice "github.com/wippyai/runtime/service/dispatcher/stream"
)

const (
	// IncomingNamespace is the WASI namespace for incoming HTTP types.
	IncomingNamespace = "wasi:http/types@0.2.0"
)

// Resource type IDs for incoming HTTP resources
const (
	TypeHTTPIncomingRequest  = resource.Handle(110)
	TypeHTTPIncomingBody     = resource.Handle(111)
	TypeHTTPOutgoingResponse = resource.Handle(112)
	TypeHTTPOutgoingBody     = resource.Handle(113)
	TypeHTTPFields           = resource.Handle(114)
)

// IncomingHost implements wasi:http/types@0.2.0 for incoming requests.
type IncomingHost struct {
	resources         *resource.InstanceResources
	incomingRequests  *resource.TypedTable[*HTTPIncomingRequest]
	incomingBodies    *resource.TypedTable[*HTTPIncomingBody]
	outgoingResponses *resource.TypedTable[*HTTPOutgoingResponse]
	outgoingBodies    *resource.TypedTable[*HTTPOutgoingBody]
	fields            *resource.TypedTable[*HTTPFields]
}

// NewIncomingHost creates a new incoming HTTP host with shared resources.
func NewIncomingHost(resources *resource.InstanceResources) *IncomingHost {
	return &IncomingHost{
		resources:         resources,
		incomingRequests:  resource.NewTypedTable[*HTTPIncomingRequest](resources.Table(), uint32(TypeHTTPIncomingRequest)),
		incomingBodies:    resource.NewTypedTable[*HTTPIncomingBody](resources.Table(), uint32(TypeHTTPIncomingBody)),
		outgoingResponses: resource.NewTypedTable[*HTTPOutgoingResponse](resources.Table(), uint32(TypeHTTPOutgoingResponse)),
		outgoingBodies:    resource.NewTypedTable[*HTTPOutgoingBody](resources.Table(), uint32(TypeHTTPOutgoingBody)),
		fields:            resource.NewTypedTable[*HTTPFields](resources.Table(), uint32(TypeHTTPFields)),
	}
}

// Info returns host metadata.
func (h *IncomingHost) Info() wasmapi.HostInfo {
	return wasmapi.HostInfo{
		Namespace:   IncomingNamespace,
		Description: "WASI HTTP incoming request types",
		Class:       []string{wasmapi.ClassNetwork, wasmapi.ClassIO},
	}
}

// Namespace implements wasmrt.Host interface.
func (h *IncomingHost) Namespace() string {
	return IncomingNamespace
}

// Register returns the host registration.
func (h *IncomingHost) Register() *wasmapi.HostRegistration {
	return &wasmapi.HostRegistration{
		Functions: map[string]any{
			// incoming-request methods
			"[method]incoming-request.method":          h.requestMethod,
			"[method]incoming-request.path-with-query": h.requestPathWithQuery,
			"[method]incoming-request.scheme":          h.requestScheme,
			"[method]incoming-request.authority":       h.requestAuthority,
			"[method]incoming-request.headers":         h.requestHeaders,
			"[method]incoming-request.consume":         h.requestConsume,
			"[resource-drop]incoming-request":          h.dropIncomingRequest,

			// incoming-body methods
			"[method]incoming-body.stream": h.bodyStream,
			"[resource-drop]incoming-body": h.dropIncomingBody,

			// outgoing-response methods
			"[constructor]outgoing-response":            h.newOutgoingResponse,
			"[method]outgoing-response.set-status-code": h.setStatusCode,
			"[method]outgoing-response.headers":         h.responseHeaders,
			"[method]outgoing-response.body":            h.responseBody,
			"[resource-drop]outgoing-response":          h.dropOutgoingResponse,

			// outgoing-body methods
			"[method]outgoing-body.write":  h.bodyWrite,
			"[static]outgoing-body.finish": h.bodyFinish,
			"[resource-drop]outgoing-body": h.dropOutgoingBody,

			// fields (headers) methods
			"[constructor]fields":    h.newFields,
			"[method]fields.get":     h.fieldsGet,
			"[method]fields.set":     h.fieldsSet,
			"[method]fields.append":  h.fieldsAppend,
			"[method]fields.delete":  h.fieldsDelete,
			"[method]fields.entries": h.fieldsEntries,
			"[resource-drop]fields":  h.dropFields,
		},
	}
}

// Resources returns the shared resource table.
func (h *IncomingHost) Resources() *resource.InstanceResources {
	return h.resources
}

// SetupRequest creates resources for an incoming HTTP request.
func (h *IncomingHost) SetupRequest(req *http.Request) (requestHandle, bodyHandle resource.Handle) {
	requestHandle = h.incomingRequests.Insert(&HTTPIncomingRequest{Request: req})
	bodyHandle = h.incomingBodies.Insert(&HTTPIncomingBody{Request: req})
	return
}

// Request accessors

func (h *IncomingHost) requestMethod(ctx context.Context, mod api.Module, stack []uint64) {
	req := h.getRequestFromStackOrContext(ctx, stack)
	if req == nil {
		return
	}

	method := req.Request.Method
	ptr, ok := writeString(mod, ctx, method)
	if !ok {
		return
	}
	stack[0] = uint64(ptr)
	if len(stack) > 1 {
		stack[1] = uint64(len(method))
	}
}

func (h *IncomingHost) requestPathWithQuery(ctx context.Context, mod api.Module, stack []uint64) {
	req := h.getRequestFromStackOrContext(ctx, stack)
	if req == nil {
		return
	}

	path := req.Request.URL.Path
	if req.Request.URL.RawQuery != "" {
		path += "?" + req.Request.URL.RawQuery
	}

	ptr, ok := writeString(mod, ctx, path)
	if !ok {
		return
	}
	stack[0] = uint64(ptr)
	if len(stack) > 1 {
		stack[1] = uint64(len(path))
	}
}

func (h *IncomingHost) requestScheme(ctx context.Context, mod api.Module, stack []uint64) {
	req := h.getRequestFromStackOrContext(ctx, stack)
	if req == nil {
		return
	}

	scheme := req.Request.URL.Scheme
	if scheme == "" {
		scheme = "http"
	}

	ptr, ok := writeString(mod, ctx, scheme)
	if !ok {
		return
	}
	stack[0] = uint64(ptr)
	if len(stack) > 1 {
		stack[1] = uint64(len(scheme))
	}
}

func (h *IncomingHost) requestAuthority(ctx context.Context, mod api.Module, stack []uint64) {
	req := h.getRequestFromStackOrContext(ctx, stack)
	if req == nil {
		return
	}

	authority := req.Request.Host
	ptr, ok := writeString(mod, ctx, authority)
	if !ok {
		return
	}
	stack[0] = uint64(ptr)
	if len(stack) > 1 {
		stack[1] = uint64(len(authority))
	}
}

func (h *IncomingHost) requestHeaders(ctx context.Context, mod api.Module, stack []uint64) {
	req := h.getRequestFromStackOrContext(ctx, stack)
	if req == nil {
		return
	}

	fields := &HTTPFields{Values: make(map[string][]string)}
	for k, v := range req.Request.Header {
		fields.Values[k] = v
	}

	handle := h.fields.Insert(fields)
	if len(stack) > 0 {
		stack[0] = uint64(handle)
	}
}

func (h *IncomingHost) requestConsume(ctx context.Context, mod api.Module, stack []uint64) {
	if len(stack) == 0 {
		return
	}
	handle := resource.Handle(stack[0])
	req, ok := h.incomingRequests.Get(handle)
	if !ok {
		return
	}

	bodyHandle := h.incomingBodies.Insert(&HTTPIncomingBody{Request: req.Request})
	stack[0] = uint64(bodyHandle)
}

func (h *IncomingHost) dropIncomingRequest(ctx context.Context, mod api.Module, stack []uint64) {
	if len(stack) > 0 {
		h.resources.Table().Remove(resource.Handle(stack[0]))
	}
}

// Body methods

func (h *IncomingHost) bodyStream(ctx context.Context, mod api.Module, stack []uint64) {
	if len(stack) == 0 {
		return
	}
	handle := resource.Handle(stack[0])
	body, ok := h.incomingBodies.Get(handle)
	if !ok || body.Request == nil || body.Request.Body == nil {
		stack[0] = 0
		return
	}

	// Register the request body with the stream registry for async reads.
	// This allows the dispatcher to handle reads without blocking.
	registry := streamservice.GetOrCreateStreamRegistry(ctx)
	streamID := registry.RegisterStream(body.Request.Body)

	// Create an InputStream resource with the registered stream ID.
	inputStream := &resource.InputStream{
		StreamID: streamID,
	}
	streamHandle := h.resources.InputStreams().Insert(inputStream)
	stack[0] = uint64(streamHandle)
}

func (h *IncomingHost) dropIncomingBody(ctx context.Context, mod api.Module, stack []uint64) {
	if len(stack) > 0 {
		h.resources.Table().Remove(resource.Handle(stack[0]))
	}
}

// Response methods

func (h *IncomingHost) newOutgoingResponse(ctx context.Context, mod api.Module, stack []uint64) {
	resp := &HTTPOutgoingResponse{
		StatusCode: 200,
		Headers:    make(map[string][]string),
	}

	if len(stack) > 0 && stack[0] != 0 {
		if fields, ok := h.fields.Get(resource.Handle(stack[0])); ok {
			for k, v := range fields.Values {
				resp.Headers[k] = v
			}
		}
	}

	handle := h.outgoingResponses.Insert(resp)
	stack[0] = uint64(handle)
}

func (h *IncomingHost) setStatusCode(ctx context.Context, mod api.Module, stack []uint64) {
	if len(stack) < 2 {
		return
	}
	handle := resource.Handle(stack[0])
	statusCode := uint16(stack[1])

	if resp, ok := h.outgoingResponses.Get(handle); ok {
		resp.StatusCode = statusCode
	}
}

func (h *IncomingHost) responseHeaders(ctx context.Context, mod api.Module, stack []uint64) {
	if len(stack) == 0 {
		return
	}
	handle := resource.Handle(stack[0])
	resp, ok := h.outgoingResponses.Get(handle)
	if !ok {
		stack[0] = 0
		return
	}

	fields := &HTTPFields{Values: resp.Headers}
	fieldsHandle := h.fields.Insert(fields)
	stack[0] = uint64(fieldsHandle)
}

func (h *IncomingHost) responseBody(ctx context.Context, mod api.Module, stack []uint64) {
	if len(stack) == 0 {
		return
	}
	handle := resource.Handle(stack[0])
	resp, ok := h.outgoingResponses.Get(handle)
	if !ok {
		stack[0] = 0
		return
	}

	body := &HTTPOutgoingBody{Response: resp}
	bodyHandle := h.outgoingBodies.Insert(body)
	stack[0] = uint64(bodyHandle)
}

func (h *IncomingHost) dropOutgoingResponse(ctx context.Context, mod api.Module, stack []uint64) {
	if len(stack) > 0 {
		h.resources.Table().Remove(resource.Handle(stack[0]))
	}
}

// OutgoingBody methods

func (h *IncomingHost) bodyWrite(ctx context.Context, mod api.Module, stack []uint64) {
	if len(stack) == 0 {
		return
	}
	handle := resource.Handle(stack[0])
	body, ok := h.outgoingBodies.Get(handle)
	if !ok {
		stack[0] = 0
		return
	}

	// Register a buffer writer with the stream registry for async writes.
	registry := streamservice.GetOrCreateStreamRegistry(ctx)
	streamID := registry.RegisterStream(&bodyBufferWriter{body: body})

	// Create an OutputStream resource with the registered stream ID.
	outputStream := &resource.OutputStream{
		StreamID: streamID,
	}
	streamHandle := h.resources.OutputStreams().Insert(outputStream)
	stack[0] = uint64(streamHandle)
}

func (h *IncomingHost) bodyFinish(ctx context.Context, mod api.Module, stack []uint64) {
	if len(stack) == 0 {
		return
	}
	handle := resource.Handle(stack[0])
	body, ok := h.outgoingBodies.Get(handle)
	if !ok || body.Response == nil {
		return
	}

	reqCtx, ok := httpservice.GetRequestContext(ctx)
	if !ok {
		return
	}

	w := reqCtx.ResponseWriter()
	for k, vals := range body.Response.Headers {
		for _, v := range vals {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(int(body.Response.StatusCode))
	if len(body.Data) > 0 {
		w.Write(body.Data)
	}
	reqCtx.MarkHandled()

	h.resources.Table().Remove(handle)
}

func (h *IncomingHost) dropOutgoingBody(ctx context.Context, mod api.Module, stack []uint64) {
	if len(stack) > 0 {
		h.resources.Table().Remove(resource.Handle(stack[0]))
	}
}

// Fields methods

func (h *IncomingHost) newFields(ctx context.Context, mod api.Module, stack []uint64) {
	fields := &HTTPFields{Values: make(map[string][]string)}
	handle := h.fields.Insert(fields)
	if len(stack) > 0 {
		stack[0] = uint64(handle)
	}
}

func (h *IncomingHost) fieldsGet(ctx context.Context, mod api.Module, stack []uint64) {
	if len(stack) < 3 {
		return
	}
	handle := resource.Handle(stack[0])
	namePtr := uint32(stack[1])
	nameLen := uint32(stack[2])

	fields, ok := h.fields.Get(handle)
	if !ok {
		return
	}

	name, ok := readString(mod, namePtr, nameLen)
	if !ok {
		return
	}

	vals := fields.Values[name]
	if len(vals) == 0 {
		stack[0] = 0
		return
	}

	ptr, ok := writeString(mod, ctx, vals[0])
	if !ok {
		return
	}
	stack[0] = uint64(ptr)
	if len(stack) > 1 {
		stack[1] = uint64(len(vals[0]))
	}
}

func (h *IncomingHost) fieldsSet(ctx context.Context, mod api.Module, stack []uint64) {
	if len(stack) < 5 {
		return
	}
	handle := resource.Handle(stack[0])
	namePtr := uint32(stack[1])
	nameLen := uint32(stack[2])
	valuePtr := uint32(stack[3])
	valueLen := uint32(stack[4])

	fields, ok := h.fields.Get(handle)
	if !ok {
		return
	}

	name, ok := readString(mod, namePtr, nameLen)
	if !ok {
		return
	}
	value, ok := readString(mod, valuePtr, valueLen)
	if !ok {
		return
	}

	fields.Values[name] = []string{value}
}

func (h *IncomingHost) fieldsAppend(ctx context.Context, mod api.Module, stack []uint64) {
	if len(stack) < 5 {
		return
	}
	handle := resource.Handle(stack[0])
	namePtr := uint32(stack[1])
	nameLen := uint32(stack[2])
	valuePtr := uint32(stack[3])
	valueLen := uint32(stack[4])

	fields, ok := h.fields.Get(handle)
	if !ok {
		return
	}

	name, ok := readString(mod, namePtr, nameLen)
	if !ok {
		return
	}
	value, ok := readString(mod, valuePtr, valueLen)
	if !ok {
		return
	}

	fields.Values[name] = append(fields.Values[name], value)
}

func (h *IncomingHost) fieldsDelete(ctx context.Context, mod api.Module, stack []uint64) {
	if len(stack) < 3 {
		return
	}
	handle := resource.Handle(stack[0])
	namePtr := uint32(stack[1])
	nameLen := uint32(stack[2])

	fields, ok := h.fields.Get(handle)
	if !ok {
		return
	}

	name, ok := readString(mod, namePtr, nameLen)
	if !ok {
		return
	}

	delete(fields.Values, name)
}

func (h *IncomingHost) fieldsEntries(ctx context.Context, mod api.Module, stack []uint64) {
	if len(stack) == 0 {
		return
	}
	stack[0] = 0
}

func (h *IncomingHost) dropFields(ctx context.Context, mod api.Module, stack []uint64) {
	if len(stack) > 0 {
		h.resources.Table().Remove(resource.Handle(stack[0]))
	}
}

// Helper to get request from stack handle or context
func (h *IncomingHost) getRequestFromStackOrContext(ctx context.Context, stack []uint64) *HTTPIncomingRequest {
	if len(stack) > 0 {
		if req, ok := h.incomingRequests.Get(resource.Handle(stack[0])); ok {
			return req
		}
	}
	reqCtx, ok := httpservice.GetRequestContext(ctx)
	if !ok || reqCtx.Request() == nil {
		return nil
	}
	return &HTTPIncomingRequest{Request: reqCtx.Request()}
}

// Helper to write string to WASM memory
func writeString(mod api.Module, ctx context.Context, s string) (uint32, bool) {
	mem := mod.Memory()
	if mem == nil {
		return 0, false
	}

	realloc := mod.ExportedFunction("cabi_realloc")
	if realloc == nil {
		realloc = mod.ExportedFunction("canonical_abi_realloc")
	}

	var ptr uint32
	if realloc != nil {
		results, err := realloc.Call(ctx, 0, 0, 1, uint64(len(s)))
		if err != nil || len(results) == 0 {
			return 0, false
		}
		ptr = uint32(results[0])
	} else {
		ptr = 65536
	}

	if !mem.Write(ptr, []byte(s)) {
		return 0, false
	}
	return ptr, true
}

// Helper to read string from WASM memory
func readString(mod api.Module, ptr, length uint32) (string, bool) {
	mem := mod.Memory()
	if mem == nil {
		return "", false
	}
	data, ok := mem.Read(ptr, length)
	if !ok {
		return "", false
	}
	return string(data), true
}

// Resource types

type HTTPIncomingRequest struct {
	Request *http.Request
}

// Drop implements resource.Dropper.
func (r *HTTPIncomingRequest) Drop() {
	if r.Request != nil && r.Request.Body != nil {
		r.Request.Body.Close()
	}
	r.Request = nil
}

type HTTPIncomingBody struct {
	Request *http.Request
	Data    []byte
}

// Drop implements resource.Dropper.
func (b *HTTPIncomingBody) Drop() {
	if b.Request != nil && b.Request.Body != nil {
		b.Request.Body.Close()
	}
	b.Request = nil
	b.Data = nil
}

type HTTPOutgoingResponse struct {
	StatusCode uint16
	Headers    map[string][]string
}

// Drop implements resource.Dropper.
func (r *HTTPOutgoingResponse) Drop() {
	r.Headers = nil
}

type HTTPOutgoingBody struct {
	Response *HTTPOutgoingResponse
	Data     []byte
}

// Drop implements resource.Dropper.
func (b *HTTPOutgoingBody) Drop() {
	b.Response = nil
	b.Data = nil
}

type HTTPFields struct {
	Values map[string][]string
}

// Drop implements resource.Dropper.
func (f *HTTPFields) Drop() {
	f.Values = nil
}

// bodyBufferWriter wraps HTTPOutgoingBody for stream writes.
// Implements io.WriteCloser for StreamRegistry.
type bodyBufferWriter struct {
	body *HTTPOutgoingBody
}

func (w *bodyBufferWriter) Write(p []byte) (int, error) {
	w.body.Data = append(w.body.Data, p...)
	return len(p), nil
}

func (w *bodyBufferWriter) Close() error {
	return nil
}

// Compile-time check
var _ wasmapi.Host = (*IncomingHost)(nil)
