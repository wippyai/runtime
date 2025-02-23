package __ignore

import (
	"context"
	"fmt"
	"github.com/ponyruntime/pony/api/function"
	"github.com/ponyruntime/pony/api/runtime"
	"net/http"

	"github.com/ponyruntime/pony/api/payload"
	config "github.com/ponyruntime/pony/api/service/http"
	"go.uber.org/zap"
)

// Handler processes HTTP requests for specific endpoints.
// It handles request validation, execution, and response formatting.
type Handler struct {
	funcs function.Registry
	dtt   payload.Transcoder
	log   *zap.Logger
}

// NewEndpointHandler creates a new Handler with the required dependencies.
func NewEndpointHandler(
	funcs function.Registry,
	dtt payload.Transcoder,
	log *zap.Logger,
) *Handler {
	return &Handler{
		funcs: funcs,
		dtt:   dtt,
		log:   log,
	}
}

// Handle processes incoming HTTP requests.
// It extracts route information, validates the request, executes the task,
// and writes the response back to the client.
func (h *Handler) Handle(w http.ResponseWriter, r *http.Request) {
	routeInfo, err := h.getRouteInfo(r)
	if err != nil {
		h.handleError(w, err, http.StatusInternalServerError)
		return
	}

	rCtx := config.NewRequestContext(r, w)
	ctx := context.WithValue(r.Context(), config.RequestCtx, rCtx)

	if _, err = h.executeTask(ctx, runtime.Task{ID: routeInfo.Endpoint.Func}); err != nil {
		if !rCtx.ResponseHandled() {
			h.handleError(w, err, http.StatusInternalServerError)
		}
		return
	}

	if !rCtx.ResponseHandled() {
		http.Error(w, "no response sent by endpoint", http.StatusInternalServerError)
	}
}

// getRouteInfo extracts route information from the request context.
func (h *Handler) getRouteInfo(r *http.Request) (*config.RouteInfo, error) {
	routeInfo, ok := r.Context().Value(config.RouteCtx).(*config.RouteInfo)
	if !ok {
		h.log.Error("route info not found in context")
		return nil, fmt.Errorf("route info not found")
	}
	return routeInfo, nil
}

// executeTask runs the task and handles context cancellation.
func (h *Handler) executeTask(ctx context.Context, task runtime.Task) (*runtime.Result, error) {
	resultCh, err := h.funcs.Call(ctx, task)
	if err != nil {
		return nil, fmt.Errorf("executing task: %w", err)
	}

	select {
	case result := <-resultCh:
		if result == nil {
			return nil, fmt.Errorf("received nil result from funcs")
		}
		return result, result.Error
	case <-ctx.Done():
		return nil, fmt.Errorf("request canceled: %w", ctx.Err())
	}
}

// handleError logs the error and writes it to the response.
func (h *Handler) handleError(w http.ResponseWriter, err error, statusCode int) {
	h.log.Debug("request error", zap.Error(err), zap.Int("status_code", statusCode))
	http.Error(w, err.Error(), statusCode)
}
