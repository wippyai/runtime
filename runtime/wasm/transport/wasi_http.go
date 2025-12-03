package transport

import (
	"context"
	"fmt"
	"net/http"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/resource"
	api "github.com/wippyai/runtime/api/runtime/wasm"
	httpservice "github.com/wippyai/runtime/api/service/http"
)

// Resource type IDs for WASI HTTP.
// These must match the types used by wasi/http hosts.
const (
	TypeHTTPIncomingRequest  uint32 = 110
	TypeHTTPIncomingBody     uint32 = 111
	TypeHTTPResponseOutparam uint32 = 115
)

// HTTPIncomingRequest wraps *http.Request for resource table.
type HTTPIncomingRequest struct {
	Request *http.Request
}

func (r *HTTPIncomingRequest) Drop() {
	if r.Request != nil && r.Request.Body != nil {
		r.Request.Body.Close()
	}
	r.Request = nil
}

// HTTPIncomingBody wraps request body for resource table.
type HTTPIncomingBody struct {
	Request *http.Request
}

func (b *HTTPIncomingBody) Drop() {
	if b.Request != nil && b.Request.Body != nil {
		b.Request.Body.Close()
	}
	b.Request = nil
}

// HTTPResponseOutparam holds the response outparam slot.
type HTTPResponseOutparam struct {
	Writer  http.ResponseWriter
	Request *http.Request
}

func (r *HTTPResponseOutparam) Drop() {
	r.Writer = nil
	r.Request = nil
}

// WASIHTTPTransport implements Transport for WASI HTTP incoming-handler.
// Stateless - extracts HTTP request from context and creates handles.
type WASIHTTPTransport struct{}

// NewWASIHTTPTransport creates a new WASI HTTP transport.
func NewWASIHTTPTransport() *WASIHTTPTransport {
	return &WASIHTTPTransport{}
}

func (t *WASIHTTPTransport) Name() string {
	return api.TransportWASIHTTP
}

// Prepare extracts *http.Request from context and creates WASI HTTP handles.
// Returns [request_handle, response_outparam] as per wasi:http/incoming-handler.
func (t *WASIHTTPTransport) Prepare(ctx context.Context, store *resource.Store, input payload.Payloads, args []uint64) ([]uint64, error) {
	fmt.Printf("DEBUG WASIHTTPTransport.Prepare: store=%v, input=%d payloads\n", store != nil, len(input))

	reqCtx, ok := httpservice.GetRequestContext(ctx)
	if !ok {
		fmt.Printf("DEBUG WASIHTTPTransport.Prepare: no HTTP request context\n")
		return args, ErrNoHTTPRequest
	}

	req := reqCtx.Request()
	if req == nil {
		fmt.Printf("DEBUG WASIHTTPTransport.Prepare: request is nil\n")
		return args, ErrNoHTTPRequest
	}

	fmt.Printf("DEBUG WASIHTTPTransport.Prepare: request=%s %s\n", req.Method, req.URL.Path)

	table := store.Table()

	// Create incoming-request handle
	reqHandle := table.Insert(TypeHTTPIncomingRequest, &HTTPIncomingRequest{Request: req})
	fmt.Printf("DEBUG WASIHTTPTransport.Prepare: created reqHandle=%d (type=%d)\n", reqHandle, TypeHTTPIncomingRequest)

	// Create response-outparam handle
	respHandle := table.Insert(TypeHTTPResponseOutparam, &HTTPResponseOutparam{
		Writer:  reqCtx.ResponseWriter(),
		Request: req,
	})
	fmt.Printf("DEBUG WASIHTTPTransport.Prepare: created respHandle=%d (type=%d)\n", respHandle, TypeHTTPResponseOutparam)

	result := append(args, uint64(reqHandle), uint64(respHandle))
	fmt.Printf("DEBUG WASIHTTPTransport.Prepare: returning args=%v\n", result)

	return result, nil
}

// Compile-time check
var _ api.Transport = (*WASIHTTPTransport)(nil)
