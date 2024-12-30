package handler

import (
	"context"
	"fmt"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/runtime"
	config "github.com/ponyruntime/pony/api/service/http"
	"go.uber.org/zap"
	"io"
	"net/http"
)

// EndpointHandler processes HTTP requests for specific endpoints.
// It handles request validation, execution, and response formatting.
type EndpointHandler struct {
	executor   runtime.Executor
	transcoder payload.Transcoder
	logger     *zap.Logger
}

// NewEndpointHandler creates a new EndpointHandler with the required dependencies.
func NewEndpointHandler(
	executor runtime.Executor,
	transcoder payload.Transcoder,
	logger *zap.Logger,
) *EndpointHandler {
	return &EndpointHandler{
		executor:   executor,
		transcoder: transcoder,
		logger:     logger,
	}
}

// Handle processes incoming HTTP requests.
// It extracts route information, validates the request, executes the task,
// and writes the response back to the client.
func (h *EndpointHandler) Handle(w http.ResponseWriter, r *http.Request) {
	routeInfo, err := h.getRouteInfo(r)
	if err != nil {
		h.handleError(w, err, http.StatusInternalServerError)
		return
	}

	task, err := h.createTask(r, routeInfo)
	if err != nil {
		statusCode := http.StatusBadRequest
		h.handleError(w, err, statusCode)
		return
	}

	// allows internal funcs to work with the request directly
	task.Context = context.WithValue(task.Context, config.RequestCtx, config.NewRequestContext(r, w))

	result, err := h.executeTask(task)
	if err != nil {
		h.handleError(w, err, http.StatusInternalServerError)
		return
	}

	h.writeResponse(w, result, routeInfo.Endpoint)
}

// getRouteInfo extracts route information from the request context.
func (h *EndpointHandler) getRouteInfo(r *http.Request) (*config.RouteInfo, error) {
	routeInfo, ok := r.Context().Value(config.RouteCtx).(*config.RouteInfo)
	if !ok {
		h.logger.Error("route info not found in context")
		return nil, fmt.Errorf("route info not found")
	}
	return routeInfo, nil
}

// createTask builds a task from the HTTP request and route information.
func (h *EndpointHandler) createTask(r *http.Request, info *config.RouteInfo) (runtime.Task, error) {
	if !info.Endpoint.JsonInput {
		return runtime.Task{
			Context: r.Context(),
			Target:  registry.ID(info.Endpoint.Target),
		}, nil
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		return runtime.Task{}, fmt.Errorf("reading request body: %w", err)
	}
	defer r.Body.Close()

	if err := h.validateJsonInput(body, info.Endpoint.JsonSchema); err != nil {
		return runtime.Task{}, fmt.Errorf("validation failed: %w", err)
	}

	return runtime.Task{
		Context: r.Context(),
		Target:  registry.ID(info.Endpoint.Target),
		Payload: payload.NewPayload(body, payload.Json),
	}, nil
}

// validateJsonInput validates JSON input against the provided schema.
func (h *EndpointHandler) validateJsonInput(body []byte, schema interface{}) error {
	if schema == nil {
		return nil
	}

	validator, err := newJsonValidator(schema)
	if err != nil {
		return fmt.Errorf("creating JSON validator: %w", err)
	}

	return validator.Validate(body)
}

// executeTask runs the task and handles context cancellation.
func (h *EndpointHandler) executeTask(task runtime.Task) (*runtime.Result, error) {
	resultCh, err := h.executor.Execute(task)
	if err != nil {
		return nil, fmt.Errorf("executing task: %w", err)
	}

	select {
	case result := <-resultCh:
		if result == nil {
			return nil, fmt.Errorf("received nil result from executor")
		}
		return result, nil
	case <-task.Context.Done():
		return nil, fmt.Errorf("request cancelled: %w", task.Context.Err())
	}
}

// writeResponse formats and writes the task result to the HTTP response.
func (h *EndpointHandler) writeResponse(w http.ResponseWriter, result *runtime.Result, cfg config.EndpointConfig) {
	if result.Error != nil {
		h.handleError(w, result.Error, http.StatusInternalServerError)
		return
	}

	statusCode := http.StatusOK
	if cfg.SuccessStatusCode != 0 {
		statusCode = cfg.SuccessStatusCode
	}

	if cfg.JsonOutput {
		h.writeJsonResponse(w, result.Payload, statusCode)
		return
	}

	h.writeRawResponse(w, result.Payload, statusCode)
}

// writeJsonResponse writes a JSON response with proper headers.
func (h *EndpointHandler) writeJsonResponse(w http.ResponseWriter, p payload.Payload, statusCode int) {
	out, err := h.transcoder.Transcode(p, payload.Json)
	if err != nil {
		h.handleError(w, fmt.Errorf("transcoding to JSON: %w", err), http.StatusInternalServerError)
		return
	}

	data, ok := out.Data().([]byte)
	if !ok {
		h.handleError(w, fmt.Errorf("invalid payload type: %T", out.Data()), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	if _, err := w.Write(data); err != nil {
		h.logger.Error("error writing JSON response", zap.Error(err))
	}
}

// writeRawResponse writes a raw response with the given status code.
func (h *EndpointHandler) writeRawResponse(w http.ResponseWriter, p payload.Payload, statusCode int) {
	w.WriteHeader(statusCode)

	if p == nil {
		return // No content case
	}

	if data, ok := p.Data().([]byte); ok {
		if _, err := w.Write(data); err != nil {
			h.logger.Error("error writing raw response", zap.Error(err))
		}
	}
}

// handleError logs the error and writes it to the response.
func (h *EndpointHandler) handleError(w http.ResponseWriter, err error, statusCode int) {
	h.logger.Debug("request error", zap.Error(err), zap.Int("status_code", statusCode))
	http.Error(w, err.Error(), statusCode)
}
