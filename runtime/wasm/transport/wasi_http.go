// SPDX-License-Identifier: MPL-2.0

package transport

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/wippyai/runtime/api/payload"
	httpapi "github.com/wippyai/runtime/api/service/http"
)

var (
	errWASIHTTPRequestContextNotFound = errors.New("http request context not found")
	errWASIHTTPNilRequest             = errors.New("http request is nil")
	errWASIHTTPNilResponseWriter      = errors.New("http response writer is nil")
)

// WASIHTTPTransport maps HTTP request/response context to wasm args/results.
type WASIHTTPTransport struct{}

// NewWASIHTTPTransport creates wasi-http transport.
func NewWASIHTTPTransport() *WASIHTTPTransport {
	return &WASIHTTPTransport{}
}

// Prepare builds call args from payload input, or falls back to HTTP request body.
func (t *WASIHTTPTransport) Prepare(ctx context.Context, input payload.Payloads) ([]any, error) {
	if len(input) > 0 {
		return PayloadsToArgs(ctx, input)
	}

	reqCtx, ok := httpapi.GetRequestContext(ctx)
	if !ok || reqCtx == nil {
		return nil, errWASIHTTPRequestContextNotFound
	}
	req := reqCtx.Request()
	if req == nil {
		return nil, errWASIHTTPNilRequest
	}
	if req.Body == nil {
		return nil, nil
	}

	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	if len(body) == 0 {
		return nil, nil
	}

	return []any{string(body)}, nil
}

// EncodeResult writes wasm result into HTTP response and marks it handled.
func (t *WASIHTTPTransport) EncodeResult(ctx context.Context, result any) (payload.Payload, error) {
	reqCtx, ok := httpapi.GetRequestContext(ctx)
	if !ok || reqCtx == nil {
		return nil, errWASIHTTPRequestContextNotFound
	}
	if reqCtx.ResponseHandled() {
		return nil, nil
	}

	w := reqCtx.ResponseWriter()
	if w == nil {
		return nil, errWASIHTTPNilResponseWriter
	}

	switch v := result.(type) {
	case nil:
		w.WriteHeader(http.StatusNoContent)
	case []byte:
		if w.Header().Get("Content-Type") == "" {
			w.Header().Set("Content-Type", "application/octet-stream")
		}
		if _, err := w.Write(v); err != nil {
			return nil, err
		}
	case string:
		if w.Header().Get("Content-Type") == "" {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		}
		if _, err := io.WriteString(w, v); err != nil {
			return nil, err
		}
	default:
		if w.Header().Get("Content-Type") == "" {
			w.Header().Set("Content-Type", "application/json")
		}
		data, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		if _, err := w.Write(data); err != nil {
			return nil, err
		}
	}

	reqCtx.MarkHandled()
	return nil, nil
}
