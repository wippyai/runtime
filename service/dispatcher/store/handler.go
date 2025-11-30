// Package store provides store command handlers for the dispatcher system.
package store

import (
	"context"

	"github.com/wippyai/runtime/api/dispatcher"
	storeapi "github.com/wippyai/runtime/api/dispatcher/store"
)

// StoreGetHandler handles store get commands.
type StoreGetHandler struct{}

func NewStoreGetHandler() *StoreGetHandler {
	return &StoreGetHandler{}
}

func (h *StoreGetHandler) Handle(ctx context.Context, cmd dispatcher.Command, emit dispatcher.EmitFunc) error {
	getCmd := cmd.(*storeapi.StoreGetCmd)

	value, err := getCmd.Store.Get(ctx, getCmd.Key)
	emit(storeapi.StoreGetResponse{Value: value, Error: err})
	return nil
}

// StoreSetHandler handles store set commands.
type StoreSetHandler struct{}

func NewStoreSetHandler() *StoreSetHandler {
	return &StoreSetHandler{}
}

func (h *StoreSetHandler) Handle(ctx context.Context, cmd dispatcher.Command, emit dispatcher.EmitFunc) error {
	setCmd := cmd.(*storeapi.StoreSetCmd)

	err := setCmd.Store.Set(ctx, setCmd.Entry)
	emit(storeapi.StoreSetResponse{Error: err})
	return nil
}

// StoreDeleteHandler handles store delete commands.
type StoreDeleteHandler struct{}

func NewStoreDeleteHandler() *StoreDeleteHandler {
	return &StoreDeleteHandler{}
}

func (h *StoreDeleteHandler) Handle(ctx context.Context, cmd dispatcher.Command, emit dispatcher.EmitFunc) error {
	deleteCmd := cmd.(*storeapi.StoreDeleteCmd)

	err := deleteCmd.Store.Delete(ctx, deleteCmd.Key)
	emit(storeapi.StoreDeleteResponse{Error: err})
	return nil
}

// StoreHasHandler handles store has commands.
type StoreHasHandler struct{}

func NewStoreHasHandler() *StoreHasHandler {
	return &StoreHasHandler{}
}

func (h *StoreHasHandler) Handle(ctx context.Context, cmd dispatcher.Command, emit dispatcher.EmitFunc) error {
	hasCmd := cmd.(*storeapi.StoreHasCmd)

	exists, err := hasCmd.Store.Has(ctx, hasCmd.Key)
	emit(storeapi.StoreHasResponse{Exists: exists, Error: err})
	return nil
}

// Service bundles all store handlers.
type Service struct {
	Get    *StoreGetHandler
	Set    *StoreSetHandler
	Delete *StoreDeleteHandler
	Has    *StoreHasHandler
}

// NewService creates a new store service with all handlers initialized.
func NewService() *Service {
	return &Service{
		Get:    NewStoreGetHandler(),
		Set:    NewStoreSetHandler(),
		Delete: NewStoreDeleteHandler(),
		Has:    NewStoreHasHandler(),
	}
}

// RegisterAll registers all store handlers with the given registry function.
func (s *Service) RegisterAll(register func(id dispatcher.CommandID, h dispatcher.Handler)) {
	register(storeapi.CmdStoreGet, s.Get)
	register(storeapi.CmdStoreSet, s.Set)
	register(storeapi.CmdStoreDelete, s.Delete)
	register(storeapi.CmdStoreHas, s.Has)
}
