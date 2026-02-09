package transport

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	httpapi "github.com/wippyai/runtime/api/service/http"
)

func TestWASIHTTPTransportPrepareFromRequestBody(t *testing.T) {
	ctx, fc := ctxapi.OpenFrameContext(context.Background())
	defer func() { _ = fc.Close() }()

	req := httptest.NewRequest("POST", "/test/wasm/greet", strings.NewReader("WebAssembly"))
	rec := httptest.NewRecorder()
	rctx := httpapi.NewRequestContext(req, rec)
	if err := fc.Set(httpapi.RequestKey(), rctx); err != nil {
		t.Fatalf("fc.Set() error = %v", err)
	}

	tr := NewWASIHTTPTransport()
	args, err := tr.Prepare(ctx, nil)
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	if len(args) != 1 {
		t.Fatalf("Prepare() args len = %d, want 1", len(args))
	}
	if got, ok := args[0].(string); !ok || got != "WebAssembly" {
		t.Fatalf("Prepare() arg[0] = %#v, want %q", args[0], "WebAssembly")
	}
}

func TestWASIHTTPTransportPrepareFromInputPayloads(t *testing.T) {
	tr := NewWASIHTTPTransport()
	args, err := tr.Prepare(context.Background(), payload.Payloads{payload.New("x")})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	if len(args) != 1 {
		t.Fatalf("Prepare() args len = %d, want 1", len(args))
	}
	if got, ok := args[0].(string); !ok || got != "x" {
		t.Fatalf("Prepare() arg[0] = %#v, want %q", args[0], "x")
	}
}

func TestWASIHTTPTransportPrepareMissingRequestContext(t *testing.T) {
	tr := NewWASIHTTPTransport()
	if _, err := tr.Prepare(context.Background(), nil); err == nil {
		t.Fatal("Prepare() expected error without request context")
	}
}

func TestWASIHTTPTransportEncodeResultString(t *testing.T) {
	ctx, fc := ctxapi.OpenFrameContext(context.Background())
	defer func() { _ = fc.Close() }()

	req := httptest.NewRequest("POST", "/test/wasm/greet", nil)
	rec := httptest.NewRecorder()
	rctx := httpapi.NewRequestContext(req, rec)
	if err := fc.Set(httpapi.RequestKey(), rctx); err != nil {
		t.Fatalf("fc.Set() error = %v", err)
	}

	tr := NewWASIHTTPTransport()
	if _, err := tr.EncodeResult(ctx, "Hello, WebAssembly!"); err != nil {
		t.Fatalf("EncodeResult() error = %v", err)
	}

	if !rctx.ResponseHandled() {
		t.Fatal("EncodeResult() should mark response handled")
	}
	if got := rec.Code; got != 200 {
		t.Fatalf("response status = %d, want 200", got)
	}
	if got := rec.Body.String(); got != "Hello, WebAssembly!" {
		t.Fatalf("response body = %q, want %q", got, "Hello, WebAssembly!")
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/plain") {
		t.Fatalf("content-type = %q, want text/plain", got)
	}
}

func TestWASIHTTPTransportEncodeResultJSON(t *testing.T) {
	ctx, fc := ctxapi.OpenFrameContext(context.Background())
	defer func() { _ = fc.Close() }()

	req := httptest.NewRequest("GET", "/json", nil)
	rec := httptest.NewRecorder()
	rctx := httpapi.NewRequestContext(req, rec)
	if err := fc.Set(httpapi.RequestKey(), rctx); err != nil {
		t.Fatalf("fc.Set() error = %v", err)
	}

	tr := NewWASIHTTPTransport()
	if _, err := tr.EncodeResult(ctx, map[string]any{"ok": true}); err != nil {
		t.Fatalf("EncodeResult() error = %v", err)
	}

	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("content-type = %q, want application/json", got)
	}
	if got := rec.Body.String(); got != `{"ok":true}` {
		t.Fatalf("response body = %q, want %q", got, `{"ok":true}`)
	}
}
