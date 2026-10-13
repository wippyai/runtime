// SPDX-License-Identifier: MPL-2.0

package code

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	glua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/registry"
	api "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/code/cache"
	"go.uber.org/zap"
)

func TestGotoCaptureAfterToolchainUpgrade(t *testing.T) {
	const source = `
local content = ""
local function current() return content end
for _, chunk in ipairs({ "", "Hello" }) do
    if chunk == "" then goto continue end
    content = content .. chunk
    ::continue::
end
return current()
`
	dir := t.TempDir()
	node := Node{ID: registry.NewID("test.goto", "capture"), Kind: api.Function, Source: source}
	newManager := func(identity string) *Manager {
		cm, err := NewCodeManager(zap.NewNop(), &testEventBus{}, Config{
			Cache: cache.Config{
				Dir: dir, Enabled: true, CompileEnabled: true,
				ToolchainIdentity: identity,
			},
		})
		require.NoError(t, err)
		require.NoError(t, cm.AddNode(context.Background(), node, nil))
		return cm
	}
	run := func(cm *Manager) string {
		compiled, err := cm.Compile(node.ID, nil)
		require.NoError(t, err)
		state := glua.NewState()
		defer state.Close()
		state.Push(state.NewFunctionFromProto(compiled.Main))
		require.NoError(t, state.PCall(0, 1, nil))
		return state.Get(-1).String()
	}

	old := newManager("go-lua-v1.6.5")
	oldFingerprint, _, err := old.compileFingerprint(node.ID)
	require.NoError(t, err)
	// A sentinel stands in for an artifact emitted by the old compiler.
	stale, err := glua.CompileString(`return "stale compiler artifact"`, node.ID.String())
	require.NoError(t, err)
	old.saveCompileCache(&node, oldFingerprint, nil, stale)

	warm := newManager("go-lua-v1.6.5")
	require.Equal(t, "stale compiler artifact", run(warm))
	require.Equal(t, uint64(1), warm.compileCacheHits.Load())

	upgraded := newManager("go-lua-v1.6.6")
	require.Equal(t, "Hello", run(upgraded))
	require.Zero(t, upgraded.compileCacheHits.Load())
	require.Equal(t, uint64(1), upgraded.compileCacheMisses.Load())

	// The freshly compiled result must also survive a warm restart.
	restarted := newManager("go-lua-v1.6.6")
	require.Equal(t, "Hello", run(restarted))
	require.Equal(t, uint64(1), restarted.compileCacheHits.Load())
}
