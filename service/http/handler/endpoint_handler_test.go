package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/runtime"
	config "github.com/ponyruntime/pony/api/service/http"
	"go.uber.org/zap"
)

// MockExecutor is a simple mock implementation of runtime.Executor
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

func TestEndpointHandler_Handle_SuccessfulJsonRequest(t *testing.T) {
	// Setup
	executor := &MockExecutor{}
	transcoder := &MockTranscoder{}
	logger := zap.NewNop()

	handler := NewEndpointHandler(executor, transcoder, logger)

	// Create test request
	reqBody := []byte(`{"test": "data"}`)
	req := httptest.NewRequest("POST", "/test", bytes.NewReader(reqBody))

	// Setup route info with correct context key
	routeInfo := &config.RouteInfo{
		EndpointID: "test-endpoint",
		Endpoint: config.EndpointConfig{
			JSONInput:  true,
			JSONOutput: true,
		},
	}
	ctx := context.WithValue(req.Context(), config.RouteCtx, routeInfo)
	req = req.WithContext(ctx)

	// Setup response recorder
	w := httptest.NewRecorder()

	// Setup executor response
	resultChan := make(chan *runtime.Result, 1)
	expectedResponse := []byte(`{"result":"success"}`)
	resultChan <- &runtime.Result{
		Payload: payload.NewPayload(expectedResponse, payload.JSON),
	}
	close(resultChan)

	executor.executeFunc = func(task runtime.Task) (chan *runtime.Result, error) {
		return resultChan, nil
	}

	transcoder.transcodeFunc = func(p payload.Payload, format payload.Format) (payload.Payload, error) {
		return payload.NewPayload(expectedResponse, payload.JSON), nil
	}

	// Run
	handler.Handle(w, req)

	// Assert
	if w.Code != http.StatusOK {
		t.Errorf("expected status code %d, got %d", http.StatusOK, w.Code)
	}

	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected Content-type %s, got %s", "application/json", ct)
	}

	if !json.Valid(w.Body.Bytes()) {
		t.Error("response is not valid JSON")
	}

	if string(w.Body.Bytes()) != string(expectedResponse) {
		t.Errorf("expected response %s, got %s", string(expectedResponse), w.Body.String())
	}
}

func TestEndpointHandler_Handle_ValidationError(t *testing.T) {
	// Setup
	executor := &MockExecutor{}
	transcoder := &MockTranscoder{}
	logger := zap.NewNop()

	handler := NewEndpointHandler(executor, transcoder, logger)

	// Create test request with invalid JSON
	reqBody := []byte(`{invalid json}`)
	req := httptest.NewRequest("POST", "/test", bytes.NewReader(reqBody))

	// Setup route info with correct context key
	routeInfo := &config.RouteInfo{
		EndpointID: "test-endpoint",
		Endpoint: config.EndpointConfig{
			JSONInput: true,
			JSONSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"test": map[string]interface{}{
						"type": "string",
					},
				},
			},
		},
	}
	ctx := context.WithValue(req.Context(), config.RouteCtx, routeInfo)
	req = req.WithContext(ctx)

	// Setup response recorder
	w := httptest.NewRecorder()

	// Run
	handler.Handle(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status code %d, got %d", http.StatusBadRequest, w.Code)
	}
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
	executor.executeFunc = func(task runtime.Task) (chan *runtime.Result, error) {
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

func TestEndpointHandler_Handle_RawResponse(t *testing.T) {
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
		Endpoint: config.EndpointConfig{
			JSONOutput: false,
		},
	}
	ctx := context.WithValue(req.Context(), config.RouteCtx, routeInfo)
	req = req.WithContext(ctx)

	// Setup response recorder
	w := httptest.NewRecorder()

	// Setup executor response
	resultChan := make(chan *runtime.Result, 1)
	expectedResponse := []byte("raw data")
	resultChan <- &runtime.Result{
		Payload: payload.NewPayload(expectedResponse, payload.Bytes),
	}
	close(resultChan)

	executor.executeFunc = func(task runtime.Task) (chan *runtime.Result, error) {
		return resultChan, nil
	}

	// Run
	handler.Handle(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status code %d, got %d", http.StatusOK, w.Code)
	}

	if w.Body.String() != string(expectedResponse) {
		t.Errorf("expected response %q, got %q", string(expectedResponse), w.Body.String())
	}
}

func TestEndpointHandler_Handle_ContextCancellation(t *testing.T) {
	// Setup
	executor := &MockExecutor{}
	transcoder := &MockTranscoder{}
	logger := zap.NewNop()

	handler := NewEndpointHandler(executor, transcoder, logger)

	// Create test request with cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	req := httptest.NewRequest("GET", "/test", nil)

	// Setup route info with correct context key
	routeInfo := &config.RouteInfo{
		EndpointID: "test-endpoint",
		Endpoint:   config.EndpointConfig{},
	}
	ctx = context.WithValue(ctx, config.RouteCtx, routeInfo)
	req = req.WithContext(ctx)

	// Setup response recorder
	w := httptest.NewRecorder()

	// Setup executor response that will never complete due to cancellation
	executor.executeFunc = func(task runtime.Task) (chan *runtime.Result, error) {
		return make(chan *runtime.Result), nil
	}

	// Run
	handler.Handle(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status code %d, got %d", http.StatusInternalServerError, w.Code)
	}

	if !strings.Contains(w.Body.String(), "request cancelled") {
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

func TestEndpointHandler_Handle_CustomSuccessStatusCode(t *testing.T) {
	// Setup
	executor := &MockExecutor{}
	transcoder := &MockTranscoder{}
	logger := zap.NewNop()

	handler := NewEndpointHandler(executor, transcoder, logger)

	// Create test request
	req := httptest.NewRequest("POST", "/test", nil)

	// Setup route info with custom success status code
	routeInfo := &config.RouteInfo{
		EndpointID: "test-endpoint",
		Endpoint: config.EndpointConfig{
			SuccessStatusCode: http.StatusCreated,
		},
	}
	ctx := context.WithValue(req.Context(), config.RouteCtx, routeInfo)
	req = req.WithContext(ctx)

	// Setup response recorder
	w := httptest.NewRecorder()

	// Setup executor response
	resultChan := make(chan *runtime.Result, 1)
	resultChan <- &runtime.Result{
		Payload: payload.NewPayload([]byte("success"), payload.Bytes),
	}
	close(resultChan)

	executor.executeFunc = func(task runtime.Task) (chan *runtime.Result, error) {
		return resultChan, nil
	}

	// Run
	handler.Handle(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("expected status code %d, got %d", http.StatusCreated, w.Code)
	}
}

func TestEndpointHandler_Handle_JsonTranscodingError(t *testing.T) {
	// Setup
	executor := &MockExecutor{}
	transcoder := &MockTranscoder{}
	logger := zap.NewNop()

	handler := NewEndpointHandler(executor, transcoder, logger)

	// Create test request
	req := httptest.NewRequest("GET", "/test", nil)

	// Setup route info
	routeInfo := &config.RouteInfo{
		EndpointID: "test-endpoint",
		Endpoint: config.EndpointConfig{
			JSONOutput: true,
		},
	}
	ctx := context.WithValue(req.Context(), config.RouteCtx, routeInfo)
	req = req.WithContext(ctx)

	// Setup response recorder
	w := httptest.NewRecorder()

	// Setup executor response
	resultChan := make(chan *runtime.Result, 1)
	resultChan <- &runtime.Result{
		Payload: payload.NewPayload([]byte("raw data"), payload.Bytes),
	}
	close(resultChan)

	executor.executeFunc = func(task runtime.Task) (chan *runtime.Result, error) {
		return resultChan, nil
	}

	// Setup transcoder to return error
	transcoder.transcodeFunc = func(p payload.Payload, format payload.Format) (payload.Payload, error) {
		return nil, errors.New("transcoding failed")
	}

	// Run
	handler.Handle(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status code %d, got %d", http.StatusInternalServerError, w.Code)
	}

	if !strings.Contains(w.Body.String(), "transcoding failed") {
		t.Errorf("expected error message containing 'transcoding failed', got %q", w.Body.String())
	}
}

func TestEndpointHandler_Handle_NilPayload(t *testing.T) {
	// Setup
	executor := &MockExecutor{}
	transcoder := &MockTranscoder{}
	logger := zap.NewNop()

	handler := NewEndpointHandler(executor, transcoder, logger)

	// Create test request
	req := httptest.NewRequest("GET", "/test", nil)

	// Setup route info
	routeInfo := &config.RouteInfo{
		EndpointID: "test-endpoint",
		Endpoint:   config.EndpointConfig{},
	}
	ctx := context.WithValue(req.Context(), config.RouteCtx, routeInfo)
	req = req.WithContext(ctx)

	// Setup response recorder
	w := httptest.NewRecorder()

	// Setup executor response with nil payload
	resultChan := make(chan *runtime.Result, 1)
	resultChan <- &runtime.Result{
		Payload: nil,
	}
	close(resultChan)

	executor.executeFunc = func(task runtime.Task) (chan *runtime.Result, error) {
		return resultChan, nil
	}

	// Run
	handler.Handle(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status code %d, got %d", http.StatusOK, w.Code)
	}

	if w.Body.Len() != 0 {
		t.Errorf("expected empty response body, got %q", w.Body.String())
	}
}

func TestEndpointHandler_Handle_InvalidPayloadType(t *testing.T) {
	// Setup
	executor := &MockExecutor{}
	transcoder := &MockTranscoder{}
	logger := zap.NewNop()

	handler := NewEndpointHandler(executor, transcoder, logger)

	// Create test request
	req := httptest.NewRequest("GET", "/test", nil)

	// Setup route info
	routeInfo := &config.RouteInfo{
		EndpointID: "test-endpoint",
		Endpoint: config.EndpointConfig{
			JSONOutput: true,
		},
	}
	ctx := context.WithValue(req.Context(), config.RouteCtx, routeInfo)
	req = req.WithContext(ctx)

	// Setup response recorder
	w := httptest.NewRecorder()

	// Setup executor response
	resultChan := make(chan *runtime.Result, 1)
	resultChan <- &runtime.Result{
		Payload: payload.NewPayload("invalid type", payload.JSON), // String instead of []byte
	}
	close(resultChan)

	executor.executeFunc = func(task runtime.Task) (chan *runtime.Result, error) {
		return resultChan, nil
	}

	transcoder.transcodeFunc = func(p payload.Payload, format payload.Format) (payload.Payload, error) {
		return p, nil // Return the same invalid payload
	}

	// Run
	handler.Handle(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status code %d, got %d", http.StatusInternalServerError, w.Code)
	}

	if !strings.Contains(w.Body.String(), "invalid payload type") {
		t.Errorf("expected error message containing 'invalid payload type', got %q", w.Body.String())
	}
}

func TestEndpointHandler_Handle_NilExecutorResult(t *testing.T) {
	// Setup
	executor := &MockExecutor{}
	transcoder := &MockTranscoder{}
	logger := zap.NewNop()

	handler := NewEndpointHandler(executor, transcoder, logger)

	// Create test request
	req := httptest.NewRequest("GET", "/test", nil)

	// Setup route info
	routeInfo := &config.RouteInfo{
		EndpointID: "test-endpoint",
		Endpoint:   config.EndpointConfig{},
	}
	ctx := context.WithValue(req.Context(), config.RouteCtx, routeInfo)
	req = req.WithContext(ctx)

	// Setup response recorder
	w := httptest.NewRecorder()

	// Setup executor to return nil result
	resultChan := make(chan *runtime.Result, 1)
	resultChan <- nil
	close(resultChan)

	executor.executeFunc = func(task runtime.Task) (chan *runtime.Result, error) {
		return resultChan, nil
	}

	// Run
	handler.Handle(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status code %d, got %d", http.StatusInternalServerError, w.Code)
	}

	if !strings.Contains(w.Body.String(), "received nil result") {
		t.Errorf("expected error message containing 'received nil result', got %q", w.Body.String())
	}
}
