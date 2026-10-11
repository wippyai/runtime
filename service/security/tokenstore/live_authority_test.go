// SPDX-License-Identifier: MPL-2.0

package tokenstore_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	ctxapi "github.com/wippyai/runtime/api/context"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/function"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/resource"
	runtimeapi "github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/api/security"
	tokenapi "github.com/wippyai/runtime/api/service/security/tokenstore"
	tokenimpl "github.com/wippyai/runtime/service/security/tokenstore"
	memorystore "github.com/wippyai/runtime/service/store/memory"
	securitysys "github.com/wippyai/runtime/system/security"
	"go.uber.org/zap"
)

type lookupFunc func(context.Context, runtimeapi.Task) (*runtimeapi.Result, error)

func (f lookupFunc) Call(ctx context.Context, task runtimeapi.Task) (*runtimeapi.Result, error) {
	return f(ctx, task)
}

type resourcePolicy struct {
	id       registry.ID
	resource string
	decision security.Result
}

func (p resourcePolicy) ID() registry.ID { return p.id }
func (p resourcePolicy) Evaluate(_ security.Actor, _, resource string, _ attrs.Bag) security.Result {
	if resource == p.resource {
		return p.decision
	}
	return security.Undefined
}

func TestTokenLiveAuthority(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	storeID := registry.NewID("app", "tokens")
	kv := memorystore.NewStore(storeID, nil, zap.NewNop())
	started, err := kv.Start(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, kv.Stop(ctx)) })
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("store does not start")
	}
	resources := newTestResourceRegistry()
	resources.Register(storeID, kv)
	reg := newTestSecurityRegistry()
	before := resourcePolicy{registry.NewID("app", "before"), "retained", security.Allow}
	removed := resourcePolicy{registry.NewID("app", "removed"), "removed", security.Allow}
	after := resourcePolicy{registry.NewID("app", "after"), "retained", security.Allow}
	added := resourcePolicy{registry.NewID("app", "added"), "added", security.Allow}
	ceiling := resourcePolicy{registry.NewID("app", "ceiling"), "retained", security.Allow}
	for _, p := range []resourcePolicy{before, removed, after, added, ceiling} {
		reg.policies[p.id.String()] = p
	}
	cfg := &tokenapi.Config{Store: storeID, TokenKey: "signed", TokenLength: 32}
	ts, err := tokenimpl.NewStoreTokenStore(cfg, &jsonTranscoder{}, resources, reg)
	require.NoError(t, err)
	actor := security.Actor{ID: "member", Meta: attrs.Bag{"status": "old", "security_groups": []string{"old"}}}
	snapshot := securitysys.NewScope([]security.Policy{before, removed})
	token, err := ts.Create(ctx, actor, snapshot, security.TokenDetails{Meta: attrs.Bag{"restriction": "retained"}})
	require.NoError(t, err)
	unrestricted, err := ts.Create(ctx, actor, snapshot, security.TokenDetails{})
	require.NoError(t, err)
	removalToken, err := ts.Create(ctx, actor, securitysys.NewScope([]security.Policy{after}), security.TokenDetails{})
	require.NoError(t, err)
	base := string(token)
	for i, c := range base {
		if c == '.' {
			base = base[:i]
			break
		}
	}
	stored, err := kv.Get(ctx, registry.ParseID(base))
	require.NoError(t, err)
	bytesBefore, err := json.Marshal(stored.Data())
	require.NoError(t, err)

	var configured tokenapi.Config
	require.NoError(t, json.Unmarshal([]byte(`{"store":"app:tokens","token_key":"signed","token_length":32,"subject_lookup":"app:lookup"}`), &configured))
	live, err := tokenimpl.NewStoreTokenStore(&configured, &jsonTranscoder{}, resources, reg)
	require.NoError(t, err)
	groups := []string{"app:current", "app:extra"}
	reg.groups["app:current"] = []security.Policy{after}
	reg.groups["app:extra"] = []security.Policy{added}
	reg.groups["app:default"] = []security.Policy{after, added}
	delete(reg.policies, before.id.String())
	delete(reg.policies, removed.id.String())
	calls := 0
	var lookupError error
	var failure any
	ctx = resource.WithRegistry(ctx, resources)
	ctx = function.WithRegistry(ctx, lookupFunc(func(callCtx context.Context, task runtimeapi.Task) (*runtimeapi.Result, error) {
		calls++
		require.Equal(t, registry.NewID("app", "lookup"), task.ID)
		require.Same(t, ctxapi.AppFromContext(ctx), ctxapi.AppFromContext(callCtx))
		require.Len(t, task.Payloads, 1)
		var input struct {
			TokenMeta attrs.Bag `json:"token_meta"`
			SubjectID string    `json:"subject_id"`
		}
		require.NoError(t, (&jsonTranscoder{}).Unmarshal(task.Payloads[0], &input))
		require.Equal(t, "member", input.SubjectID)
		if lookupError != nil {
			return nil, lookupError
		}
		if failure != nil {
			return &runtimeapi.Result{Value: payload.New(failure)}, nil
		}
		output := map[string]any{"subject_id": "member", "meta": attrs.Bag{"status": "active"}, "groups": groups, "scope_groups": []string{"app:default"}}
		if input.TokenMeta["restriction"] == "retained" {
			output["ceiling"] = map[string]any{"policies": []string{"app:ceiling"}}
		}
		return &runtimeapi.Result{Value: payload.New(output)}, nil
	}))

	t.Run("legacy token follows renamed and added policies", func(t *testing.T) {
		gotActor, scope, validateErr := live.Validate(ctx, unrestricted)
		require.NoError(t, validateErr)
		require.Equal(t, "active", gotActor.Meta["status"])
		require.Equal(t, security.Allow, scope.Evaluate(gotActor, "read", "retained", nil))
		require.Equal(t, security.Allow, scope.Evaluate(gotActor, "read", "added", nil))
		require.NotEqual(t, security.Allow, scope.Evaluate(gotActor, "read", "removed", nil))
		require.Positive(t, calls)
	})
	t.Run("stored narrowing intersects current authority", func(t *testing.T) {
		gotActor, scope, validateErr := live.Validate(ctx, token)
		require.NoError(t, validateErr)
		require.Equal(t, security.Allow, scope.Evaluate(gotActor, "read", "retained", nil))
		require.NotEqual(t, security.Allow, scope.Evaluate(gotActor, "read", "added", nil))
		require.True(t, scope.Contains(after.id))
		require.NotEqual(t, security.Allow, scope.With(added).Evaluate(gotActor, "read", "added", nil))
		require.NotEqual(t, security.Allow, scope.Without(after.id).Evaluate(gotActor, "read", "retained", nil))
		copied := securitysys.NewScope(scope.Policies())
		require.Equal(t, security.Allow, copied.Evaluate(gotActor, "read", "retained", nil))
		require.NotEqual(t, security.Allow, copied.Evaluate(gotActor, "read", "added", nil))

		groups = []string{}
		gotActor, scope, validateErr = live.Validate(ctx, token)
		require.NoError(t, validateErr)
		require.NotEqual(t, security.Allow, scope.Evaluate(gotActor, "read", "retained", nil))
		groups = []string{"app:current", "app:extra"}
	})
	t.Run("group removal revokes immediately", func(t *testing.T) {
		groups = []string{}
		gotActor, scope, validateErr := live.Validate(ctx, removalToken)
		require.NoError(t, validateErr)
		require.NotEqual(t, security.Allow, scope.Evaluate(gotActor, "read", "retained", nil))
		groups = []string{"app:current", "app:extra"}
	})
	t.Run("disabled and missing subjects deny", func(t *testing.T) {
		for _, kind := range []apierror.Kind{apierror.PermissionDenied, apierror.NotFound} {
			failure = map[string]any{"success": false, "subject_id": "member", "error": map[string]any{"message": "subject unavailable", "kind": kind}, "retriable": false}
			_, scope, validateErr := live.Validate(ctx, unrestricted)
			require.Error(t, validateErr)
			require.Nil(t, scope)
			var classified apierror.Error
			require.ErrorAs(t, validateErr, &classified)
			require.Equal(t, kind, classified.Kind())
		}
		failure = nil
	})
	t.Run("infrastructure errors propagate", func(t *testing.T) {
		lookupError = errors.New("database offline")
		_, scope, validateErr := live.Validate(ctx, unrestricted)
		require.ErrorIs(t, validateErr, lookupError)
		require.Nil(t, scope)
		lookupError = nil
	})
	t.Run("configured lookup absence fails closed", func(t *testing.T) {
		_, scope, validateErr := live.Validate(ctxapi.NewRootContext(), token)
		require.ErrorIs(t, validateErr, function.ErrRegistryNotFound)
		require.Nil(t, scope)
	})
	t.Run("malformed authority fails closed", func(t *testing.T) {
		t.Cleanup(func() { failure = nil })
		for _, response := range []any{
			map[string]any{"subject_id": "someone-else", "groups": []string{"app:current"}},
			map[string]any{"subject_id": "member"},
			map[string]any{"subject_id": "member", "groups": []string{"app:missing"}},
			map[string]any{"subject_id": "member", "groups": []string{"app:current"}, "ceiling": map[string]any{"policies": []string{"app:missing"}}},
		} {
			failure = response
			_, scope, validateErr := live.Validate(ctx, token)
			require.Error(t, validateErr)
			require.Nil(t, scope)
		}
	})
	t.Run("denials in either authority or ceiling take precedence", func(t *testing.T) {
		t.Cleanup(func() { failure = nil; delete(reg.groups, "app:denied") })
		deny := resourcePolicy{registry.NewID("app", "deny"), "retained", security.Deny}
		reg.policies["app:deny"] = deny
		reg.groups["app:denied"] = []security.Policy{after, deny}
		for _, response := range []any{
			map[string]any{"subject_id": "member", "groups": []string{"app:denied"}, "ceiling": map[string]any{"policies": []string{"app:ceiling"}}},
			map[string]any{"subject_id": "member", "groups": []string{"app:current"}, "ceiling": map[string]any{"policies": []string{"app:ceiling", "app:deny"}}},
		} {
			failure = response
			gotActor, scope, validateErr := live.Validate(ctx, token)
			require.NoError(t, validateErr)
			require.Equal(t, security.Deny, scope.Evaluate(gotActor, "read", "retained", nil))
		}
	})
	t.Run("empty ceiling denies all", func(t *testing.T) {
		t.Cleanup(func() { failure = nil })
		failure = map[string]any{"subject_id": "member", "groups": []string{"app:current"}, "ceiling": map[string]any{}}
		gotActor, scope, validateErr := live.Validate(ctx, token)
		require.NoError(t, validateErr)
		require.NotEqual(t, security.Allow, scope.Evaluate(gotActor, "read", "retained", nil))
	})
	t.Run("all HTTP token channels install current authority", func(t *testing.T) {
		resources.Register(registry.ParseID("app:live"), tokenProvider{live})
		middleware := tokenimpl.CreateTokenAuthMiddleware(map[string]string{tokenimpl.OptionTokenStore: "app:live"})
		for _, channel := range []string{"header", "query", "cookie", "websocket"} {
			requestCtx, frame := ctxapi.OpenFrameContext(ctx)
			request := httptest.NewRequestWithContext(requestCtx, http.MethodGet, "/", nil)
			switch channel {
			case "header", "websocket":
				request.Header.Set("Authorization", "Bearer "+string(unrestricted))
			case "query":
				query := request.URL.Query()
				query.Set("x-auth-token", string(unrestricted))
				request.URL.RawQuery = query.Encode()
			case "cookie":
				request.AddCookie(&http.Cookie{Name: "x-auth-token", Value: string(unrestricted)})
			}
			if channel == "websocket" {
				request.Header.Set("Upgrade", "websocket")
				request.Header.Set("Connection", "Upgrade")
			}
			admitted := false
			middleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				currentActor, exists := security.GetActor(r.Context())
				require.True(t, exists)
				scope, exists := security.GetScope(r.Context())
				require.True(t, exists)
				require.Equal(t, security.Allow, scope.Evaluate(currentActor, "read", "retained", nil))
				require.Equal(t, security.Allow, scope.Evaluate(currentActor, "read", "added", nil))
				admitted = true
			})).ServeHTTP(httptest.NewRecorder(), request)
			require.True(t, admitted, channel)
			ctxapi.ReleaseFrameContext(frame)
		}
	})
	t.Run("signature and revocation precede lookup", func(t *testing.T) {
		previous := calls
		_, _, validateErr := live.Validate(ctx, security.Token(string(unrestricted)+"bad"))
		require.ErrorIs(t, validateErr, security.ErrTokenInvalid)
		require.NoError(t, live.Revoke(ctx, unrestricted))
		_, _, validateErr = live.Validate(ctx, unrestricted)
		require.ErrorIs(t, validateErr, security.ErrTokenNotFound)
		require.Equal(t, previous, calls)
	})
	t.Run("validation preserves stored bytes", func(t *testing.T) {
		storedAfter, getErr := kv.Get(ctx, registry.ParseID(base))
		require.NoError(t, getErr)
		bytesAfter, marshalErr := json.Marshal(storedAfter.Data())
		require.NoError(t, marshalErr)
		require.Equal(t, bytesBefore, bytesAfter)
	})
	t.Run("no resolver retains issuance policy semantics", func(t *testing.T) {
		gotActor, scope, validateErr := ts.Validate(ctx, token)
		require.NoError(t, validateErr)
		require.Equal(t, actor.ID, gotActor.ID)
		require.Equal(t, actor.Meta["status"], gotActor.Meta["status"])
		require.Empty(t, scope.Policies())
	})
}

type tokenProvider struct{ store security.TokenStore }

func (p tokenProvider) Acquire(context.Context, registry.ID, resource.AccessMode) (resource.Resource[any], error) {
	return tokenResource(p), nil
}

type tokenResource struct{ store security.TokenStore }

func (r tokenResource) Get() (any, error) { return r.store, nil }
func (tokenResource) Release()            {}
