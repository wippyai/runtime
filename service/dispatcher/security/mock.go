package security

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/wippyai/runtime/api/dispatcher"
	securityapi "github.com/wippyai/runtime/api/dispatcher/security"
	secapi "github.com/wippyai/runtime/api/security"
)

// MockTokenStore implements secapi.TokenStore for testing.
type MockTokenStore struct {
	mu     sync.Mutex
	tokens map[secapi.Token]mockToken
}

type mockToken struct {
	actor secapi.Actor
	scope secapi.Scope
}

func NewMockTokenStore() *MockTokenStore {
	return &MockTokenStore{
		tokens: make(map[secapi.Token]mockToken),
	}
}

func (s *MockTokenStore) Validate(ctx context.Context, token secapi.Token) (secapi.Actor, secapi.Scope, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t, ok := s.tokens[token]; ok {
		return t.actor, t.scope, nil
	}
	return secapi.Actor{}, nil, secapi.ErrTokenInvalid
}

func (s *MockTokenStore) Create(ctx context.Context, actor secapi.Actor, scope secapi.Scope, details secapi.TokenDetails) (secapi.Token, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	token := secapi.Token("mock-token-" + actor.ID)
	s.tokens[token] = mockToken{actor: actor, scope: scope}
	return token, nil
}

func (s *MockTokenStore) Revoke(ctx context.Context, token secapi.Token) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.tokens, token)
	return nil
}

// MockTokenValidateHandler validates tokens for testing.
type MockTokenValidateHandler struct {
	store *MockTokenStore
	count atomic.Int64
}

func NewMockTokenValidateHandler(store *MockTokenStore) *MockTokenValidateHandler {
	return &MockTokenValidateHandler{store: store}
}

func (h *MockTokenValidateHandler) Handle(ctx context.Context, cmd dispatcher.Command, emit dispatcher.EmitFunc) error {
	h.count.Add(1)
	validateCmd := cmd.(*securityapi.TokenValidateCmd)
	actor, scope, err := h.store.Validate(ctx, validateCmd.Token)
	emit(securityapi.TokenValidateResponse{
		Actor: actor,
		Scope: scope,
		Error: err,
	})
	return nil
}

func (h *MockTokenValidateHandler) Count() int64 { return h.count.Load() }

// MockTokenCreateHandler creates tokens for testing.
type MockTokenCreateHandler struct {
	store *MockTokenStore
	count atomic.Int64
}

func NewMockTokenCreateHandler(store *MockTokenStore) *MockTokenCreateHandler {
	return &MockTokenCreateHandler{store: store}
}

func (h *MockTokenCreateHandler) Handle(ctx context.Context, cmd dispatcher.Command, emit dispatcher.EmitFunc) error {
	h.count.Add(1)
	createCmd := cmd.(*securityapi.TokenCreateCmd)
	token, err := h.store.Create(ctx, createCmd.Actor, createCmd.Scope, createCmd.Details)
	emit(securityapi.TokenCreateResponse{
		Token: token,
		Error: err,
	})
	return nil
}

func (h *MockTokenCreateHandler) Count() int64 { return h.count.Load() }

// MockTokenRevokeHandler revokes tokens for testing.
type MockTokenRevokeHandler struct {
	store *MockTokenStore
	count atomic.Int64
}

func NewMockTokenRevokeHandler(store *MockTokenStore) *MockTokenRevokeHandler {
	return &MockTokenRevokeHandler{store: store}
}

func (h *MockTokenRevokeHandler) Handle(ctx context.Context, cmd dispatcher.Command, emit dispatcher.EmitFunc) error {
	h.count.Add(1)
	revokeCmd := cmd.(*securityapi.TokenRevokeCmd)
	err := h.store.Revoke(ctx, revokeCmd.Token)
	emit(securityapi.TokenRevokeResponse{
		Error: err,
	})
	return nil
}

func (h *MockTokenRevokeHandler) Count() int64 { return h.count.Load() }

// MockService bundles all mock handlers for testing.
type MockService struct {
	Store         *MockTokenStore
	TokenValidate *MockTokenValidateHandler
	TokenCreate   *MockTokenCreateHandler
	TokenRevoke   *MockTokenRevokeHandler
}

func NewMockService() *MockService {
	store := NewMockTokenStore()
	return &MockService{
		Store:         store,
		TokenValidate: NewMockTokenValidateHandler(store),
		TokenCreate:   NewMockTokenCreateHandler(store),
		TokenRevoke:   NewMockTokenRevokeHandler(store),
	}
}

func (s *MockService) RegisterAll(register func(id dispatcher.CommandID, h dispatcher.Handler)) {
	register(securityapi.CmdTokenValidate, s.TokenValidate)
	register(securityapi.CmdTokenCreate, s.TokenCreate)
	register(securityapi.CmdTokenRevoke, s.TokenRevoke)
}
