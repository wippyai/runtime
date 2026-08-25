// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/internal/version"
	registryimpl "github.com/wippyai/runtime/system/registry"
	regexp "github.com/wippyai/runtime/system/registry/expansion"
	"github.com/wippyai/runtime/system/registry/history/memory"
	historysqlite "github.com/wippyai/runtime/system/registry/history/sqlite"
	"github.com/wippyai/runtime/system/registry/topology"
	"go.uber.org/zap"
)

func TestDependencyHandler_RestartReloadsRebuiltReplacementWithoutHub(t *testing.T) {
	ctx := newTestContext()
	rootDir := t.TempDir()
	lockPath := filepath.Join(rootDir, "wippy.lock")
	replacementPath := filepath.Join(rootDir, "local-mod")
	staticBundle := filepath.Join(replacementPath, "static", "app.js")
	registryPath := filepath.Join(rootDir, "registry.db")

	require.NoError(t, os.MkdirAll(filepath.Dir(staticBundle), 0o755))
	require.NoError(t, os.WriteFile(lockPath, []byte(`directories:
  modules: .wippy
  src: ./src
replacements:
  - from: local/mod
    to: ./local-mod
`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(replacementPath, "_index.json"), []byte(`{
  "namespace": "local.mod",
  "entries": [{"name": "svc", "kind": "registry.entry", "data": {"generation": "one"}}]
}`), 0o600))
	require.NoError(t, os.WriteFile(staticBundle, []byte("window.build = 'one';\n"), 0o600))

	hubCalls := 0
	unreachableHub := &fakeHub{
		getManifest: func(context.Context, string, string, string) (*ModuleManifest, error) {
			hubCalls++
			return nil, errors.New("hub must not be contacted for a local replacement")
		},
		getDownload: func(context.Context, *DownloadParams) (*DownloadInfo, error) {
			hubCalls++
			return nil, errors.New("hub must not be contacted for a local replacement")
		},
	}
	newHandler := func() *DependencyHandler {
		handler, err := NewDependencyHandler(DependencyHandlerOptions{
			Hub:       unreachableHub,
			Logger:    zap.NewNop(),
			LockPath:  lockPath,
			VendorDir: filepath.Join(rootDir, "vendor"),
		})
		require.NoError(t, err)
		return handler
	}
	newRegistry := func(history regapi.History, handler *DependencyHandler) *registryimpl.Reg {
		resolver := topology.NewResolver()
		handler.resolver = resolver
		return registryimpl.NewRegistry(
			history,
			&bootRecordingRunner{},
			topology.NewStateBuilder(zap.NewNop(), resolver),
			resolver,
			zap.NewNop(),
			registryimpl.WithKindDirective(
				regapi.NamespaceDependency,
				regexp.NewDependencyDirective(handler.Expand).WithResolutionTransition(handler.ReconcileResolution),
			),
		)
	}

	history, err := historysqlite.NewSQLite(registryPath, zap.NewNop())
	require.NoError(t, err)
	initialHistory := history
	t.Cleanup(func() { _ = initialHistory.Close() })
	registry := newRegistry(history, newHandler())
	root := regapi.Entry{
		ID:   regapi.NewID("app.deps", "local"),
		Kind: regapi.NamespaceDependency,
		Data: payload.New(map[string]any{"component": "local/mod", "version": "v0.1.0"}),
	}
	fresh := newRegistry(memory.New(), newHandler())
	startupCtx := regapi.WithDependencyAccess(newTestContext(), regapi.DependencyAccessUnspecified)
	require.NoError(t, fresh.LoadState(startupCtx, fixtureState(regapi.State{root}), version.New(0)))
	_, err = fresh.GetEntry(regapi.NewID("local.mod", "svc"))
	require.NoError(t, err)
	require.Zero(t, hubCalls)

	version, err := registry.Apply(ctx, regapi.ChangeSet{{Kind: regapi.EntryCreate, Entry: root}})
	require.NoError(t, err)
	require.Zero(t, hubCalls)

	entryID := regapi.NewID("local.mod", "svc")
	_, err = registry.GetEntry(entryID)
	require.NoError(t, err)
	initial, ok := registry.EntryProvenance(entryID)
	require.True(t, ok)
	require.NotEmpty(t, initial.Digest, "a replacement records its source tree identity")

	// A frontend rebuild mutates only static content. Simulate a cold restart
	// with the persisted registry intact and an unreachable Hub.
	require.NoError(t, os.WriteFile(staticBundle, []byte("window.build = 'two';\n"), 0o600))
	require.NoError(t, history.Close())

	history, err = historysqlite.NewSQLite(registryPath, zap.NewNop())
	require.NoError(t, err)
	t.Cleanup(func() { _ = history.Close() })
	restarted := newRegistry(history, newHandler())
	restartCtx := regapi.WithDependencyAccess(newTestContext(), regapi.DependencyAccessVerifiedOffline)
	require.NoError(t, restarted.LoadState(restartCtx, regapi.ProvenancedState{}, version))
	require.Zero(t, hubCalls)

	reloadedEntry, err := restarted.GetEntry(entryID)
	require.NoError(t, err)
	reloaded, ok := restarted.EntryProvenance(entryID)
	require.True(t, ok)
	require.NotEqual(t, initial.Digest, reloaded.Digest,
		"a rebuilt tree advances the resident record while the stored checkpoint stands")
	require.Equal(t, "one", reloadedEntry.Data.Data().(map[string]any)["generation"])
}
