// SPDX-License-Identifier: MPL-2.0

package engine

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/code"
	"github.com/wippyai/runtime/runtime/lua/code/cache"
	"go.uber.org/zap"
)

func newCountingCodeManager(t *testing.T, typeCheck code.TypeCheckConfig) *code.Manager {
	t.Helper()
	cm, err := code.NewCodeManager(zap.NewNop(), nil, code.Config{
		TypeCheck: typeCheck,
		Cache: cache.Config{
			Dir:               t.TempDir(),
			Enabled:           true,
			CompileEnabled:    true,
			ToolchainIdentity: "deferred-compile-test",
		},
	})
	require.NoError(t, err)
	return cm
}

// compileReads counts compile-artifact lookups, one per node compilation that
// is not served by retained code.
func compileReads(cm *code.Manager) uint64 {
	stats := cm.CacheStats()
	return stats.CompileHits + stats.CompileMisses
}

func addDeferredTestNode(t *testing.T, cm *code.Manager, id registry.ID, source string) {
	t.Helper()
	require.NoError(t, cm.AddNode(context.Background(), code.Node{
		ID:     id,
		Kind:   luaapi.Process,
		Source: source,
	}, nil))
}

func updateDeferredTestNode(t *testing.T, cm *code.Manager, id registry.ID, source string) {
	t.Helper()
	require.NoError(t, cm.UpdateNode(context.Background(), code.Node{
		ID:     id,
		Source: source,
	}, nil))
}

func runFactoryResult(t *testing.T, factory process.FactoryFunc) string {
	t.Helper()
	created, err := factory()
	require.NoError(t, err)
	proc := created.(*Process)
	defer proc.Close()

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	require.NoError(t, proc.Init(ctx, "", nil))
	var out process.StepOutput
	for i := 0; i < 10; i++ {
		out.Reset()
		require.NoError(t, proc.Step(nil, &out))
		if out.Status() == process.StepDone {
			require.NotNil(t, out.Result())
			return fmt.Sprint(out.Result().Data())
		}
	}
	t.Fatal("process did not complete")
	return ""
}

func TestCompileOnFirstUse_UnusedEntryIsNotCompiled(t *testing.T) {
	cm := newCountingCodeManager(t, code.TypeCheckConfig{})
	id := registry.NewID("test.deferred", "unused")
	addDeferredTestNode(t, cm, id, `return "v1"`)

	_, err := NewProcessFactory(cm).CreateFactory(id, CompileOnFirstUse())
	require.NoError(t, err)

	assert.Zero(t, compileReads(cm), "an entry nothing runs must not compile")
}

func TestCreateFactory_CompilesAtCreationByDefault(t *testing.T) {
	cm := newCountingCodeManager(t, code.TypeCheckConfig{})
	id := registry.NewID("test.deferred", "eager")
	addDeferredTestNode(t, cm, id, `return "v1"`)

	_, err := NewProcessFactory(cm).CreateFactory(id)
	require.NoError(t, err)

	assert.Equal(t, uint64(1), compileReads(cm))
}

func TestCompileOnFirstUse_ConcurrentFirstUseCompilesOnce(t *testing.T) {
	cm := newCountingCodeManager(t, code.TypeCheckConfig{})
	id := registry.NewID("test.deferred", "concurrent")
	addDeferredTestNode(t, cm, id, `return "v1"`)

	factory, err := NewProcessFactory(cm).CreateFactory(id, CompileOnFirstUse())
	require.NoError(t, err)

	const callers = 32
	start := make(chan struct{})
	errs := make(chan error, callers)
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			proc, err := factory()
			if err == nil {
				proc.Close()
			}
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}

	assert.Equal(t, uint64(1), compileReads(cm))
	assert.Equal(t, "v1", runFactoryResult(t, factory))
	assert.Equal(t, uint64(1), compileReads(cm), "later uses reuse the first compilation")
}

func TestCompileOnFirstUse_UpdateBeforeFirstUseRunsNewCode(t *testing.T) {
	cm := newCountingCodeManager(t, code.TypeCheckConfig{})
	pf := NewProcessFactory(cm)
	id := registry.NewID("test.deferred", "update_before")
	addDeferredTestNode(t, cm, id, `return "v1"`)

	before, err := pf.CreateFactory(id, CompileOnFirstUse())
	require.NoError(t, err)

	updateDeferredTestNode(t, cm, id, `return "v2"`)
	after, err := pf.CreateFactory(id, CompileOnFirstUse())
	require.NoError(t, err)
	assert.Zero(t, compileReads(cm))

	assert.Equal(t, "v2", runFactoryResult(t, after))
	assert.Equal(t, "v2", runFactoryResult(t, before), "a generation compiled after the update never runs replaced code")
}

func TestCompileOnFirstUse_UpdateAfterFirstUseRunsNewCode(t *testing.T) {
	cm := newCountingCodeManager(t, code.TypeCheckConfig{})
	pf := NewProcessFactory(cm)
	id := registry.NewID("test.deferred", "update_after")
	addDeferredTestNode(t, cm, id, `return "v1"`)

	before, err := pf.CreateFactory(id, CompileOnFirstUse())
	require.NoError(t, err)
	assert.Equal(t, "v1", runFactoryResult(t, before))

	updateDeferredTestNode(t, cm, id, `return "v2"`)
	after, err := pf.CreateFactory(id, CompileOnFirstUse())
	require.NoError(t, err)

	assert.Equal(t, "v2", runFactoryResult(t, after))
	assert.Equal(t, uint64(2), compileReads(cm))
}

func TestCompileOnFirstUse_CompileErrorSurfacesAtFirstUse(t *testing.T) {
	const source = `goto nowhere`

	eagerCM := newCountingCodeManager(t, code.TypeCheckConfig{})
	eagerID := registry.NewID("test.deferred", "broken")
	addDeferredTestNode(t, eagerCM, eagerID, source)
	_, eagerErr := NewProcessFactory(eagerCM).CreateFactory(eagerID)
	require.Error(t, eagerErr)

	cm := newCountingCodeManager(t, code.TypeCheckConfig{})
	id := registry.NewID("test.deferred", "broken")
	addDeferredTestNode(t, cm, id, source)
	factory, err := NewProcessFactory(cm).CreateFactory(id, CompileOnFirstUse())
	require.NoError(t, err)

	for i := 0; i < 2; i++ {
		_, err = factory()
		require.Error(t, err)
		assert.Equal(t, eagerErr.Error(), err.Error())
		var apiErr apierror.Error
		require.True(t, errors.As(err, &apiErr), "first use reports the compile error type")
		assert.Equal(t, apierror.Internal, apiErr.Kind())
		assert.Equal(t, apierror.False, apiErr.Retryable())
	}
}

func TestCompileOnFirstUse_StrictTypecheckCompilesAtCreation(t *testing.T) {
	cm := newCountingCodeManager(t, code.TypeCheckConfig{Enabled: true, Strict: true})
	id := registry.NewID("test.deferred", "strict")
	addDeferredTestNode(t, cm, id, `local n: number = "text" return n`)

	_, err := NewProcessFactory(cm).CreateFactory(id, CompileOnFirstUse())
	require.ErrorContains(t, err, "type errors in", "strict type checking rejects code when its factory is created")
}
