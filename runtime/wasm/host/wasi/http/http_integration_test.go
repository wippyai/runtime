package http

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/dispatcher"
	httpapi "github.com/wippyai/runtime/api/dispatcher/http"
	"github.com/wippyai/runtime/api/registry"
	wasmapi "github.com/wippyai/runtime/api/runtime/wasm"
	"github.com/wippyai/runtime/api/security"
	httpservice "github.com/wippyai/runtime/api/service/http"
	"github.com/wippyai/runtime/runtime/wasm/engine"
	"github.com/wippyai/runtime/runtime/wasm/host"
	"github.com/wippyai/runtime/runtime/wasm/resource"
	wasmrt "github.com/wippyai/wasm-runtime/runtime"
)

// WAT module that imports WASI HTTP outgoing handler and makes a request.
// This simulates a Rust/Go WASM component making an outgoing HTTP request.
const wasiHTTPOutgoingWAT = `(module
  ;; Import WASI HTTP outgoing handler
  (import "wasi:http/outgoing-handler@0.2.0" "handle"
    (func $http_handle (param i32) (result i32)))

  (memory (export "memory") 1)

  ;; Make HTTP request function
  (func $make_request (export "make_request") (param $req_handle i32) (result i32)
    (call $http_handle (local.get $req_handle))
  )

  ;; Standard CABI exports
  (func $cabi_realloc (export "cabi_realloc")
    (param $old_ptr i32) (param $old_size i32) (param $align i32) (param $new_size i32)
    (result i32)
    i32.const 1024
  )
)`

// allowAllScope allows all security checks
type allowAllScope struct{}

func (s *allowAllScope) With(_ security.Policy) security.Scope { return s }
func (s *allowAllScope) Without(_ registry.ID) security.Scope  { return s }
func (s *allowAllScope) Contains(_ registry.ID) bool           { return false }
func (s *allowAllScope) Policies() []security.Policy           { return nil }
func (s *allowAllScope) Evaluate(_ security.Actor, _, _ string, _ registry.Metadata) security.Result {
	return security.Allow
}

// mockDispatcher intercepts commands and returns mock responses.
type mockDispatcher struct {
	handlers map[dispatcher.CommandID]dispatcher.Handler
}

func newMockDispatcher() *mockDispatcher {
	return &mockDispatcher{
		handlers: make(map[dispatcher.CommandID]dispatcher.Handler),
	}
}

func (d *mockDispatcher) Register(id dispatcher.CommandID, h dispatcher.Handler) {
	d.handlers[id] = h
}

func (d *mockDispatcher) Dispatch(cmd dispatcher.Command) dispatcher.Handler {
	return d.handlers[cmd.CmdID()]
}

// mockHTTPHandler returns a mock HTTP response without making real requests.
type mockHTTPHandler struct {
	statusCode int
	body       []byte
	headers    map[string]string
	called     bool
	lastCmd    *httpapi.RequestCmd
}

func newMockHTTPHandler(statusCode int, body []byte) *mockHTTPHandler {
	return &mockHTTPHandler{
		statusCode: statusCode,
		body:       body,
		headers:    map[string]string{"Content-Type": "application/json"},
	}
}

func (h *mockHTTPHandler) Handle(ctx context.Context, cmd dispatcher.Command, emit dispatcher.Emitter) error {
	h.called = true
	h.lastCmd = cmd.(*httpapi.RequestCmd)

	emit(httpapi.Response{
		StatusCode: h.statusCode,
		Headers:    h.headers,
		Body:       h.body,
		URL:        h.lastCmd.URL,
	})
	return nil
}

// TestHTTPOutgoingWithScheduler tests WASI HTTP outgoing with scheduler and mock dispatcher.
func TestHTTPOutgoingWithScheduler(t *testing.T) {
	ctx := context.Background()

	// Create wazero runtime
	wazeroRt := wazero.NewRuntime(ctx)
	defer wazeroRt.Close(ctx)

	// Create shared resources
	resources := resource.NewInstanceResources()
	defer resources.Close()

	outgoingHost := NewOutgoingHost(resources)

	// Pre-populate an outgoing request in the resource table
	reqHandle := outgoingHost.OutgoingRequests().Insert(&OutgoingRequest{
		Method:  "POST",
		URL:     "https://api.example.com/users",
		Headers: map[string]string{"Content-Type": "application/json"},
		Body:    []byte(`{"name":"test"}`),
		Timeout: 5 * time.Second,
	})

	// Create mock dispatcher with mock HTTP handler
	mockHandler := newMockHTTPHandler(201, []byte(`{"id":123,"name":"test"}`))
	disp := newMockDispatcher()
	disp.Register(httpapi.CmdRequest, mockHandler)

	// Register the HTTP handler using MakeAsyncHandler
	httpHandler := host.MakeAsyncHandler(outgoingHost.makeHandleCmd)

	_, err := wazeroRt.NewHostModuleBuilder(OutgoingNamespace).
		NewFunctionBuilder().
		WithGoModuleFunction(httpHandler, []api.ValueType{api.ValueTypeI32}, []api.ValueType{api.ValueTypeI32}).
		Export("handle").
		Instantiate(ctx)
	require.NoError(t, err, "Failed to register HTTP host")

	// Compile WAT
	wasmBytes, err := wasmrt.CompileWAT(wasiHTTPOutgoingWAT)
	require.NoError(t, err, "Failed to compile WAT")

	// Apply asyncify transform
	asyncifiedBytes, err := engine.CompileWithAsyncify(wasmBytes, []string{
		OutgoingNamespace + ".handle",
	})
	require.NoError(t, err, "Asyncify transform failed")

	// Compile and instantiate
	compiled, err := wazeroRt.CompileModule(ctx, asyncifiedBytes)
	require.NoError(t, err, "Compile failed")

	inst, err := wazeroRt.InstantiateModule(ctx, compiled, wazero.NewModuleConfig())
	require.NoError(t, err, "Instantiate failed")
	defer inst.Close(ctx)

	// Initialize asyncify and scheduler
	asyncify, err := engine.InitAsyncify(inst)
	require.NoError(t, err, "InitAsyncify failed")

	scheduler := engine.NewScheduler(asyncify)

	// Setup context with frame, security, and async frame
	ctx = ctxapi.NewRootContext()
	ctx, _ = ctxapi.OpenFrameContext(ctx)
	security.SetActor(ctx, security.Actor{ID: "test-user"})
	security.SetScope(ctx, &allowAllScope{})

	// Set async frame with scheduler and asyncify
	asyncFrame := &wasmapi.AsyncFrame{
		Scheduler: scheduler,
		Asyncify:  asyncify,
	}
	wasmapi.SetAsyncFrame(ctx, asyncFrame)

	// Get the make_request function
	makeRequestFn := inst.ExportedFunction("make_request")
	require.NotNil(t, makeRequestFn, "make_request function not found")

	// Execute with request handle
	err = scheduler.Execute(ctx, makeRequestFn, uint64(reqHandle))
	require.NoError(t, err)

	// Run scheduler loop with mock dispatcher
	var yieldResult *engine.YieldResult
	var capturedResponse *httpapi.Response
	stepCount := 0

	for {
		result, err := scheduler.Step(ctx, yieldResult)
		if yieldResult != nil {
			engine.ReleaseYieldResult(yieldResult)
			yieldResult = nil
		}
		require.NoError(t, err)
		stepCount++

		if result.Status == engine.SchedulerDone {
			break
		}

		if result.Status == engine.SchedulerContinue {
			// Get the yielded command
			cmd := result.Command
			require.NotNil(t, cmd, "Expected command from scheduler")
			t.Logf("Scheduler yielded command: %T (ID: %d)", cmd, cmd.CmdID())

			// Dispatch to mock handler (like real dispatcher would)
			handler := disp.Dispatch(cmd)
			require.NotNil(t, handler, "No handler for command ID %d", cmd.CmdID())

			// Execute handler with emit callback
			var emittedResponse httpapi.Response
			emit := func(result any) {
				emittedResponse = result.(httpapi.Response)
				capturedResponse = &emittedResponse
			}

			err := handler.Handle(ctx, cmd, emit)
			require.NoError(t, err)

			t.Logf("Mock handler returned: status=%d, body=%s",
				emittedResponse.StatusCode, string(emittedResponse.Body))

			// Resume WASM with response status code
			yieldResult = engine.AcquireYieldResult()
			yieldResult.Value = uint64(emittedResponse.StatusCode)
		}
	}

	t.Logf("Completed in %d scheduler steps", stepCount)

	// Verify the mock handler was called with correct request
	require.True(t, mockHandler.called, "Mock HTTP handler should have been called")
	require.Equal(t, "POST", mockHandler.lastCmd.Method)
	require.Equal(t, "https://api.example.com/users", mockHandler.lastCmd.URL)
	require.Equal(t, "application/json", mockHandler.lastCmd.Headers["Content-Type"])
	require.Equal(t, []byte(`{"name":"test"}`), mockHandler.lastCmd.Body)

	// Verify response was captured
	require.NotNil(t, capturedResponse, "Should have captured response")
	require.Equal(t, 201, capturedResponse.StatusCode)
	require.Equal(t, `{"id":123,"name":"test"}`, string(capturedResponse.Body))
}

// TestHTTPIncomingWithRequestContext tests WASI HTTP incoming host directly.
func TestHTTPIncomingWithRequestContext(t *testing.T) {
	// Create shared resources
	resources := resource.NewInstanceResources()
	defer resources.Close()

	incomingHost := NewIncomingHost(resources)

	// Create simulated HTTP request
	httpReq := httptest.NewRequest("POST", "/api/users?name=test", strings.NewReader(`{"action":"create"}`))
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-Request-ID", "test-123")

	// Create response recorder
	recorder := httptest.NewRecorder()

	// Setup request handles
	reqHandle, bodyHandle := incomingHost.SetupRequest(httpReq)
	t.Logf("Request handle: %d, Body handle: %d", reqHandle, bodyHandle)
	require.NotZero(t, reqHandle)
	require.NotZero(t, bodyHandle)

	// Setup context with frame and HTTP request context
	ctx := ctxapi.NewRootContext()
	ctx, fc := ctxapi.OpenFrameContext(ctx)

	reqCtx := httpservice.NewRequestContext(httpReq, recorder)
	fc.Set(httpservice.RequestCtx, reqCtx)

	// Simulate what WASM would do: create response -> set status -> get body -> finish
	// 1. Create outgoing response
	stack := []uint64{0} // no initial headers
	incomingHost.newOutgoingResponse(ctx, nil, stack)
	respHandle := resource.Handle(stack[0])
	t.Logf("Response handle: %d", respHandle)
	require.NotZero(t, respHandle)

	// 2. Set status code 201
	stack = []uint64{uint64(respHandle), 201}
	incomingHost.setStatusCode(ctx, nil, stack)

	// 3. Get response body
	stack = []uint64{uint64(respHandle)}
	incomingHost.responseBody(ctx, nil, stack)
	bodyRespHandle := resource.Handle(stack[0])
	t.Logf("Body handle: %d", bodyRespHandle)
	require.NotZero(t, bodyRespHandle)

	// 4. Finish body (writes to ResponseWriter)
	stack = []uint64{uint64(bodyRespHandle)}
	incomingHost.bodyFinish(ctx, nil, stack)

	// Verify response was written
	require.True(t, reqCtx.ResponseHandled(), "Response should be marked as handled")
	require.Equal(t, 201, recorder.Code)
}

// TestHTTPResourceCleanupOnContextClose tests resources are cleaned on context close.
func TestHTTPResourceCleanupOnContextClose(t *testing.T) {
	resources := resource.NewInstanceResources()

	// Setup HTTP hosts
	incomingHost := NewIncomingHost(resources)
	outgoingHost := NewOutgoingHost(resources)

	// Create some resources
	req := httptest.NewRequest("GET", "/", nil)
	reqHandle, bodyHandle := incomingHost.SetupRequest(req)

	outReqHandle := outgoingHost.OutgoingRequests().Insert(&OutgoingRequest{
		Method:  "POST",
		URL:     "https://example.com/api",
		Headers: map[string]string{"Content-Type": "application/json"},
		Body:    []byte(`{"data":"test"}`),
	})

	respHandle := outgoingHost.Responses().Insert(&IncomingResponse{
		Status:  200,
		Headers: map[string]string{"X-Response-ID": "resp-123"},
		Body:    []byte(`{"result":"success"}`),
	})

	t.Logf("Created handles: incoming req=%d, body=%d, outgoing req=%d, resp=%d",
		reqHandle, bodyHandle, outReqHandle, respHandle)

	require.Equal(t, 4, resources.Len(), "Should have 4 resources")

	// Get references before close
	inReq, _ := incomingHost.incomingRequests.Get(reqHandle)
	inBody, _ := incomingHost.incomingBodies.Get(bodyHandle)
	outReq, _ := outgoingHost.OutgoingRequests().Get(outReqHandle)
	inResp, _ := outgoingHost.Responses().Get(respHandle)

	// Close resources
	resources.Close()

	// Verify all Drop() methods were called
	require.Nil(t, inReq.Request, "incoming request.Request should be nil")
	require.Nil(t, inBody.Request, "incoming body.Request should be nil")
	require.Nil(t, inBody.Data, "incoming body.Data should be nil")
	require.Nil(t, outReq.Headers, "outgoing request.Headers should be nil")
	require.Nil(t, outReq.Body, "outgoing request.Body should be nil")
	require.Nil(t, inResp.Headers, "incoming response.Headers should be nil")
	require.Nil(t, inResp.Body, "incoming response.Body should be nil")

	require.Equal(t, 0, resources.Len(), "All resources should be cleaned")
}

// TestHTTPWithAsyncFrameResources tests resource cleanup through AsyncFrame.
func TestHTTPWithAsyncFrameResources(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	ctx, _ = ctxapi.OpenFrameContext(ctx)

	// Create resources and register with async frame
	resources := resource.NewInstanceResources()
	ctx = wasmapi.WithResources(ctx, resources)

	// Create HTTP hosts and some resources
	incomingHost := NewIncomingHost(resources)
	outgoingHost := NewOutgoingHost(resources)

	req := httptest.NewRequest("DELETE", "/items/123", nil)
	incomingHost.SetupRequest(req)

	outgoingHost.OutgoingRequests().Insert(&OutgoingRequest{
		Method: "DELETE",
		URL:    "https://api.example.com/items/123",
	})

	require.Equal(t, 3, resources.Len())

	// Close through async frame
	err := wasmapi.CloseResources(ctx)
	require.NoError(t, err)

	// Resources should be nil in frame
	require.Nil(t, wasmapi.GetResources(ctx))
	require.Equal(t, 0, resources.Len())
}

// wrapHostFunc wraps host functions for wazero.
func wrapHostFunc(fn any) api.GoModuleFunc {
	switch f := fn.(type) {
	case func(ctx context.Context, mod api.Module, stack []uint64):
		return f
	case api.GoModuleFunc:
		return f
	default:
		return func(ctx context.Context, mod api.Module, stack []uint64) {}
	}
}
