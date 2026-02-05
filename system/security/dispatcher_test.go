package security

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/security"
)

type mockTokenStore struct {
	mu             sync.Mutex
	validateCalled int
	createCalled   int
	revokeCalled   int
	validateErr    error
	createErr      error
	revokeErr      error
}

func (m *mockTokenStore) Validate(_ context.Context, _ security.Token) (security.Actor, security.Scope, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.validateCalled++
	if m.validateErr != nil {
		return security.Actor{}, nil, m.validateErr
	}
	return security.Actor{ID: "user-1"}, nil, nil
}

func (m *mockTokenStore) Create(_ context.Context, actor security.Actor, _ security.Scope, _ security.TokenDetails) (security.Token, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.createCalled++
	if m.createErr != nil {
		return "", m.createErr
	}
	return security.Token("tok-" + actor.ID), nil
}

func (m *mockTokenStore) Revoke(_ context.Context, _ security.Token) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.revokeCalled++
	return m.revokeErr
}

type testResultReceiver struct {
	mu   sync.Mutex
	data any
	err  error
	done chan struct{}
}

func newTestResultReceiver() *testResultReceiver {
	return &testResultReceiver{done: make(chan struct{})}
}

func (r *testResultReceiver) CompleteYield(_ uint64, data any, err error) {
	r.mu.Lock()
	r.data = data
	r.err = err
	r.mu.Unlock()
	close(r.done)
}

func (r *testResultReceiver) wait(t *testing.T) {
	t.Helper()
	select {
	case <-r.done:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for result")
	}
}

// --- Dispatcher lifecycle ---

func TestDispatcher_StartStop(t *testing.T) {
	d := NewDispatcher(2)
	require.NoError(t, d.Start(context.Background()))
	require.NoError(t, d.Stop(context.Background()))
}

// --- Dispatcher.execute: ValidateToken ---

func TestDispatcher_Execute_ValidateToken(t *testing.T) {
	store := &mockTokenStore{}
	d := NewDispatcher(2)
	require.NoError(t, d.Start(context.Background()))
	defer func() { _ = d.Stop(context.Background()) }()

	recv := newTestResultReceiver()
	d.submit(context.Background(), &security.ValidateTokenCmd{
		TokenStore: store,
		Token:      "test-token",
	}, 1, recv)

	recv.wait(t)

	resp, ok := recv.data.(security.ValidateTokenResponse)
	require.True(t, ok)
	assert.Equal(t, "user-1", resp.Actor.ID)
	assert.NoError(t, resp.Error)
	assert.Equal(t, 1, store.validateCalled)
}

func TestDispatcher_Execute_ValidateToken_Error(t *testing.T) {
	store := &mockTokenStore{validateErr: assert.AnError}
	d := NewDispatcher(2)
	require.NoError(t, d.Start(context.Background()))
	defer func() { _ = d.Stop(context.Background()) }()

	recv := newTestResultReceiver()
	d.submit(context.Background(), &security.ValidateTokenCmd{
		TokenStore: store,
		Token:      "bad-token",
	}, 1, recv)

	recv.wait(t)

	resp, ok := recv.data.(security.ValidateTokenResponse)
	require.True(t, ok)
	assert.Equal(t, assert.AnError, resp.Error)
}

// --- Dispatcher.execute: CreateToken ---

func TestDispatcher_Execute_CreateToken(t *testing.T) {
	store := &mockTokenStore{}
	d := NewDispatcher(2)
	require.NoError(t, d.Start(context.Background()))
	defer func() { _ = d.Stop(context.Background()) }()

	recv := newTestResultReceiver()
	d.submit(context.Background(), &security.CreateTokenCmd{
		TokenStore: store,
		Actor:      security.Actor{ID: "alice"},
	}, 2, recv)

	recv.wait(t)

	resp, ok := recv.data.(security.CreateTokenResponse)
	require.True(t, ok)
	assert.Equal(t, security.Token("tok-alice"), resp.Token)
	assert.NoError(t, resp.Error)
	assert.Equal(t, 1, store.createCalled)
}

func TestDispatcher_Execute_CreateToken_Error(t *testing.T) {
	store := &mockTokenStore{createErr: assert.AnError}
	d := NewDispatcher(2)
	require.NoError(t, d.Start(context.Background()))
	defer func() { _ = d.Stop(context.Background()) }()

	recv := newTestResultReceiver()
	d.submit(context.Background(), &security.CreateTokenCmd{
		TokenStore: store,
		Actor:      security.Actor{ID: "alice"},
	}, 2, recv)

	recv.wait(t)

	resp, ok := recv.data.(security.CreateTokenResponse)
	require.True(t, ok)
	assert.Equal(t, assert.AnError, resp.Error)
}

// --- Dispatcher.execute: RevokeToken ---

func TestDispatcher_Execute_RevokeToken(t *testing.T) {
	store := &mockTokenStore{}
	d := NewDispatcher(2)
	require.NoError(t, d.Start(context.Background()))
	defer func() { _ = d.Stop(context.Background()) }()

	recv := newTestResultReceiver()
	d.submit(context.Background(), &security.RevokeTokenCmd{
		TokenStore: store,
		Token:      "revoke-me",
	}, 3, recv)

	recv.wait(t)

	resp, ok := recv.data.(security.RevokeTokenResponse)
	require.True(t, ok)
	assert.NoError(t, resp.Error)
	assert.Equal(t, 1, store.revokeCalled)
}

func TestDispatcher_Execute_RevokeToken_Error(t *testing.T) {
	store := &mockTokenStore{revokeErr: assert.AnError}
	d := NewDispatcher(2)
	require.NoError(t, d.Start(context.Background()))
	defer func() { _ = d.Stop(context.Background()) }()

	recv := newTestResultReceiver()
	d.submit(context.Background(), &security.RevokeTokenCmd{
		TokenStore: store,
		Token:      "revoke-me",
	}, 3, recv)

	recv.wait(t)

	resp, ok := recv.data.(security.RevokeTokenResponse)
	require.True(t, ok)
	assert.Equal(t, assert.AnError, resp.Error)
}

// --- RegisterAll ---

func TestDispatcher_RegisterAll(t *testing.T) {
	d := NewDispatcher(1)

	registered := make(map[dispatcher.CommandID]bool)
	d.RegisterAll(func(id dispatcher.CommandID, _ dispatcher.Handler) {
		registered[id] = true
	})

	assert.True(t, registered[security.ValidateToken])
	assert.True(t, registered[security.CreateToken])
	assert.True(t, registered[security.RevokeToken])
	assert.Len(t, registered, 3)
}
