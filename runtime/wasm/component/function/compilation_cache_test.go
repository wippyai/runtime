// SPDX-License-Identifier: MPL-2.0

package function

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tetratelabs/wazero"
	wasmcomponent "github.com/wippyai/runtime/runtime/wasm/component"
	"go.uber.org/zap"
)

func TestFunctionManager_RuntimeConfigCarriesManagerCache(t *testing.T) {
	rawCache, err := wasmcomponent.InMemoryCompilationCache()
	require.NoError(t, err)

	m := NewManager(zap.NewNop(), nil, nil, nil, func() (wazero.CompilationCache, error) {
		return rawCache, nil
	})

	assert.Nil(t, m.cache)

	ctx := context.Background()
	require.NoError(t, m.Start(ctx))
	require.NotNil(t, m.cache)
	assert.Same(t, rawCache, m.cache)

	// Core, component, and isolated worker configs are all built through m.runtimeConfig().
	cfg := m.runtimeConfig()
	require.NotNil(t, cfg)
	assert.Same(t, rawCache, cfg.CompilationCache)
	assert.True(t, cfg.CloseOnContextDone)

	m.Stop()
	assert.Nil(t, m.cache)
}
