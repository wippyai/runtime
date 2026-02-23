// SPDX-License-Identifier: MPL-2.0

package http

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/url"

	"github.com/wippyai/runtime/api/dispatcher"
	httpapi "github.com/wippyai/runtime/api/service/http"
	"github.com/wippyai/runtime/runtime/security"
	wippyhost "github.com/wippyai/runtime/runtime/wasm/host/wippy"
	wasmengine "github.com/wippyai/wasm-runtime/engine"
	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

func isPrivateIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsUnspecified()
}

func checkPrivateIP(ctx context.Context, urlStr string) string {
	u, err := url.Parse(urlStr)
	if err != nil {
		return ""
	}

	host := u.Hostname()
	if host == "" {
		return ""
	}

	if ip := net.ParseIP(host); ip != nil {
		if isPrivateIP(ip) {
			if !security.IsAllowed(ctx, "http_client.private_ip", host, nil) {
				return "not allowed: private IP " + host
			}
		}
		return ""
	}

	ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
	if err != nil {
		return ""
	}

	for _, ip := range ips {
		if isPrivateIP(ip) {
			if !security.IsAllowed(ctx, "http_client.private_ip", ip.String(), nil) {
				return "not allowed: private IP " + ip.String()
			}
		}
	}

	return ""
}

const (
	// OutgoingHandlerNamespace is the WASI HTTP outgoing handler namespace.
	OutgoingHandlerNamespace = "wasi:http/outgoing-handler@0.2.8"
)

// Outgoing handler resource type IDs (110-113 range, matches wasm-runtime).
const (
	resourceTypeOutgoingRequest        = preview2.ResourceType(110)
	resourceTypeRequestBody            = preview2.ResourceType(111)
	resourceTypeFutureIncomingResponse = preview2.ResourceType(112)
	resourceTypeIncomingResponse       = preview2.ResourceType(113)
)

// OutgoingHandlerHost implements wasi:http/outgoing-handler@0.2.8 through Wippy dispatcher.
// Handle() yields httpapi.RequestCmd via asyncify suspend instead of using direct http.Client.
type OutgoingHandlerHost struct {
	resources *preview2.ResourceTable
}

// NewOutgoingHandlerHost creates a dispatcher-integrated outgoing HTTP host.
func NewOutgoingHandlerHost(resources *preview2.ResourceTable) *OutgoingHandlerHost {
	return &OutgoingHandlerHost{resources: resources}
}

// Namespace implements wasm-runtime Host.
func (h *OutgoingHandlerHost) Namespace() string {
	return OutgoingHandlerNamespace
}

// AsyncFunctions marks handle as async import for asyncify.
func (h *OutgoingHandlerHost) AsyncFunctions() []string {
	return []string{"handle"}
}

// outgoing request resources

type outgoingRequestResource struct {
	url     *url.URL
	headers map[string][]string
	body    *bytes.Buffer
	method  string
}

func (r *outgoingRequestResource) Type() preview2.ResourceType { return resourceTypeOutgoingRequest }
func (r *outgoingRequestResource) Drop()                       {}

type requestBodyResource struct {
	buffer *bytes.Buffer
}

func (b *requestBodyResource) Type() preview2.ResourceType { return resourceTypeRequestBody }
func (b *requestBodyResource) Drop()                       {}

// ConstructorOutgoingRequest creates a new outgoing request.
func (h *OutgoingHandlerHost) ConstructorOutgoingRequest(_ context.Context, headersHandle uint32) uint32 {
	headers := make(map[string][]string)
	if r, ok := h.resources.Get(headersHandle); ok {
		if fields, ok := r.(interface{ Values() map[string][]string }); ok {
			for k, v := range fields.Values() {
				headers[k] = append([]string{}, v...)
			}
		}
	}

	req := &outgoingRequestResource{
		method:  "GET",
		url:     &url.URL{Scheme: "http"},
		headers: headers,
		body:    &bytes.Buffer{},
	}
	return h.resources.Add(req)
}

// MethodOutgoingRequestSetMethod sets the method.
func (h *OutgoingHandlerHost) MethodOutgoingRequestSetMethod(_ context.Context, self uint32, method string) uint32 {
	r, ok := h.resources.Get(self)
	if !ok {
		return 1
	}
	req, ok := r.(*outgoingRequestResource)
	if !ok {
		return 1
	}
	req.method = method
	return 0
}

// MethodOutgoingRequestSetPathWithQuery sets path and query.
func (h *OutgoingHandlerHost) MethodOutgoingRequestSetPathWithQuery(_ context.Context, self uint32, hasPath bool, path string) uint32 {
	r, ok := h.resources.Get(self)
	if !ok {
		return 1
	}
	req, ok := r.(*outgoingRequestResource)
	if !ok {
		return 1
	}
	if hasPath {
		req.url.Path = path
	}
	return 0
}

// MethodOutgoingRequestSetScheme sets the scheme.
func (h *OutgoingHandlerHost) MethodOutgoingRequestSetScheme(_ context.Context, self uint32, hasScheme bool, scheme uint8) uint32 {
	r, ok := h.resources.Get(self)
	if !ok {
		return 1
	}
	req, ok := r.(*outgoingRequestResource)
	if !ok {
		return 1
	}
	if hasScheme {
		if scheme == 1 {
			req.url.Scheme = "https"
		} else {
			req.url.Scheme = "http"
		}
	}
	return 0
}

// MethodOutgoingRequestSetAuthority sets the authority (host).
func (h *OutgoingHandlerHost) MethodOutgoingRequestSetAuthority(_ context.Context, self uint32, hasAuth bool, authority string) uint32 {
	r, ok := h.resources.Get(self)
	if !ok {
		return 1
	}
	req, ok := r.(*outgoingRequestResource)
	if !ok {
		return 1
	}
	if hasAuth {
		req.url.Host = authority
	}
	return 0
}

// MethodOutgoingRequestHeaders returns the request headers.
func (h *OutgoingHandlerHost) MethodOutgoingRequestHeaders(_ context.Context, self uint32) uint32 {
	r, ok := h.resources.Get(self)
	if !ok {
		return 0
	}
	req, ok := r.(*outgoingRequestResource)
	if !ok {
		return 0
	}

	fields := preview2.NewFieldsResource()
	for k, vs := range req.headers {
		for _, v := range vs {
			fields.Append(k, v)
		}
	}
	return h.resources.Add(fields)
}

// MethodOutgoingRequestBody gets the request body.
func (h *OutgoingHandlerHost) MethodOutgoingRequestBody(_ context.Context, self uint32) (uint32, uint32) {
	r, ok := h.resources.Get(self)
	if !ok {
		return 0, 1
	}
	req, ok := r.(*outgoingRequestResource)
	if !ok {
		return 0, 1
	}

	body := &requestBodyResource{buffer: req.body}
	handle := h.resources.Add(body)
	return handle, 0
}

// ResourceDropOutgoingRequest drops an outgoing request resource.
func (h *OutgoingHandlerHost) ResourceDropOutgoingRequest(_ context.Context, self uint32) {
	h.resources.Remove(self)
}

// MethodRequestBodyWrite gets a stream for writing body data.
func (h *OutgoingHandlerHost) MethodRequestBodyWrite(_ context.Context, self uint32) (uint32, uint32) {
	r, ok := h.resources.Get(self)
	if !ok {
		return 0, 1
	}
	body, ok := r.(*requestBodyResource)
	if !ok {
		return 0, 1
	}

	stream := preview2.NewOutputStreamResource(body.buffer)
	handle := h.resources.Add(stream)
	return handle, 0
}

// ResourceDropRequestBody drops a request body resource.
func (h *OutgoingHandlerHost) ResourceDropRequestBody(_ context.Context, self uint32) {
	h.resources.Remove(self)
}

// Handle sends an HTTP request through Wippy dispatcher.
// This is the async point: on first call it suspends with httpapi.RequestCmd,
// on rewind it resolves the response from the async value store.
func (h *OutgoingHandlerHost) Handle(ctx context.Context, requestHandle uint32, _ bool, _ uint32) (uint32, uint32) {
	async := wasmengine.GetAsyncify(ctx)

	if async != nil && async.IsRewinding(ctx) {
		result, err := wasmengine.Resume(ctx)
		if err != nil {
			panic(fmt.Errorf("http outgoing handle resume: %w", err))
		}

		store := wippyhost.GetAsyncValueStore(ctx)
		if store == nil {
			panic("http outgoing handle: async value store not found in context")
		}

		data, ok := store.Take(result)
		if !ok {
			future := &futureIncomingResponseResource{
				err:   fmt.Errorf("response token %d not found", result),
				ready: true,
			}
			return h.resources.Add(future), 0
		}

		resp, ok := data.(httpapi.Response)
		if !ok {
			future := &futureIncomingResponseResource{
				err:   fmt.Errorf("unexpected response type %T", data),
				ready: true,
			}
			return h.resources.Add(future), 0
		}

		future := h.buildFutureFromResponse(resp)
		return h.resources.Add(future), 0
	}

	r, ok := h.resources.Get(requestHandle)
	if !ok {
		return 0, 1
	}
	req, ok := r.(*outgoingRequestResource)
	if !ok {
		return 0, 1
	}

	urlStr := req.url.String()

	if !security.IsAllowed(ctx, "http_client.request", urlStr, nil) {
		future := &futureIncomingResponseResource{
			err:   fmt.Errorf("not allowed: %s", urlStr),
			ready: true,
		}
		return h.resources.Add(future), 0
	}

	if errMsg := checkPrivateIP(ctx, urlStr); errMsg != "" {
		future := &futureIncomingResponseResource{
			err:   fmt.Errorf("%s", errMsg),
			ready: true,
		}
		return h.resources.Add(future), 0
	}

	cmd := h.buildRequestCmd(req)
	op := &RequestPendingOp{cmd: cmd}

	if async == nil {
		panic("http outgoing handle requires asyncify context")
	}

	if err := wasmengine.Suspend(ctx, op); err != nil {
		panic(fmt.Errorf("http outgoing handle suspend: %w", err))
	}

	return 0, 0
}

func (h *OutgoingHandlerHost) buildRequestCmd(req *outgoingRequestResource) *httpapi.RequestCmd {
	cmd := httpapi.AcquireRequestCmd()
	cmd.Method = req.method
	cmd.URL = req.url.String()

	if len(req.headers) > 0 {
		cmd.Headers = make(map[string][]string, len(req.headers))
		for k, vs := range req.headers {
			cmd.Headers[k] = append([]string{}, vs...)
		}
	}

	if req.body != nil && req.body.Len() > 0 {
		body := make([]byte, req.body.Len())
		copy(body, req.body.Bytes())
		cmd.Body = body
	}

	return cmd
}

func (h *OutgoingHandlerHost) buildFutureFromResponse(resp httpapi.Response) *futureIncomingResponseResource {
	if resp.Error != "" {
		return &futureIncomingResponseResource{
			err:   fmt.Errorf("%s", resp.Error),
			ready: true,
		}
	}

	headers := make(map[string][]string, len(resp.Headers))
	for k, vs := range resp.Headers {
		headers[k] = vs
	}

	return &futureIncomingResponseResource{
		statusCode: uint16(resp.StatusCode),
		headers:    headers,
		body:       resp.Body,
		ready:      true,
	}
}

// future incoming response resources

type futureIncomingResponseResource struct {
	err        error
	headers    map[string][]string
	body       []byte
	statusCode uint16
	ready      bool
}

func (f *futureIncomingResponseResource) Type() preview2.ResourceType {
	return resourceTypeFutureIncomingResponse
}
func (f *futureIncomingResponseResource) Drop() {}

// MethodFutureIncomingResponseSubscribe subscribes to the future.
func (h *OutgoingHandlerHost) MethodFutureIncomingResponseSubscribe(_ context.Context, self uint32) uint32 {
	pollable := &preview2.PollableResource{}
	r, ok := h.resources.Get(self)
	if ok {
		if future, ok := r.(*futureIncomingResponseResource); ok {
			pollable.SetReady(future.ready)
		}
	}
	return h.resources.Add(pollable)
}

// MethodFutureIncomingResponseGet gets the response.
func (h *OutgoingHandlerHost) MethodFutureIncomingResponseGet(_ context.Context, self uint32) (uint32, bool, uint32) {
	r, ok := h.resources.Get(self)
	if !ok {
		return 0, false, 0
	}
	future, ok := r.(*futureIncomingResponseResource)
	if !ok {
		return 0, false, 0
	}
	if !future.ready {
		return 0, false, 0
	}
	if future.err != nil {
		return 0, true, 1
	}

	resp := &incomingResponseResource{
		statusCode: future.statusCode,
		headers:    future.headers,
		body:       future.body,
	}
	handle := h.resources.Add(resp)
	return handle, true, 0
}

// ResourceDropFutureIncomingResponse drops a future incoming response.
func (h *OutgoingHandlerHost) ResourceDropFutureIncomingResponse(_ context.Context, self uint32) {
	h.resources.Remove(self)
}

// incoming response resources

type incomingResponseResource struct {
	headers    map[string][]string
	body       []byte
	statusCode uint16
}

func (r *incomingResponseResource) Type() preview2.ResourceType { return resourceTypeIncomingResponse }
func (r *incomingResponseResource) Drop()                       {}

// MethodIncomingResponseStatus gets the status code.
func (h *OutgoingHandlerHost) MethodIncomingResponseStatus(_ context.Context, self uint32) uint16 {
	r, ok := h.resources.Get(self)
	if !ok {
		return 0
	}
	resp, ok := r.(*incomingResponseResource)
	if !ok {
		return 0
	}
	return resp.statusCode
}

// MethodIncomingResponseHeaders gets the response headers.
func (h *OutgoingHandlerHost) MethodIncomingResponseHeaders(_ context.Context, self uint32) uint32 {
	r, ok := h.resources.Get(self)
	if !ok {
		return 0
	}
	resp, ok := r.(*incomingResponseResource)
	if !ok {
		return 0
	}

	fields := preview2.NewFieldsResource()
	for k, vs := range resp.headers {
		for _, v := range vs {
			fields.Append(k, v)
		}
	}
	return h.resources.Add(fields)
}

// MethodIncomingResponseConsume consumes the response body.
func (h *OutgoingHandlerHost) MethodIncomingResponseConsume(_ context.Context, self uint32) (uint32, uint32) {
	r, ok := h.resources.Get(self)
	if !ok {
		return 0, 1
	}
	resp, ok := r.(*incomingResponseResource)
	if !ok {
		return 0, 1
	}
	body := preview2.NewInputStreamResource(resp.body)
	return h.resources.Add(body), 0
}

// ResourceDropIncomingResponse drops an incoming response.
func (h *OutgoingHandlerHost) ResourceDropIncomingResponse(_ context.Context, self uint32) {
	h.resources.Remove(self)
}

// Register implements ExplicitRegistrar.
func (h *OutgoingHandlerHost) Register() map[string]any {
	return map[string]any{
		"handle": h.Handle,
		// Outgoing request methods
		"[constructor]outgoing-request":                h.ConstructorOutgoingRequest,
		"[method]outgoing-request.set-method":          h.MethodOutgoingRequestSetMethod,
		"[method]outgoing-request.set-path-with-query": h.MethodOutgoingRequestSetPathWithQuery,
		"[method]outgoing-request.set-scheme":          h.MethodOutgoingRequestSetScheme,
		"[method]outgoing-request.set-authority":       h.MethodOutgoingRequestSetAuthority,
		"[method]outgoing-request.headers":             h.MethodOutgoingRequestHeaders,
		"[method]outgoing-request.body":                h.MethodOutgoingRequestBody,
		"[resource-drop]outgoing-request":              h.ResourceDropOutgoingRequest,
		// Request body methods
		"[method]request-body.write":  h.MethodRequestBodyWrite,
		"[resource-drop]request-body": h.ResourceDropRequestBody,
		// Future incoming response methods
		"[method]future-incoming-response.subscribe": h.MethodFutureIncomingResponseSubscribe,
		"[method]future-incoming-response.get":       h.MethodFutureIncomingResponseGet,
		"[resource-drop]future-incoming-response":    h.ResourceDropFutureIncomingResponse,
		// Incoming response methods
		"[method]incoming-response.status":  h.MethodIncomingResponseStatus,
		"[method]incoming-response.headers": h.MethodIncomingResponseHeaders,
		"[method]incoming-response.consume": h.MethodIncomingResponseConsume,
		"[resource-drop]incoming-response":  h.ResourceDropIncomingResponse,
	}
}

// RequestPendingOp bridges asyncify suspension to Wippy HTTP dispatcher.
type RequestPendingOp struct {
	cmd *httpapi.RequestCmd
}

// CmdID implements wasm async pending op command ID.
func (o *RequestPendingOp) CmdID() wasmengine.CommandID {
	return wasmengine.CommandID(httpapi.Request)
}

// ToCommand returns dispatcher command for yield path.
func (o *RequestPendingOp) ToCommand() dispatcher.Command {
	return o.cmd
}

// Execute is used by standalone wasm-runtime scheduler loops.
func (o *RequestPendingOp) Execute(_ context.Context) (uint64, error) {
	return 0, fmt.Errorf("HTTP request requires Wippy dispatcher")
}
