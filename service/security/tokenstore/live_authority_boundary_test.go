// SPDX-License-Identifier: MPL-2.0

package tokenstore_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/attrs"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/function"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	runtimeapi "github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/api/security"
	tokenapi "github.com/wippyai/runtime/api/service/security/tokenstore"
	storeapi "github.com/wippyai/runtime/api/store"
	luapayload "github.com/wippyai/runtime/runtime/lua/engine/payload"
	tokenimpl "github.com/wippyai/runtime/service/security/tokenstore"
	memorystore "github.com/wippyai/runtime/service/store/memory"
	"github.com/wippyai/runtime/service/temporal/propagator"
	systempayload "github.com/wippyai/runtime/system/payload"
	jsonpayload "github.com/wippyai/runtime/system/payload/json"
	securitysys "github.com/wippyai/runtime/system/security"
	commonpb "go.temporal.io/api/common/v1"
	"go.temporal.io/sdk/converter"
	"go.uber.org/zap"
)

func authorityBoundaryFixture(t *testing.T, dtt payload.Transcoder) (context.Context, *tokenimpl.TokenStore, security.Token, *testSecurityRegistry, *memorystore.Store) {
	t.Helper()
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
	read := resourcePolicy{registry.NewID("app", "read"), "records", security.Allow}
	write := resourcePolicy{registry.NewID("app", "write"), "admin", security.Allow}
	reg.policies[read.id.String()] = read
	reg.policies[write.id.String()] = write
	reg.groups["app:current"] = []security.Policy{read, write}
	store, err := tokenimpl.NewStoreTokenStore(&tokenapi.Config{
		Store: storeID, TokenLength: 32, SubjectLookup: registry.NewID("app", "lookup"),
	}, dtt, resources, reg)
	require.NoError(t, err)
	token, err := store.Create(ctx, security.Actor{ID: "member"}, securitysys.NewScope([]security.Policy{read}), security.TokenDetails{})
	require.NoError(t, err)
	return ctx, store, token, reg, kv
}

func TestLiveAuthorityLuaObjectFields(t *testing.T) {
	for _, tc := range []struct {
		name, response   string
		allow, wantError bool
	}{
		{"nonempty_metadata", `{subject_id="member", meta={status="active"}, groups={"app:current"}}`, true, false},
		{"empty_metadata", `{subject_id="member", meta={}, groups={"app:current"}}`, true, false},
		{"empty_membership", `{subject_id="member", groups={}}`, false, false},
		{"empty_ceiling", `{subject_id="member", groups={"app:current"}, ceiling={}}`, false, false},
		{"empty_ceiling_arrays", `{subject_id="member", groups={"app:current"}, ceiling={groups={}, policies={}}}`, false, false},
		{"metadata_array", `{subject_id="member", meta={"not an object"}, groups={"app:current"}}`, false, true},
		{"metadata_scalar", `{subject_id="member", meta=42, groups={"app:current"}}`, false, true},
		{"ceiling_array", `{subject_id="member", groups={"app:current"}, ceiling={"app:read"}}`, false, true},
		{"ceiling_scalar", `{subject_id="member", groups={"app:current"}, ceiling=true}`, false, true},
		{"missing_membership", `{subject_id="member", meta={}}`, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dtt := systempayload.NewTranscoder()
			jsonpayload.Register(dtt)
			luapayload.Register(dtt)
			ctx, store, token, _, _ := authorityBoundaryFixture(t, dtt)
			l := lua.NewState()
			defer l.Close()
			require.NoError(t, l.DoString("response = "+tc.response))
			response := l.GetGlobal("response")
			ctx = function.WithRegistry(ctx, lookupFunc(func(context.Context, runtimeapi.Task) (*runtimeapi.Result, error) {
				return &runtimeapi.Result{Value: payload.NewPayload(response, payload.Lua)}, nil
			}))
			actor, scope, err := store.Validate(ctx, token)
			if tc.wantError {
				require.Error(t, err)
				require.Nil(t, scope, "malformed authority must not grant a partial scope")
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.allow, scope.Evaluate(actor, "read", "records", nil) == security.Allow)
			require.Equal(t, tc.allow, scope.Evaluate(actor, "read", "admin", nil) == security.Allow)
			if tc.name == "empty_metadata" {
				require.Empty(t, actor.Meta)
			}
		})
	}
}

func TestLiveAuthorityRejectsIDOnlyBoundaries(t *testing.T) {
	ctx, store, token, _, kv := authorityBoundaryFixture(t, &jsonTranscoder{})
	ctx = function.WithRegistry(ctx, lookupFunc(func(context.Context, runtimeapi.Task) (*runtimeapi.Result, error) {
		return &runtimeapi.Result{Value: payload.New(attrs.Bag{
			"subject_id": "member", "groups": []string{"app:current"},
			"ceiling": attrs.Bag{"policies": []string{"app:read"}},
		})}, nil
	}))
	actor, scope, err := store.Validate(ctx, token)
	require.NoError(t, err)
	require.Equal(t, security.Allow, scope.Evaluate(actor, "read", "records", nil))
	require.NotEqual(t, security.Allow, scope.Evaluate(actor, "read", "admin", nil))

	for _, tc := range []struct {
		scope security.Scope
		name  string
	}{
		{scope, "restricted"},
		{securitysys.NewScope(scope.Policies()), "copied_policies"},
		{scope.Without(registry.NewID("app", "read")).Without(registry.NewID("app", "write")), "empty_restricted"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			taskCtx, frame := ctxapi.OpenFrameContext(ctx)
			defer ctxapi.ReleaseFrameContext(frame)
			require.NoError(t, security.SetActor(taskCtx, actor))
			require.NoError(t, security.SetScope(taskCtx, tc.scope))
			taskCtx = propagator.WithSecurityAudience(taskCtx, "test-workflow")
			claim, err := propagator.ExtractSecurityPayload(taskCtx)
			require.ErrorIs(t, err, security.ErrPolicyNotReferenceable)
			require.Nil(t, claim)

			writer := &authorityHeaderWriter{fields: make(map[string]*commonpb.Payload)}
			err = propagator.New(converter.GetDefaultDataConverter(), []byte("authority-test-key-0123456789abcd")).Inject(
				propagator.WithValues(taskCtx, map[string]any{"ordinary": "value"}), writer)
			require.ErrorIs(t, err, security.ErrPolicyNotReferenceable)
			require.Empty(t, writer.fields, "rejected security must not emit partial headers")

			before, err := kv.List(ctx, storeapi.ListOptions{})
			require.NoError(t, err)
			minted, err := store.Create(ctx, actor, tc.scope, security.TokenDetails{})
			require.ErrorIs(t, err, security.ErrPolicyNotReferenceable)
			require.Empty(t, minted)
			after, err := kv.List(ctx, storeapi.ListOptions{})
			require.NoError(t, err)
			require.Equal(t, before, after, "rejected mint must not write a token")
		})
	}
}

type authorityHeaderWriter struct {
	fields map[string]*commonpb.Payload
}

func (w *authorityHeaderWriter) Set(key string, value *commonpb.Payload) {
	w.fields[key] = value
}

func TestLiveAuthorityUnrestrictedSignedRoundTrip(t *testing.T) {
	ctx, store, token, reg, _ := authorityBoundaryFixture(t, &jsonTranscoder{})
	ctx = function.WithRegistry(ctx, lookupFunc(func(context.Context, runtimeapi.Task) (*runtimeapi.Result, error) {
		return &runtimeapi.Result{Value: payload.New(attrs.Bag{
			"subject_id": "member", "groups": []string{"app:current"},
		})}, nil
	}))
	actor, scope, err := store.Validate(ctx, token)
	require.NoError(t, err)
	ctx, frame := ctxapi.OpenFrameContext(ctx)
	defer ctxapi.ReleaseFrameContext(frame)
	require.NoError(t, security.SetActor(ctx, actor))
	require.NoError(t, security.SetScope(ctx, scope))
	ctx = propagator.WithSecurityAudience(ctx, "test-workflow")
	claim, err := propagator.ExtractSecurityPayload(ctx)
	require.NoError(t, err)
	key := []byte("authority-test-key-0123456789abcde")
	dc := converter.GetDefaultDataConverter()
	header, err := propagator.AddSecurityToHeader(dc, nil, claim, key)
	require.NoError(t, err)
	verified, err := propagator.ExtractSecurityFromHeader(dc, header, "test-workflow", key)
	require.NoError(t, err)
	destination := security.WithRegistry(ctxapi.NewRootContext(), reg)
	destination, destinationFrame := ctxapi.OpenFrameContext(destination)
	defer ctxapi.ReleaseFrameContext(destinationFrame)
	require.NoError(t, propagator.ApplySecurityPayload(destination, verified))
	restored, ok := security.GetScope(destination)
	require.True(t, ok)
	restoredActor, ok := security.GetActor(destination)
	require.True(t, ok)
	require.Equal(t, actor, restoredActor)
	require.Equal(t, security.Allow, restored.Evaluate(actor, "read", "records", nil))
	require.Equal(t, security.Allow, restored.Evaluate(actor, "read", "admin", nil))
	minted, err := store.Create(ctx, actor, scope, security.TokenDetails{})
	require.NoError(t, err)
	require.NotEmpty(t, minted, "unrestricted policy-ID scopes still support minting")
}
