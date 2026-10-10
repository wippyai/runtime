// SPDX-License-Identifier: MPL-2.0

package tokenstore_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/function"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/resource"
	runtimeapi "github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/api/security"
	httpapi "github.com/wippyai/runtime/api/service/http"
	tokenimpl "github.com/wippyai/runtime/service/security/tokenstore"
)

type middlewareFrameCloser struct{ closes int }

func (c *middlewareFrameCloser) Close() error {
	c.closes++
	return nil
}

func TestTokenAuthMiddlewareFrameOwnership(t *testing.T) {
	for _, tc := range []struct {
		name           string
		requestFrame   bool
		sealed         bool
		validationFork bool
	}{
		{name: "writable", requestFrame: true},
		{name: "validation_fork", requestFrame: true, validationFork: true},
		{name: "sealed", requestFrame: true, sealed: true},
		{name: "no_frame"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, store, token, _, _ := authorityBoundaryFixture(t, &jsonTranscoder{})
			resources := newTestResourceRegistry()
			resources.Register(registry.NewID("app", "auth"), tokenProvider{store})
			ctx = resource.WithRegistry(ctx, resources)
			validated := false
			ctx = function.WithRegistry(ctx, lookupFunc(func(callCtx context.Context, _ runtimeapi.Task) (*runtimeapi.Result, error) {
				validated = true
				if tc.validationFork {
					_, frame := ctxapi.ForkFrameContext(callCtx)
					defer ctxapi.ReleaseFrameContext(frame)
					require.True(t, ctxapi.FrameFromContext(callCtx).IsSealed())
				}
				return &runtimeapi.Result{Value: payload.New(map[string]any{
					"subject_id": "member", "groups": []string{"app:current"},
				})}, nil
			}))

			metadataKey := &ctxapi.Key{Name: "request.metadata", Execution: true}
			server := &middlewareFrameCloser{}
			requestKeys := []*ctxapi.Key{httpapi.RequestKey(), httpapi.ServerIDKey()}
			route := &httpapi.RouteInfo{Endpoint: registry.NewID("app", "endpoint")}
			var requestFrame ctxapi.FrameContext
			if tc.requestFrame {
				ctx, requestFrame = ctxapi.OpenFrameContext(ctx)
				defer ctxapi.ReleaseFrameContext(requestFrame)
				require.NoError(t, requestFrame.Set(metadataKey, "request"))
				require.NoError(t, requestFrame.Set(httpapi.ServerKey(), server))
				require.NoError(t, httpapi.SetRouteInfo(ctx, route))
				require.NoError(t, httpapi.SetRouteLabel(ctx, "endpoint"))
				require.NoError(t, httpapi.SetServerHost(ctx, "localhost"))
				for _, key := range requestKeys {
					require.NoError(t, requestFrame.Set(key, key.Name))
				}
				if tc.sealed {
					requestFrame.Seal()
				}
			}
			request := httptest.NewRequestWithContext(ctx, http.MethodGet, "/", nil)
			request.Header.Set("Authorization", "Bearer "+string(token))
			closer := &middlewareFrameCloser{}
			called := false
			middleware := tokenimpl.CreateTokenAuthMiddleware(map[string]string{tokenimpl.OptionTokenStore: "app:auth"})
			middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				require.True(t, validated)
				actor, ok := security.GetActor(r.Context())
				require.True(t, ok, "validated actor is visible to the next handler")
				require.Equal(t, "member", actor.ID)
				scope, ok := security.GetScope(r.Context())
				require.True(t, ok, "validated scope is visible to the next handler")
				require.Equal(t, security.Allow, scope.Evaluate(actor, "read", "records", nil))
				frame := ctxapi.FrameFromContext(r.Context())
				require.NoError(t, frame.Set(&ctxapi.Key{Name: "handler.resource"}, closer))
				if tc.requestFrame {
					value, exists := frame.Get(metadataKey)
					require.True(t, exists)
					require.Equal(t, "request", value)
					value, exists = frame.Get(httpapi.ServerKey())
					require.True(t, exists, "relay server remains visible")
					require.Same(t, server, value)
					gotRoute, exists := httpapi.GetRouteInfo(r.Context())
					require.True(t, exists)
					require.Same(t, route, gotRoute)
					label, exists := httpapi.GetRouteLabel(r.Context())
					require.True(t, exists)
					require.Equal(t, "endpoint", label)
					for _, key := range requestKeys {
						value, exists = frame.Get(key)
						require.True(t, exists, key.Name)
						require.Equal(t, key.Name, value)
					}
					_, child := ctxapi.ForkFrameContext(r.Context())
					defer ctxapi.ReleaseFrameContext(child)
					for _, key := range append(requestKeys, httpapi.ServerKey(), metadataKey) {
						_, exists = child.Get(key)
						require.False(t, exists, "request metadata does not leak into function frames")
					}
				}
				w.WriteHeader(http.StatusOK)
				w.(http.Flusher).Flush()
				require.Zero(t, closer.closes, "frame remains alive while the response streams")
			})).ServeHTTP(httptest.NewRecorder(), request)
			require.True(t, called)
			if tc.name == "writable" {
				require.Zero(t, closer.closes, "middleware does not release the server-owned frame")
			} else {
				require.Equal(t, 1, closer.closes, "middleware releases its owned frame after the handler returns")
			}
			if tc.requestFrame {
				require.Zero(t, server.closes, "continuation leaves the server handle owned by the request frame")
				value, exists := requestFrame.Get(metadataKey)
				require.True(t, exists, "server-owned frame remains alive")
				require.Equal(t, "request", value)
				ctxapi.ReleaseFrameContext(requestFrame)
				require.Equal(t, 1, server.closes)
			}
			require.Equal(t, 1, closer.closes)
		})
	}
}
