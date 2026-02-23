// SPDX-License-Identifier: MPL-2.0

package http

import (
	"bytes"
	"context"
	"net/http"

	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

const TypesNamespace = "wasi:http/types@0.2.8"

// HTTP resource type IDs (101-104 range).
const (
	resourceTypeOutgoingResponse = preview2.ResourceType(101)
	resourceTypeOutgoingBody     = preview2.ResourceType(102)
	resourceTypeIncomingBody     = preview2.ResourceType(103)
	resourceTypeFutureTrailers   = preview2.ResourceType(104)
)

// Request represents an incoming HTTP request passed to the WASM component.
type Request struct {
	Request *http.Request
	Body    []byte
}

// Response represents the outgoing HTTP response from the WASM component.
type Response struct {
	Headers    map[string][]string
	Body       []byte
	StatusCode uint16
}

// TypesHost implements wasi:http/types@0.2.8.
type TypesHost struct {
	resources *preview2.ResourceTable

	currentRequest  *Request
	currentResponse *Response
	responseBuffer  *bytes.Buffer

	responseOutparamHandle uint32
}

func NewTypesHost(resources *preview2.ResourceTable) *TypesHost {
	return &TypesHost{
		resources:      resources,
		responseBuffer: &bytes.Buffer{},
	}
}

func (h *TypesHost) Namespace() string {
	return TypesNamespace
}

// SetRequest sets the current request for handler invocation.
func (h *TypesHost) SetRequest(req *Request) {
	h.currentRequest = req
	h.responseBuffer.Reset()
}

// GetResponse returns the current response after handler completes.
func (h *TypesHost) GetResponse() *Response {
	if h.currentResponse != nil {
		h.currentResponse.Body = h.responseBuffer.Bytes()
	}
	return h.currentResponse
}

// Reset clears state between handler invocations.
func (h *TypesHost) Reset() {
	h.currentRequest = nil
	h.currentResponse = nil
	h.responseBuffer.Reset()
}

// SetResponseOutparamHandle sets the response outparam handle.
func (h *TypesHost) SetResponseOutparamHandle(handle uint32) {
	h.responseOutparamHandle = handle
}

// GetResponseOutparamHandle returns the current response outparam handle.
func (h *TypesHost) GetResponseOutparamHandle() uint32 {
	return h.responseOutparamHandle
}

// [constructor]fields
func (h *TypesHost) ConstructorFields(_ context.Context) uint32 {
	fields := preview2.NewFieldsResource()
	return h.resources.Add(fields)
}

// [method]fields.append
func (h *TypesHost) MethodFieldsAppend(_ context.Context, self uint32, name string, value []byte) uint32 {
	r, ok := h.resources.Get(self)
	if !ok {
		return 1
	}
	fields, ok := r.(*fieldsResource)
	if !ok {
		return 1
	}
	fields.Append(name, string(value))
	return 0
}

// [method]fields.get
func (h *TypesHost) MethodFieldsGet(_ context.Context, self uint32, name string) [][]byte {
	r, ok := h.resources.Get(self)
	if !ok {
		return nil
	}
	fields, ok := r.(*fieldsResource)
	if !ok {
		return nil
	}
	values := fields.Values()[name]
	result := make([][]byte, len(values))
	for i, v := range values {
		result[i] = []byte(v)
	}
	return result
}

// [method]fields.set
func (h *TypesHost) MethodFieldsSet(_ context.Context, self uint32, name string, values [][]byte) uint32 {
	r, ok := h.resources.Get(self)
	if !ok {
		return 1
	}
	fields, ok := r.(*fieldsResource)
	if !ok {
		return 1
	}
	strValues := make([]string, len(values))
	for i, v := range values {
		strValues[i] = string(v)
	}
	fields.Set(name, strValues)
	return 0
}

// [method]fields.delete
func (h *TypesHost) MethodFieldsDelete(_ context.Context, self uint32, name string) uint32 {
	r, ok := h.resources.Get(self)
	if !ok {
		return 1
	}
	fields, ok := r.(*fieldsResource)
	if !ok {
		return 1
	}
	fields.Delete(name)
	return 0
}

// [method]fields.entries
func (h *TypesHost) MethodFieldsEntries(_ context.Context, self uint32) [][2][]byte {
	r, ok := h.resources.Get(self)
	if !ok {
		return nil
	}
	fields, ok := r.(*fieldsResource)
	if !ok {
		return nil
	}
	var result [][2][]byte
	for name, values := range fields.Values() {
		for _, value := range values {
			result = append(result, [2][]byte{[]byte(name), []byte(value)})
		}
	}
	return result
}

// [method]fields.clone
func (h *TypesHost) MethodFieldsClone(_ context.Context, self uint32) uint32 {
	r, ok := h.resources.Get(self)
	if !ok {
		return h.resources.Add(preview2.NewFieldsResource())
	}
	fields, ok := r.(*fieldsResource)
	if !ok {
		return h.resources.Add(preview2.NewFieldsResource())
	}
	return h.resources.Add(fields.Clone())
}

// [method]fields.has
func (h *TypesHost) MethodFieldsHas(_ context.Context, self uint32, name string) bool {
	r, ok := h.resources.Get(self)
	if !ok {
		return false
	}
	fields, ok := r.(*fieldsResource)
	if !ok {
		return false
	}
	return fields.Has(name)
}

// [resource-drop]fields
func (h *TypesHost) ResourceDropFields(_ context.Context, self uint32) {
	h.resources.Remove(self)
}

// [constructor]outgoing-response
func (h *TypesHost) ConstructorOutgoingResponse(_ context.Context, headersHandle uint32) uint32 {
	headers := make(map[string][]string)

	if r, ok := h.resources.Get(headersHandle); ok {
		if fields, ok := r.(*fieldsResource); ok {
			for k, v := range fields.Values() {
				headers[k] = append([]string{}, v...)
			}
		}
	}

	h.currentResponse = &Response{
		StatusCode: 200,
		Headers:    headers,
	}

	resp := &outgoingResponseResource{response: h.currentResponse}
	return h.resources.Add(resp)
}

// [method]outgoing-response.set-status-code
func (h *TypesHost) MethodOutgoingResponseSetStatusCode(_ context.Context, self uint32, status uint16) uint32 {
	r, ok := h.resources.Get(self)
	if !ok {
		return 1
	}
	resp, ok := r.(*outgoingResponseResource)
	if !ok {
		return 1
	}
	resp.response.StatusCode = status
	return 0
}

// [method]outgoing-response.body
func (h *TypesHost) MethodOutgoingResponseBody(_ context.Context, self uint32) (uint32, uint32) {
	r, ok := h.resources.Get(self)
	if !ok {
		return 0, 1
	}
	resp, ok := r.(*outgoingResponseResource)
	if !ok {
		return 0, 1
	}

	body := &outgoingBodyResource{
		response: resp.response,
		buffer:   h.responseBuffer,
	}
	handle := h.resources.Add(body)
	return handle, 0
}

// [resource-drop]outgoing-response
func (h *TypesHost) ResourceDropOutgoingResponse(_ context.Context, self uint32) {
	h.resources.Remove(self)
}

// [method]outgoing-body.write
func (h *TypesHost) MethodOutgoingBodyWrite(_ context.Context, self uint32) (uint32, uint32) {
	r, ok := h.resources.Get(self)
	if !ok {
		return 0, 1
	}
	body, ok := r.(*outgoingBodyResource)
	if !ok {
		return 0, 1
	}

	stream := preview2.NewOutputStreamResource(body.buffer)
	handle := h.resources.Add(stream)
	return handle, 0
}

// [static]outgoing-body.finish
func (h *TypesHost) StaticOutgoingBodyFinish(_ context.Context, self uint32, _ bool, _ uint32) uint32 {
	h.resources.Remove(self)
	return 0
}

// [resource-drop]outgoing-body
func (h *TypesHost) ResourceDropOutgoingBody(_ context.Context, self uint32) {
	h.resources.Remove(self)
}

// [static]response-outparam.set
func (h *TypesHost) StaticResponseOutparamSet(_ context.Context, _ uint32, _ bool, _ uint32) {
}

// [resource-drop]response-outparam
func (h *TypesHost) ResourceDropResponseOutparam(_ context.Context, self uint32) {
	h.resources.Remove(self)
}

// [method]incoming-request.method
func (h *TypesHost) MethodIncomingRequestMethod(_ context.Context, _ uint32) string {
	if h.currentRequest == nil || h.currentRequest.Request == nil {
		return "GET"
	}
	return h.currentRequest.Request.Method
}

// [method]incoming-request.path-with-query
func (h *TypesHost) MethodIncomingRequestPathWithQuery(_ context.Context, _ uint32) (string, bool) {
	if h.currentRequest == nil || h.currentRequest.Request == nil {
		return "", false
	}
	uri := h.currentRequest.Request.URL.RequestURI()
	return uri, true
}

// [method]incoming-request.scheme
func (h *TypesHost) MethodIncomingRequestScheme(_ context.Context, _ uint32) (uint8, bool) {
	if h.currentRequest == nil || h.currentRequest.Request == nil {
		return 0, false
	}
	scheme := h.currentRequest.Request.URL.Scheme
	if scheme == "https" {
		return 1, true
	}
	return 0, true
}

// [method]incoming-request.authority
func (h *TypesHost) MethodIncomingRequestAuthority(_ context.Context, _ uint32) (string, bool) {
	if h.currentRequest == nil || h.currentRequest.Request == nil {
		return "", false
	}
	return h.currentRequest.Request.Host, true
}

// [method]incoming-request.headers
func (h *TypesHost) MethodIncomingRequestHeaders(_ context.Context, _ uint32) uint32 {
	fields := preview2.NewFieldsResource()
	if h.currentRequest != nil && h.currentRequest.Request != nil {
		for k, vs := range h.currentRequest.Request.Header {
			fields.Set(k, vs)
		}
	}
	return h.resources.Add(fields)
}

// [method]incoming-request.consume
func (h *TypesHost) MethodIncomingRequestConsume(_ context.Context, _ uint32) (uint32, uint32) {
	body := &incomingBodyResource{}
	if h.currentRequest != nil {
		body.data = h.currentRequest.Body
	}
	handle := h.resources.Add(body)
	return handle, 0
}

// [resource-drop]incoming-request
func (h *TypesHost) ResourceDropIncomingRequest(_ context.Context, self uint32) {
	h.resources.Remove(self)
}

// [method]incoming-body.stream
func (h *TypesHost) MethodIncomingBodyStream(_ context.Context, self uint32) (uint32, uint32) {
	r, ok := h.resources.Get(self)
	if !ok {
		return 0, 1
	}
	body, ok := r.(*incomingBodyResource)
	if !ok {
		return 0, 1
	}

	stream := preview2.NewInputStreamResource(body.data)
	handle := h.resources.Add(stream)
	return handle, 0
}

// [static]incoming-body.finish
func (h *TypesHost) StaticIncomingBodyFinish(_ context.Context, self uint32) uint32 {
	h.resources.Remove(self)
	trailers := &futureTrailersResource{ready: true}
	return h.resources.Add(trailers)
}

// [resource-drop]incoming-body
func (h *TypesHost) ResourceDropIncomingBody(_ context.Context, self uint32) {
	h.resources.Remove(self)
}

// [method]future-trailers.subscribe
func (h *TypesHost) MethodFutureTrailersSubscribe(_ context.Context, _ uint32) uint32 {
	pollable := &preview2.PollableResource{}
	pollable.SetReady(true)
	return h.resources.Add(pollable)
}

// [method]future-trailers.get
func (h *TypesHost) MethodFutureTrailersGet(_ context.Context, _ uint32) (uint32, bool, uint32) {
	return 0, true, 0
}

// [resource-drop]future-trailers
func (h *TypesHost) ResourceDropFutureTrailers(_ context.Context, self uint32) {
	h.resources.Remove(self)
}

// Resource types

type fieldsResource = preview2.FieldsResource

type incomingBodyResource struct {
	data []byte
}

func (b *incomingBodyResource) Type() preview2.ResourceType { return resourceTypeIncomingBody }
func (b *incomingBodyResource) Drop()                       { b.data = nil }

type futureTrailersResource struct {
	ready bool
}

func (f *futureTrailersResource) Type() preview2.ResourceType { return resourceTypeFutureTrailers }
func (f *futureTrailersResource) Drop()                       {}

type outgoingResponseResource struct {
	response *Response
}

func (r *outgoingResponseResource) Type() preview2.ResourceType { return resourceTypeOutgoingResponse }
func (r *outgoingResponseResource) Drop()                       {}

type outgoingBodyResource struct {
	response *Response
	buffer   *bytes.Buffer
}

func (b *outgoingBodyResource) Type() preview2.ResourceType { return resourceTypeOutgoingBody }
func (b *outgoingBodyResource) Drop()                       {}

func (h *TypesHost) Register() map[string]any {
	return map[string]any{
		// Fields
		"[constructor]fields":    h.ConstructorFields,
		"[method]fields.append":  h.MethodFieldsAppend,
		"[method]fields.get":     h.MethodFieldsGet,
		"[method]fields.set":     h.MethodFieldsSet,
		"[method]fields.delete":  h.MethodFieldsDelete,
		"[method]fields.entries": h.MethodFieldsEntries,
		"[method]fields.clone":   h.MethodFieldsClone,
		"[method]fields.has":     h.MethodFieldsHas,
		"[resource-drop]fields":  h.ResourceDropFields,

		// Incoming request
		"[method]incoming-request.method":          h.MethodIncomingRequestMethod,
		"[method]incoming-request.path-with-query": h.MethodIncomingRequestPathWithQuery,
		"[method]incoming-request.scheme":          h.MethodIncomingRequestScheme,
		"[method]incoming-request.authority":       h.MethodIncomingRequestAuthority,
		"[method]incoming-request.headers":         h.MethodIncomingRequestHeaders,
		"[method]incoming-request.consume":         h.MethodIncomingRequestConsume,
		"[resource-drop]incoming-request":          h.ResourceDropIncomingRequest,

		// Incoming body
		"[method]incoming-body.stream": h.MethodIncomingBodyStream,
		"[static]incoming-body.finish": h.StaticIncomingBodyFinish,
		"[resource-drop]incoming-body": h.ResourceDropIncomingBody,

		// Future trailers
		"[method]future-trailers.subscribe": h.MethodFutureTrailersSubscribe,
		"[method]future-trailers.get":       h.MethodFutureTrailersGet,
		"[resource-drop]future-trailers":    h.ResourceDropFutureTrailers,

		// Outgoing response
		"[constructor]outgoing-response":            h.ConstructorOutgoingResponse,
		"[method]outgoing-response.set-status-code": h.MethodOutgoingResponseSetStatusCode,
		"[method]outgoing-response.body":            h.MethodOutgoingResponseBody,
		"[resource-drop]outgoing-response":          h.ResourceDropOutgoingResponse,

		// Outgoing body
		"[method]outgoing-body.write":  h.MethodOutgoingBodyWrite,
		"[static]outgoing-body.finish": h.StaticOutgoingBodyFinish,
		"[resource-drop]outgoing-body": h.ResourceDropOutgoingBody,

		// Response outparam
		"[static]response-outparam.set":    h.StaticResponseOutparamSet,
		"[resource-drop]response-outparam": h.ResourceDropResponseOutparam,
	}
}
