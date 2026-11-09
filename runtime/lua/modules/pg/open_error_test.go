// SPDX-License-Identifier: MPL-2.0

package pg

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	ctxapi "github.com/wippyai/runtime/api/context"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/resource"
	runtimeapi "github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/api/security"
	runtimelua "github.com/wippyai/runtime/runtime/lua"
	syspg "github.com/wippyai/runtime/service/pg"
	"go.uber.org/zap"
)

// providerRegistry serves each registered id through its resource.Provider,
// the way the system resource registry hands acquires to the pg service.
type providerRegistry struct {
	providers map[string]resource.Provider
}

func (r *providerRegistry) Acquire(ctx context.Context, id registry.ID, mode resource.AccessMode) (resource.Resource[any], error) {
	p, ok := r.providers[id.String()]
	if !ok {
		return nil, resource.ErrNotFound
	}
	return p.Acquire(ctx, id, mode)
}

func (r *providerRegistry) List() ([]registry.ID, error) {
	ids := make([]registry.ID, 0, len(r.providers))
	for id := range r.providers {
		ids = append(ids, registry.ParseID(id))
	}
	return ids, nil
}

func (r *providerRegistry) Exists(id registry.ID) bool {
	_, ok := r.providers[id.String()]
	return ok
}

// acquireFailureProvider rejects every acquire with a fixed cause.
type acquireFailureProvider struct{ err error }

func (p acquireFailureProvider) Acquire(context.Context, registry.ID, resource.AccessMode) (resource.Resource[any], error) {
	return nil, p.err
}

func newScopeService() *syspg.Service {
	return syspg.NewService(zap.NewNop(), "test:pg", nil, nil, nil, nil, nil, "local-node", nil, nil, nil)
}

func newLuaWithProviders(t *testing.T, providers map[string]resource.Provider) *lua.LState {
	t.Helper()
	lua.SetErrorMetadataExtractor(runtimelua.ErrorMetadata)
	t.Cleanup(func() { lua.SetErrorMetadataExtractor(nil) })

	l := lua.NewState()
	t.Cleanup(l.Close)
	bindPG(l)

	ctx := ctxapi.NewRootContext()
	security.SetStrictMode(ctx, false)
	resource.WithRegistry(ctx, &providerRegistry{providers: providers})

	ctx, fc := ctxapi.OpenFrameContext(ctx)
	t.Cleanup(func() { ctxapi.ReleaseFrameContext(fc) })
	require.NoError(t, runtimeapi.SetFramePID(ctx, pid.PID{Host: "h1", UniqID: "p1"}))
	l.SetContext(ctx)
	return l
}

func openScope(t *testing.T, l *lua.LState, id string) (lua.LValue, *lua.Error) {
	t.Helper()
	l.Push(l.GetGlobal("pg").(*lua.LTable).RawGetString("open"))
	l.Push(lua.LString(id))
	require.NoError(t, l.PCall(1, 2, nil))
	inst, errVal := l.Get(-2), l.Get(-1)
	l.Pop(2)
	if errVal == lua.LNil {
		return inst, nil
	}
	luaErr, ok := errVal.(*lua.Error)
	require.True(t, ok, "pg.open returns a structured error, got %T", errVal)
	return inst, luaErr
}

func TestOpenBeforeServiceStartIsRetryableUnavailable(t *testing.T) {
	svc := newScopeService()
	l := newLuaWithProviders(t, map[string]resource.Provider{"test:pg": svc})

	inst, err := openScope(t, l, "test:pg")
	assert.Equal(t, lua.LNil, inst)
	require.NotNil(t, err)
	assert.Equal(t, lua.Unavailable, err.Kind())
	assert.Equal(t, lua.TernaryTrue, err.Retryable())
	assert.Equal(t, "failed to acquire pg scope: pg: service not started", err.String())
	assert.ErrorIs(t, err, syspg.ErrServiceNotStarted)
}

func TestOpenAfterServiceStopIsRetryableUnavailable(t *testing.T) {
	svc := newScopeService()
	_, startErr := svc.Start(context.Background())
	require.NoError(t, startErr)
	require.NoError(t, svc.Stop(context.Background()))
	l := newLuaWithProviders(t, map[string]resource.Provider{"test:pg": svc})

	inst, err := openScope(t, l, "test:pg")
	assert.Equal(t, lua.LNil, inst)
	require.NotNil(t, err)
	assert.Equal(t, lua.Unavailable, err.Kind())
	assert.Equal(t, lua.TernaryTrue, err.Retryable())
	assert.Equal(t, "failed to acquire pg scope: pg: service stopped", err.String())
	assert.ErrorIs(t, err, syspg.ErrServiceStopped)
}

func TestOpenSucceedsOnceServiceStarts(t *testing.T) {
	svc := newScopeService()
	l := newLuaWithProviders(t, map[string]resource.Provider{"test:pg": svc})

	_, err := openScope(t, l, "test:pg")
	require.NotNil(t, err)
	require.Equal(t, lua.TernaryTrue, err.Retryable())

	_, startErr := svc.Start(context.Background())
	require.NoError(t, startErr)
	t.Cleanup(func() { _ = svc.Stop(context.Background()) })

	inst, err := openScope(t, l, "test:pg")
	require.Nil(t, err)
	assert.Equal(t, lua.LTUserData, inst.Type())
}

func TestOpenUnknownScopeIsNonRetryableNotFound(t *testing.T) {
	l := newLuaWithProviders(t, map[string]resource.Provider{})

	inst, err := openScope(t, l, "missing:pg")
	assert.Equal(t, lua.LNil, inst)
	require.NotNil(t, err)
	assert.Equal(t, lua.NotFound, err.Kind())
	assert.Equal(t, lua.TernaryFalse, err.Retryable())
	assert.Equal(t, "failed to acquire pg scope: resource not found", err.String())
}

func TestOpenUnclassifiedAcquireFailureIsNonRetryableInternal(t *testing.T) {
	cause := context.Canceled
	l := newLuaWithProviders(t, map[string]resource.Provider{"test:pg": acquireFailureProvider{err: cause}})

	_, err := openScope(t, l, "test:pg")
	require.NotNil(t, err)
	assert.Equal(t, lua.Internal, err.Kind())
	assert.Equal(t, lua.TernaryFalse, err.Retryable())
	assert.ErrorIs(t, err, cause)
}

func TestOpenAcquireFailureKeepsDeclaredClassification(t *testing.T) {
	cause := apierror.New(apierror.PermissionDenied, "scope sealed").WithRetryable(apierror.False)
	l := newLuaWithProviders(t, map[string]resource.Provider{"test:pg": acquireFailureProvider{err: cause}})

	_, err := openScope(t, l, "test:pg")
	require.NotNil(t, err)
	assert.Equal(t, lua.PermissionDenied, err.Kind())
	assert.Equal(t, lua.TernaryFalse, err.Retryable())
}
