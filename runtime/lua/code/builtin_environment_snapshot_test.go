// SPDX-License-Identifier: MPL-2.0

package code

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/go-lua/compiler/check"
	"github.com/wippyai/go-lua/types/io"
	"github.com/wippyai/go-lua/types/typ"
	api "github.com/wippyai/runtime/api/runtime/lua"
)

func TestBuiltinEnvironmentConcurrentManifestFinalization(t *testing.T) {
	manifest := io.NewManifest("native")
	item := typ.NewRecord().Field("id", typ.String).Build()
	manifest.DefineType("Item", item)
	manifest.AddGlobal("native_item", item)
	manifest.SetExport(typ.NewRecord().Field("item", item).Build())
	mods := []*api.ModuleDef{{Name: "native", Types: func() *io.Manifest { return manifest }}}
	const workers = 32
	environments := make([]*BuiltinEnvironment, workers)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range environments {
		wg.Go(func() {
			<-start
			env := NewBuiltinEnvironment(mods, check.Options{})
			// Registration may run concurrently with other environment builds.
			env.NewDatabase()
			environments[i] = env
		})
	}
	close(start)
	wg.Wait()
	for _, env := range environments {
		require.Same(t, manifest.Export, env.GlobalTypes["native"])
		require.Same(t, manifest.Globals["native_item"], env.GlobalTypes["native_item"])
		scoped, ok := env.TypeScope.LookupType("Item")
		require.True(t, ok)
		require.Same(t, manifest.Types["Item"], scoped)
	}
	// A subsequent build must reuse, rather than re-finalize, the graph.
	export := manifest.Export
	NewBuiltinEnvironment(mods, check.Options{}).NewDatabase()
	require.Same(t, export, manifest.Export)
}

func TestBuiltinEnvironmentSnapshotsFinalizedManifest(t *testing.T) {
	for _, bodyBacked := range []bool{false, true} {
		name := "declaration"
		if bodyBacked {
			name = "lua_export"
		}
		t.Run(name, func(t *testing.T) {
			manifest := io.NewManifest("native")
			manifest.BodyBacked = bodyBacked
			item := typ.NewRecord().Field("id", typ.String).Build()
			manifest.DefineType("Item", item)
			manifest.AddGlobal("native_item", item)
			manifest.SetExport(typ.NewRecord().Field("item", item).Build())
			env := NewBuiltinEnvironment([]*api.ModuleDef{{
				Name: "native", Types: func() *io.Manifest { return manifest },
			}}, check.Options{})

			// Database registration must not invalidate either namespace that
			// the environment has already published to its checkers.
			database := env.NewDatabase()
			require.Same(t, manifest, database.Manifest("native"))
			require.Same(t, manifest.Export, env.GlobalTypes["native"])
			require.Same(t, manifest.Globals["native_item"], env.GlobalTypes["native_item"])
			scopedItem, ok := env.TypeScope.LookupType("Item")
			require.True(t, ok)
			require.Same(t, manifest.Types["Item"], scopedItem)
			require.Equal(t, !bodyBacked, manifest.Types["Item"].(*typ.Record).Declared)
		})
	}
}
