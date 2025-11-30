// Package security provides token store command handlers for the dispatcher system.
package security

import (
	"context"

	"github.com/wippyai/runtime/api/dispatcher"
	securityapi "github.com/wippyai/runtime/api/dispatcher/security"
)

// TokenValidateHandler validates tokens.
type TokenValidateHandler struct{}

func NewTokenValidateHandler() *TokenValidateHandler {
	return &TokenValidateHandler{}
}

func (h *TokenValidateHandler) Handle(ctx context.Context, cmd dispatcher.Command, emit dispatcher.EmitFunc) error {
	validateCmd := cmd.(*securityapi.TokenValidateCmd)

	actor, scope, err := validateCmd.TokenStore.Validate(ctx, validateCmd.Token)
	emit(securityapi.TokenValidateResponse{
		Actor: actor,
		Scope: scope,
		Error: err,
	})
	return nil
}

// TokenCreateHandler creates tokens.
type TokenCreateHandler struct{}

func NewTokenCreateHandler() *TokenCreateHandler {
	return &TokenCreateHandler{}
}

func (h *TokenCreateHandler) Handle(ctx context.Context, cmd dispatcher.Command, emit dispatcher.EmitFunc) error {
	createCmd := cmd.(*securityapi.TokenCreateCmd)

	token, err := createCmd.TokenStore.Create(ctx, createCmd.Actor, createCmd.Scope, createCmd.Details)
	emit(securityapi.TokenCreateResponse{
		Token: token,
		Error: err,
	})
	return nil
}

// TokenRevokeHandler revokes tokens.
type TokenRevokeHandler struct{}

func NewTokenRevokeHandler() *TokenRevokeHandler {
	return &TokenRevokeHandler{}
}

func (h *TokenRevokeHandler) Handle(ctx context.Context, cmd dispatcher.Command, emit dispatcher.EmitFunc) error {
	revokeCmd := cmd.(*securityapi.TokenRevokeCmd)

	err := revokeCmd.TokenStore.Revoke(ctx, revokeCmd.Token)
	emit(securityapi.TokenRevokeResponse{
		Error: err,
	})
	return nil
}

// Service bundles all security handlers.
type Service struct {
	TokenValidate *TokenValidateHandler
	TokenCreate   *TokenCreateHandler
	TokenRevoke   *TokenRevokeHandler
}

// NewService creates a new security service with all handlers initialized.
func NewService() *Service {
	return &Service{
		TokenValidate: NewTokenValidateHandler(),
		TokenCreate:   NewTokenCreateHandler(),
		TokenRevoke:   NewTokenRevokeHandler(),
	}
}

// RegisterAll registers all security handlers with the given registry function.
func (s *Service) RegisterAll(register func(id dispatcher.CommandID, h dispatcher.Handler)) {
	register(securityapi.CmdTokenValidate, s.TokenValidate)
	register(securityapi.CmdTokenCreate, s.TokenCreate)
	register(securityapi.CmdTokenRevoke, s.TokenRevoke)
}
