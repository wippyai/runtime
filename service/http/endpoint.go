package http

import (
	"context"
	"fmt"
	"github.com/ponyruntime/pony/api/runtime"
	"net/http"

	"github.com/ponyruntime/pony/api/payload"
	config "github.com/ponyruntime/pony/api/service/http"
	"go.uber.org/zap"
)

// EndpointHandler processes HTTP requests for specific endpoints.
// It handles request validation, execution, and response formatting.
type EndpointHandler struct {
	executor   runtime.FunctionRegistry
	transcoder payload.Transcoder
	log        *zap.Logger
}

// NewEndpointHandler creates a new EndpointHandler with the required dependencies.
func NewEndpointHandler(
	executor runtime.FunctionRegistry,
	transcoder payload.Transcoder,
	log *zap.Logger,
) *EndpointHandler {
	return &EndpointHandler{
		executor:   executor,
		transcoder: transcoder,
		log:        log,
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
	rCtx := config.NewRequestContext(r, w)
	task.Context = context.WithValue(task.Context, config.RequestCtx, rCtx)

	if _, err = h.executeTask(task); err != nil {
		if !rCtx.ResponseHandled() {
			h.handleError(w, err, http.StatusInternalServerError)
		}
		return
	}

	// we never write results to the response directly, use context wrapper instead
}

// getRouteInfo extracts route information from the request context.
func (h *EndpointHandler) getRouteInfo(r *http.Request) (*config.RouteInfo, error) {
	routeInfo, ok := r.Context().Value(config.RouteCtx).(*config.RouteInfo)
	if !ok {
		h.log.Error("route info not found in context")
		return nil, fmt.Errorf("route info not found")
	}
	return routeInfo, nil
}

// createTask builds a task from the HTTP request and route information.
func (h *EndpointHandler) createTask(r *http.Request, info *config.RouteInfo) (runtime.Task, error) {
	return runtime.Task{
		Context: r.Context(),
		Target:  info.Endpoint.Target,
	}, nil
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
		return nil, fmt.Errorf("request canceled: %w", task.Context.Err())
	}
}

// handleError logs the error and writes it to the response.
func (h *EndpointHandler) handleError(w http.ResponseWriter, err error, statusCode int) {
	h.log.Debug("request error", zap.Error(err), zap.Int("status_code", statusCode))
	http.Error(w, err.Error(), statusCode)
}
