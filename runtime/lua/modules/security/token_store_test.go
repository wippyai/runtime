// SPDX-License-Identifier: MPL-2.0

package security

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/registry"
	secapi "github.com/wippyai/runtime/api/security"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	secsystem "github.com/wippyai/runtime/system/security"
)

func TestValidateYieldErrorMetadata(t *testing.T) {
	lua.SetErrorMetadataExtractor(func(err error) *lua.ErrorMetadata {
		root := apierror.BuildChain(err).Root()
		return &lua.ErrorMetadata{Kind: lua.Kind(root.Kind), Retryable: root.Retryable, Details: root.Details}
	})
	t.Cleanup(func() { lua.SetErrorMetadataExtractor(nil) })

	for _, kind := range []apierror.Kind{apierror.PermissionDenied, apierror.Unavailable, apierror.Invalid, apierror.Internal} {
		for _, retryable := range []apierror.Ternary{apierror.False, apierror.True, apierror.Unspecified} {
			cause := apierror.New(kind, "validation fails").WithRetryable(retryable).
				WithDetails(attrs.Bag{"stage": "subject lookup", "attempt": 2})
			for _, path := range []string{"dispatcher", "native"} {
				t.Run(fmt.Sprintf("%s/%s/%s", path, kind, retryable), func(t *testing.T) {
					l := lua.NewState()
					defer l.Close()
					var data any
					var err error
					wrapped := fmt.Errorf("store validation: %w", cause)
					if path == "dispatcher" {
						err = wrapped
					} else {
						data = secapi.ValidateTokenResponse{Error: wrapped}
					}
					result := (&ValidateYield{}).HandleResult(l, data, err)
					if len(result) != 3 || result[0] != lua.LNil || result[1] != lua.LNil {
						t.Fatalf("unexpected validation result: %v", result)
					}
					got, ok := result[2].(*lua.Error)
					if !ok {
						t.Fatalf("expected structured error, got %T", result[2])
					}
					wantRetry := map[apierror.Ternary]lua.Ternary{apierror.False: lua.TernaryFalse,
						apierror.True: lua.TernaryTrue, apierror.Unspecified: lua.TernaryUnknown}[retryable]
					if got.Kind() != lua.Kind(kind) || got.Retryable() != wantRetry {
						t.Errorf("metadata = %s/%s, want %s/%s", got.Kind(), got.Retryable(), kind, wantRetry)
					}
					if !reflect.DeepEqual(got.Details(), map[string]any{"stage": "subject lookup", "attempt": 2}) {
						t.Errorf("details = %v", got.Details())
					}
					if !errors.Is(got, cause) || got.Context != "validate token" {
						t.Errorf("cause or operation context is lost: %v", got)
					}
				})
			}
		}
	}
}

func TestValidateYieldInternalFailures(t *testing.T) {
	for _, path := range []string{"dispatcher", "native", "malformed response"} {
		t.Run(path, func(t *testing.T) {
			l := lua.NewState()
			defer l.Close()
			var data any
			var err error
			cause := errors.New("plain failure")
			switch path {
			case "dispatcher":
				err = cause
			case "native":
				data = secapi.ValidateTokenResponse{Error: cause}
			case "malformed response":
				data = "unexpected"
			}
			result := (&ValidateYield{}).HandleResult(l, data, err)
			if len(result) != 3 || result[0] != lua.LNil || result[1] != lua.LNil {
				t.Fatalf("unexpected validation result: %v", result)
			}
			got, ok := result[2].(*lua.Error)
			if !ok || got.Kind() != lua.Internal || got.Retryable() != lua.TernaryFalse {
				t.Fatalf("expected nonretryable Internal, got %v", result[2])
			}
		})
	}
}

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
