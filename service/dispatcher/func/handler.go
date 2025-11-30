// Package funchandler provides function call command handlers for the dispatcher system.
package funchandler

import (
	"context"
	"errors"

	"github.com/wippyai/runtime/api/dispatcher"
	funcapi "github.com/wippyai/runtime/api/dispatcher/func"
	"github.com/wippyai/runtime/api/function"
)

var ErrRegistryNotFound = errors.New("function registry not found in context")

// CallHandler processes function call commands.
type CallHandler struct{}

// NewCallHandler creates a new function call handler.
func NewCallHandler() *CallHandler {
	return &CallHandler{}
}

// Handle implements dispatcher.Handler.
func (h *CallHandler) Handle(ctx context.Context, cmd dispatcher.Command, emit dispatcher.EmitFunc) error {
	callCmd := cmd.(*funcapi.CallCmd)

	registry := function.GetRegistry(ctx)
	if registry == nil {
		emit(funcapi.Response{Error: ErrRegistryNotFound})
		return nil
	}

	result, err := registry.Call(ctx, callCmd.Task)
	if err != nil {
		emit(funcapi.Response{Error: err})
		return nil
	}

	if result.Error != nil {
		emit(funcapi.Response{Error: result.Error})
		return nil
	}

	emit(funcapi.Response{Value: result.Value})
	return nil
}

// Service bundles function handlers for convenient registration.
type Service struct {
	Call *CallHandler

	// Async function call handlers
	AsyncStart  *AsyncStartHandler
	AsyncAwait  *AsyncAwaitHandler
	AsyncCancel *AsyncCancelHandler
}

// NewService creates a new function service with all handlers initialized.
func NewService() *Service {
	return &Service{
		Call:        NewCallHandler(),
		AsyncStart:  NewAsyncStartHandler(),
		AsyncAwait:  NewAsyncAwaitHandler(),
		AsyncCancel: NewAsyncCancelHandler(),
	}
}

// RegisterAll registers all function handlers with the given registry function.
func (s *Service) RegisterAll(register func(id dispatcher.CommandID, h dispatcher.Handler)) {
	register(funcapi.CmdCall, s.Call)
	register(funcapi.CmdAsyncStart, s.AsyncStart)
	register(funcapi.CmdAsyncAwait, s.AsyncAwait)
	register(funcapi.CmdAsyncCancel, s.AsyncCancel)
}
