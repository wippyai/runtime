// SPDX-License-Identifier: MPL-2.0

package security

import (
	"context"
	"testing"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/registry"
	secapi "github.com/wippyai/runtime/api/security"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	secsystem "github.com/wippyai/runtime/system/security"
)

// denyingTokenStore fails the test if any method runs, proving the permission
// check short-circuits before the store is touched.
type denyingTokenStore struct {
	t *testing.T
}

func (s *denyingTokenStore) Create(context.Context, secapi.Actor, secapi.Scope, secapi.TokenDetails) (secapi.Token, error) {
	s.t.Fatal("token store must not be reached when access is denied")
	return "", nil
}

func (s *denyingTokenStore) Validate(context.Context, secapi.Token) (secapi.Actor, secapi.Scope, error) {
	s.t.Fatal("token store must not be reached when access is denied")
	return secapi.Actor{}, nil, nil
}

func (s *denyingTokenStore) Revoke(context.Context, secapi.Token) error {
	s.t.Fatal("token store must not be reached when access is denied")
	return nil
}

// setupDeniedTokenStore builds a state whose scope denies everything and binds
// a token store handle plus an actor and scope as globals.
func setupDeniedTokenStore(t *testing.T) *lua.LState {
	t.Helper()

	actor := secapi.Actor{ID: "test-user"}
	pol := newMockPolicy("test", "deny-all", secapi.Deny)
	scope := secsystem.NewScope([]secapi.Policy{pol})
	l := setupStateWithSecurityContext(actor, scope)
	t.Cleanup(func() { l.Close() })

	ts := NewTokenStore(l.Context(), registry.NewID("test", "tokens"), nil, &denyingTokenStore{t: t})
	ud := l.NewUserData()
	ud.Value = ts
	ud.Metatable = value.GetTypeMetatable(l, tokenStoreTypeName)
	l.SetGlobal("store", ud)
	l.SetGlobal("actor", wrapActor(l, actor))
	l.SetGlobal("scope", wrapScope(l, scope))
	return l
}

func TestTokenStoreValidatePermissionDenied(t *testing.T) {
	l := setupDeniedTokenStore(t)

	err := l.DoString(`
		local a, s, err = store:validate("some-token")
		assert(a == nil, "expected nil actor under a deny policy")
		assert(s == nil, "expected nil scope under a deny policy")
		assert(err ~= nil, "expected error under a deny policy")
		assert(err:kind() == errors.PERMISSION_DENIED, "expected PERMISSION_DENIED kind, got: " .. tostring(err:kind()))
		assert(err:retryable() == false, "expected not retryable")
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}

func TestTokenStoreCreatePermissionDenied(t *testing.T) {
	l := setupDeniedTokenStore(t)

	err := l.DoString(`
		local token, err = store:create(actor, scope)
		assert(token == nil, "expected nil token under a deny policy")
		assert(err ~= nil, "expected error under a deny policy")
		assert(err:kind() == errors.PERMISSION_DENIED, "expected PERMISSION_DENIED kind, got: " .. tostring(err:kind()))
		assert(err:retryable() == false, "expected not retryable")
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}

func TestTokenStoreRevokePermissionDenied(t *testing.T) {
	l := setupDeniedTokenStore(t)

	err := l.DoString(`
		local ok, err = store:revoke("some-token")
		assert(ok == nil, "expected nil result under a deny policy")
		assert(err ~= nil, "expected error under a deny policy")
		assert(err:kind() == errors.PERMISSION_DENIED, "expected PERMISSION_DENIED kind, got: " .. tostring(err:kind()))
		assert(err:retryable() == false, "expected not retryable")
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}
