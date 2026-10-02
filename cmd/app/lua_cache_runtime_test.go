// SPDX-License-Identifier: MPL-2.0

package app

import (
	"context"
	"encoding/hex"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/registry"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/code"
	"github.com/wippyai/runtime/runtime/lua/code/cache"
	"go.uber.org/zap"
)

func TestEmbeddedLuaCacheRuntimeHitsAndMismatchCompiles(t *testing.T) {
	schema, toolchain, err := LuaCacheIdentity()
	require.NoError(t, err)
	cfg := code.Config{Cache: cache.Config{Dir: t.TempDir(), Enabled: true, CompileEnabled: true, TypecheckEnabled: true, ToolchainIdentity: toolchain}, TypeCheck: code.TypeCheckConfig{Enabled: true, Strict: true}}
	original, err := code.NewCodeManager(zap.NewNop(), nil, cfg)
	require.NoError(t, err)
	id := registry.NewID("app", "main")
	node := code.Node{ID: id, Kind: luaapi.Function, Source: "return 1", Method: "main"}
	require.NoError(t, original.AddNode(nil, node, nil))
	_, err = original.Compile(id, nil)
	require.NoError(t, err)
	archive := makeLuaCacheArchive(t, cfg.Cache.Dir)
	seed := LuaCacheSeed{Archive: archive, Digest: "sha256:" + hex.EncodeToString(sha256Bytes(archive)), SchemaVersion: schema, ToolchainIdentity: toolchain}
	embedded, err := seedLuaCache(&seed)
	require.NoError(t, err)
	for _, changed := range []bool{false, true} {
		cfg.Cache.Dir = t.TempDir()
		cfg.EmbeddedCache = embedded
		cm, err := code.NewCodeManager(zap.NewNop(), nil, cfg)
		require.NoError(t, err)
		if changed {
			node.Source = "return 2"
		}
		require.NoError(t, cm.AddNode(context.Background(), node, nil))
		_, err = cm.Compile(id, nil)
		require.NoError(t, err)
		stats := cm.CacheStats()
		if changed {
			require.Positive(t, stats.CompileMisses)
			require.Positive(t, stats.TypecheckMisses)
		} else {
			require.Positive(t, stats.CompileHits)
			require.Positive(t, stats.TypecheckHits)
			require.Zero(t, stats.CompileMisses)
			require.Zero(t, stats.TypecheckMisses)
			require.NoDirExists(t, filepath.Join(cfg.Cache.Dir, "v1", "entries"))
		}
	}
}

func TestRunnerUsesEmbeddedCacheAndContinuesOnSeedMismatch(t *testing.T) {
	for _, mismatch := range []bool{false, true} {
		t.Run(map[bool]string{false: "verified", true: "mismatch"}[mismatch], func(t *testing.T) {
			record := captureExecution(t)
			e := runnableExecutable(t)
			seed, key, _ := testLuaCacheSeed(t)
			if mismatch {
				seed.Digest = "wrong"
			}
			e.LuaCacheSeed = &seed
			state := filepath.Join(t.TempDir(), "state")
			require.NoError(t, Run(context.Background(), e, []string{"--state", state, "run"}))
			require.Equal(t, 1, record.calls)
			raw, present := record.options.Overrides.Get("lua.cache.embedded")
			if mismatch {
				require.False(t, present)
			} else {
				require.True(t, present)
				store, ok := raw.(cache.Reader)
				require.True(t, ok)
				_, hit, err := store.Get(key)
				require.NoError(t, err)
				require.True(t, hit)
			}
			require.NoDirExists(t, luaCachePath(state))
		})
	}
}
