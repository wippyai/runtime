package http

import (
	"context"
	"errors"
	"github.com/ponyruntime/pony/api/runtime"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ponyruntime/pony/api/payload"
	config "github.com/ponyruntime/pony/api/service/http"
	"go.uber.org/zap"
)

// MockExecutor is a simple mock implementation of runtime.FuncRegistry
type MockExecutor struct {
	executeFunc func(runtime.Task) (chan *runtime.Result, error)
}

func (m *MockExecutor) Execute(task runtime.Task) (chan *runtime.Result, error) {
	if m.executeFunc != nil {
		return m.executeFunc(task)
	}
	return nil, errors.New("no execute function set")
}

// MockTranscoder is a simple mock implementation of payload.Transcoder
type MockTranscoder struct {
	transcodeFunc func(payload.Payload, payload.Format) (payload.Payload, error)
	unmarshalFunc func(payload.Payload, interface{}) error
}

func (m *MockTranscoder) Transcode(p payload.Payload, format payload.Format) (payload.Payload, error) {
	if m.transcodeFunc != nil {
		return m.transcodeFunc(p, format)
	}
	return nil, errors.New("no transcode function set")
}

func (m *MockTranscoder) Unmarshal(p payload.Payload, v interface{}) error {
	if m.unmarshalFunc != nil {
		return m.unmarshalFunc(p, v)
	}
	return errors.New("no unmarshal function set")
}

func TestEndpointHandler_Handle_ExecutorError(t *testing.T) {
	// Setup
	executor := &MockExecutor{}
	transcoder := &MockTranscoder{}
	logger := zap.NewNop()

	handler := NewEndpointHandler(executor, transcoder, logger)

	// Create test request
	req := httptest.NewRequest("GET", "/test", nil)

	// Setup route info with correct context key
	routeInfo := &config.RouteInfo{
		EndpointID: "test-endpoint",
		Endpoint:   config.EndpointConfig{},
	}
	ctx := context.WithValue(req.Context(), config.RouteCtx, routeInfo)
	req = req.WithContext(ctx)

	// Setup response recorder
	w := httptest.NewRecorder()

	// Setup executor error
	expectedError := "executor error"
	executor.executeFunc = func(runtime.Task) (chan *runtime.Result, error) {
		return nil, errors.New(expectedError)
	}

	// Run
	handler.Handle(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status code %d, got %d", http.StatusInternalServerError, w.Code)
	}

	if !strings.Contains(w.Body.String(), expectedError) {
		t.Errorf("expected error message containing %q, got %q", expectedError, w.Body.String())
	}
}

func TestEndpointHandler_Handle_ContextCancellation(t *testing.T) {
	// Setup
	executor := &MockExecutor{}
	transcoder := &MockTranscoder{}
	logger := zap.NewNop()

	handler := NewEndpointHandler(executor, transcoder, logger)

	// Create test request with canceled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	req := httptest.NewRequest("GET", "/test", nil)

	// Setup route info with a correct context key
	routeInfo := &config.RouteInfo{
		EndpointID: "test-endpoint",
		Endpoint:   config.EndpointConfig{},
	}
	ctx = context.WithValue(ctx, config.RouteCtx, routeInfo)
	req = req.WithContext(ctx)

	// Setup response recorder
	w := httptest.NewRecorder()

	// Setup executor response that will never complete due to cancellation
	executor.executeFunc = func(runtime.Task) (chan *runtime.Result, error) {
		return make(chan *runtime.Result), nil
	}

	// Run
	handler.Handle(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status code %d, got %d", http.StatusInternalServerError, w.Code)
	}

	if !strings.Contains(w.Body.String(), "request canceled") {
		t.Errorf("expected error message containing 'request cancelled', got %q", w.Body.String())
	}
}

func TestEndpointHandler_Handle_MissingRouteInfo(t *testing.T) {
	// Setup
	executor := &MockExecutor{}
	transcoder := &MockTranscoder{}
	logger := zap.NewNop()

	handler := NewEndpointHandler(executor, transcoder, logger)

	// Create test request without route info in context
	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()

	// Run
	handler.Handle(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status code %d, got %d", http.StatusInternalServerError, w.Code)
	}

	if !strings.Contains(w.Body.String(), "route info not found") {
		t.Errorf("expected error message containing 'route info not found', got %q", w.Body.String())
	}
}

func TestEndpointHandler_Handle_SuccessfulResponse(t *testing.T) {
	// Setup
	executor := &MockExecutor{}
	transcoder := &MockTranscoder{}
	logger := zap.NewNop()

	handler := NewEndpointHandler(executor, transcoder, logger)

	// Create test request
	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()

	// Setup route info
	routeInfo := &config.RouteInfo{
		EndpointID: "test-endpoint",
		Endpoint: config.EndpointConfig{
			Target: "test.function",
		},
	}
	ctx := context.WithValue(req.Context(), config.RouteCtx, routeInfo)
	req = req.WithContext(ctx)

	// Setup successful executor response
	resultCh := make(chan *runtime.Result, 1)
	resultCh <- &runtime.Result{} // Empty result since we're using context for response
	close(resultCh)

	executor.executeFunc = func(task runtime.Task) (chan *runtime.Result, error) {
		// Verify the RequestContext was properly set
		rCtx, ok := task.Context.Value(config.RequestCtx).(*config.RequestContext)
		if !ok {
			t.Error("RequestContext not set in task context")
		}

		// Simulate successful response through context wrapper
		_, _ = rCtx.ResponseWriter().Write([]byte("test response"))
		return resultCh, nil
	}

	// Run
	handler.Handle(w, req)

	// Verify response
	if w.Code != http.StatusOK {
		t.Errorf("expected status code %d, got %d", http.StatusOK, w.Code)
	}

	if w.Body.String() != "test response" {
		t.Errorf("expected body %q, got %q", "test response", w.Body.String())
	}
}
