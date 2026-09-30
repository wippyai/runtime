// SPDX-License-Identifier: MPL-2.0

package code

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/registry"
	api "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/code/cache"
)

func fingerprintValues(t *testing.T, cm *Manager, graph *MemoryGraph, id registry.ID) [3]string {
	t.Helper()
	runtime, err := runtimeFingerprintMemo(graph, id, make(map[registry.ID]string), cm.toolchainIdentity)
	require.NoError(t, err)
	compile, _, err := cm.compileFingerprintFromGraph(graph, id)
	require.NoError(t, err)
	typecheck, _, err := cm.typecheckFingerprintFromGraph(graph, id)
	require.NoError(t, err)
	return [3]string{runtime, compile, typecheck}
}

func versionedFingerprintGraph(t *testing.T) (*Manager, registry.ID, registry.ID) {
	t.Helper()
	cm, _, app := setupFingerprintGraph(t)
	lib := registry.NewID("lib", "util")
	for i, id := range []registry.ID{lib, app} {
		node, err := cm.memGraph.GetNode(id)
		require.NoError(t, err)
		copy := *node
		copy.Version = Version{Revision: uint64(i + 1), Hash: HashNode(node)}
		var deps []Import
		if id == app {
			deps = []Import{{ID: lib, Alias: "util"}}
		}
		require.NoError(t, cm.memGraph.UpdateNode(&copy, deps))
	}
	return cm, lib, app
}

func TestFingerprintMemoInvalidationAndSnapshotIsolation(t *testing.T) {
	cm, lib, app := versionedFingerprintGraph(t)
	initial := fingerprintValues(t, cm, cm.memGraph, app)
	frozen := cm.memGraph.snapshotReachable(app, nil)
	require.Same(t, cm.memGraph.fingerprints, frozen.fingerprints)
	for _, stage := range cm.memGraph.fingerprints.stages {
		require.Len(t, stage, 2)
	}
	require.Equal(t, initial, fingerprintValues(t, cm, frozen, app))
	// Changing a dependency invalidates an unchanged entrypoint revision.
	libNode, err := cm.memGraph.GetNode(lib)
	require.NoError(t, err)
	replacement := *libNode
	replacement.Source = "return 2"
	replacement.Version = Version{Revision: 3, Hash: HashNode(&replacement)}
	require.NoError(t, cm.memGraph.UpdateNode(&replacement, nil))
	changed := fingerprintValues(t, cm, cm.memGraph, app)
	for i := range initial {
		require.NotEqual(t, initial[i], changed[i])
	}
	require.Equal(t, initial, fingerprintValues(t, cm, frozen, app))
	// Edge mutations must invalidate fingerprints independently of node revision.
	require.NoError(t, cm.memGraph.RemoveDependency(app, lib))
	removed := fingerprintValues(t, cm, cm.memGraph, app)
	for i := range changed {
		require.NotEqual(t, changed[i], removed[i])
	}
	require.NoError(t, cm.memGraph.AddDependency(app, lib, "renamed"))
	renamed := fingerprintValues(t, cm, cm.memGraph, app)
	for i := range changed {
		require.NotEqual(t, changed[i], renamed[i])
	}
	// Same-content delete/recreate keeps persistent keys but changes runtime tag.
	require.NoError(t, cm.memGraph.RemoveDependency(app, lib))
	require.NoError(t, cm.memGraph.RemoveNode(lib))
	replacement.Version.Revision++
	require.NoError(t, cm.memGraph.AddNode(&replacement))
	require.NoError(t, cm.memGraph.AddDependency(app, lib, "renamed"))
	recreated := fingerprintValues(t, cm, cm.memGraph, app)
	require.NotEqual(t, renamed[0], recreated[0])
	require.Equal(t, renamed[1:], recreated[1:])
	// Invalid mutations don't poison memoized results.
	require.Error(t, cm.memGraph.AddDependency(lib, app, "cycle"))
	require.Equal(t, recreated, fingerprintValues(t, cm, cm.memGraph, app))
}

func TestFingerprintMemoConfigurationAndBounds(t *testing.T) {
	cm, _, app := versionedFingerprintGraph(t)
	initial := fingerprintValues(t, cm, cm.memGraph, app)
	cm.builtinHash = "new-builtins"
	builtins := fingerprintValues(t, cm, cm.memGraph, app)
	require.Equal(t, initial[:2], builtins[:2])
	require.NotEqual(t, initial[2], builtins[2])
	cm.typeCfgHash = "new-config"
	config := fingerprintValues(t, cm, cm.memGraph, app)
	require.NotEqual(t, builtins[2], config[2])
	cm.toolchainIdentity = "new-toolchain"
	toolchain := fingerprintValues(t, cm, cm.memGraph, app)
	for i := range toolchain {
		require.NotEqual(t, config[i], toolchain[i])
	}
	for _, stage := range cm.memGraph.fingerprints.stages {
		require.Len(t, stage, 2)
	}
}

func TestFingerprintMemoConcurrentSnapshots(t *testing.T) {
	cm, _, app := versionedFingerprintGraph(t)
	want := fingerprintValues(t, cm, cm.memGraph, app)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		graph := cm.memGraph.snapshotReachable(app, nil)
		wg.Go(func() {
			for n := 0; n < 100; n++ {
				// All snapshots share the memo but own graph/node maps.
				got := fingerprintValues(t, cm, graph, app)
				if got != want {
					t.Errorf("fingerprints changed: %v", got)
				}
			}
		})
	}
	wg.Wait()
}

func TestFingerprintMemoRevisionZeroIsNotCached(t *testing.T) {
	cm, node, app := setupFingerprintGraph(t)
	_ = fingerprintValues(t, cm, cm.memGraph, app)
	_, err := cm.memGraph.GetNode(node.ID)
	require.NoError(t, err)
	node.Source = "return 3"
	require.Equal(t, api.Library, node.Kind)
	for _, stage := range cm.memGraph.fingerprints.stages {
		require.Empty(t, stage)
	}
}

func TestFingerprintMetadataIsolation(t *testing.T) {
	cm, _, app := versionedFingerprintGraph(t)
	for _, fingerprint := range []func(registry.ID) (string, []cache.DepMeta, error){cm.compileFingerprint, cm.typecheckFingerprint} {
		fp, deps, err := fingerprint(app)
		require.NoError(t, err)
		require.Len(t, deps, 1)
		deps[0].Alias = "caller-mutated"
		nextFP, nextDeps, err := fingerprint(app)
		require.NoError(t, err)
		require.Equal(t, fp, nextFP)
		require.Equal(t, "util", nextDeps[0].Alias)
	}
}

func TestFingerprintMemoUnversionedDependency(t *testing.T) {
	cm, lib, app := setupFingerprintGraph(t)
	node, err := cm.memGraph.GetNode(app)
	require.NoError(t, err)
	replacement := *node
	replacement.Version = Version{Revision: 1, Hash: HashNode(node)}
	require.NoError(t, cm.memGraph.UpdateNode(&replacement, []Import{{ID: lib.ID, Alias: "util"}}))
	initial := fingerprintValues(t, cm, cm.memGraph, app)
	lib.Source = "return 2"
	updated := fingerprintValues(t, cm, cm.memGraph, app)
	for i := range initial {
		require.NotEqual(t, initial[i], updated[i])
	}
}
