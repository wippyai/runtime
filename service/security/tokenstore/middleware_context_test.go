// SPDX-License-Identifier: MPL-2.0

package tokenstore_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/resource"
	"github.com/wippyai/runtime/api/security"
	tokenimpl "github.com/wippyai/runtime/service/security/tokenstore"
	securitysys "github.com/wippyai/runtime/system/security"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

type middlewareTokenStore struct {
	security.TokenStore
	validate func(context.Context) (security.Actor, security.Scope, error)
}

func (s middlewareTokenStore) Validate(ctx context.Context, _ security.Token) (security.Actor, security.Scope, error) {
	return s.validate(ctx)
}

type middlewareTokenProvider struct {
	acquire func() (resource.Resource[any], error)
}

func (p middlewareTokenProvider) Acquire(context.Context, registry.ID, resource.AccessMode) (resource.Resource[any], error) {
	return p.acquire()
}

type middlewareTokenResource struct {
	get      func() (any, error)
	releases int
}

func (r *middlewareTokenResource) Get() (any, error) { return r.get() }
func (r *middlewareTokenResource) Release()          { r.releases++ }

func TestTokenAuthMiddlewareDoneContext(t *testing.T) {
	for _, stage := range []string{"before", "deadline", "without_token", "acquire", "acquire_success", "resource", "resource_success", "validate", "valid_token"} {
		t.Run(stage, func(t *testing.T) {
			core, observed := observer.New(zap.DebugLevel)
			ctx := logs.WithLogger(ctxapi.NewRootContext(), zap.New(core))
			var cancel context.CancelFunc
			if stage == "deadline" {
				ctx, cancel = context.WithDeadline(ctx, time.Now().Add(-time.Second))
			} else {
				ctx, cancel = context.WithCancel(ctx)
			}
			defer cancel()
			if stage == "before" || stage == "without_token" {
				cancel()
			}
			resources := newTestResourceRegistry()
			res := &middlewareTokenResource{}
			res.get = func() (any, error) {
				require.NoError(t, ctx.Err(), "cancellation stops before reading the resource")
				if stage == "resource_success" {
					cancel()
				}
				if stage == "resource" {
					cancel()
					return nil, ctx.Err()
				}
				return middlewareTokenStore{validate: func(context.Context) (security.Actor, security.Scope, error) {
					require.NoError(t, ctx.Err(), "cancellation stops before validation")
					cancel()
					if stage == "valid_token" {
						return security.Actor{ID: "member"}, securitysys.NewScope(nil), nil
					}
					return security.Actor{}, nil, ctx.Err()
				}}, nil
			}
			resources.Register(registry.ParseID("test:auth"), middlewareTokenProvider{acquire: func() (resource.Resource[any], error) {
				if stage == "acquire_success" {
					cancel()
				}
				if stage == "acquire" {
					cancel()
					return nil, ctx.Err()
				}
				return res, nil
			}})
			ctx = resource.WithRegistry(ctx, resources)
			req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/", nil)
			if stage != "without_token" {
				req.Header.Set("Authorization", "Bearer token")
			}
			called := false
			recorder := httptest.NewRecorder()
			middleware := tokenimpl.CreateTokenAuthMiddleware(map[string]string{tokenimpl.OptionTokenStore: "test:auth"})
			middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true })).ServeHTTP(recorder, req)
			require.False(t, called, "a finished request does not reach the next handler")
			require.Empty(t, recorder.Body.String())
			require.False(t, recorder.Flushed)
			require.Empty(t, observed.All(), "cancellation is not an authentication failure")
			if stage == "acquire_success" || stage == "resource_success" || stage == "resource" || stage == "validate" || stage == "valid_token" {
				require.Equal(t, 1, res.releases)
			}
		})
	}
}

func TestTokenAuthMiddlewareStoreFault(t *testing.T) {
	for _, stage := range []string{"acquire", "resource", "validate"} {
		t.Run(stage, func(t *testing.T) {
			fault := errors.New("store unavailable")
			core, observed := observer.New(zap.DebugLevel)
			ctx := logs.WithLogger(ctxapi.NewRootContext(), zap.New(core))
			resources := newTestResourceRegistry()
			res := &middlewareTokenResource{get: func() (any, error) {
				if stage == "resource" {
					return nil, fault
				}
				return middlewareTokenStore{validate: func(context.Context) (security.Actor, security.Scope, error) { return security.Actor{}, nil, fault }}, nil
			}}
			resources.Register(registry.ParseID("test:auth"), middlewareTokenProvider{acquire: func() (resource.Resource[any], error) {
				if stage == "acquire" {
					return nil, fault
				}
				return res, nil
			}})
			ctx = resource.WithRegistry(ctx, resources)
			req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/", nil)
			req.Header.Set("Authorization", "Bearer token")
			called := false
			middleware := tokenimpl.CreateTokenAuthMiddleware(map[string]string{tokenimpl.OptionTokenStore: "test:auth"})
			middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				_, hasActor := security.GetActor(r.Context())
				require.False(t, hasActor)
				w.WriteHeader(http.StatusNoContent)
			})).ServeHTTP(httptest.NewRecorder(), req)
			require.True(t, called, "genuine faults preserve unauthenticated pass-through")
			entries := observed.All()
			require.Len(t, entries, 1)
			level := zap.ErrorLevel
			if stage == "validate" {
				level = zap.DebugLevel
			}
			require.Equal(t, level, entries[0].Level)
			require.Equal(t, fault.Error(), entries[0].ContextMap()["error"])
			if stage != "acquire" {
				require.Equal(t, 1, res.releases)
			}
		})
	}
}
