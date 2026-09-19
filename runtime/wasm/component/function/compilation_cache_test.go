// SPDX-License-Identifier: MPL-2.0

package function

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tetratelabs/wazero"
	wasmcomponent "github.com/wippyai/runtime/runtime/wasm/component"
	"github.com/wippyai/wasm-runtime/asyncify"
	"go.uber.org/zap"
)

func TestFunctionManager_RuntimeConfigCarriesManagerCache(t *testing.T) {
	rawCompCache, err := wasmcomponent.InMemoryCompilationCache()
	require.NoError(t, err)
	rawTransCache := asyncify.NewMemoryTransformCache()

	caches := wasmcomponent.Caches{
		Compilation: func() (wazero.CompilationCache, error) {
			return rawCompCache, nil
		},
		Transform: rawTransCache,
	}

	m := NewManager(zap.NewNop(), nil, nil, nil, caches)

	assert.Nil(t, m.cache)

	ctx := context.Background()
	require.NoError(t, m.Start(ctx))
	require.NotNil(t, m.cache)
	assert.Same(t, rawCompCache, m.cache)

	// Core, component, and isolated worker configs are all built through m.runtimeConfig().
	cfg := m.runtimeConfig()
	require.NotNil(t, cfg)
	assert.Same(t, rawCompCache, cfg.CompilationCache)
	assert.Same(t, rawTransCache, cfg.TransformCache)
	assert.True(t, cfg.CloseOnContextDone)

	m.Stop()
	assert.Nil(t, m.cache)
}

func TestFunctionManager_RuntimeConfigCarriesBothCaches(t *testing.T) {
	rawCompCache, err := wasmcomponent.InMemoryCompilationCache()
	require.NoError(t, err)
	rawTransCache := asyncify.NewMemoryTransformCache()

	caches := wasmcomponent.Caches{
		Compilation: func() (wazero.CompilationCache, error) {
			return rawCompCache, nil
		},
		Transform: rawTransCache,
	}

	m := NewManager(zap.NewNop(), nil, nil, nil, caches)
	require.NoError(t, m.Start(context.Background()))
	defer m.Stop()

	// Core, component, and isolated worker runtimes all receive m.runtimeConfig().
	cfg := m.runtimeConfig()
	require.NotNil(t, cfg)
	assert.Same(t, rawCompCache, cfg.CompilationCache)
	assert.Same(t, rawTransCache, cfg.TransformCache)
	assert.True(t, cfg.CloseOnContextDone)
}
