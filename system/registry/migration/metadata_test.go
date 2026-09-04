// SPDX-License-Identifier: MPL-2.0

package migration_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/internal/version"
	"github.com/wippyai/runtime/system/registry/history/memory"
	"github.com/wippyai/runtime/system/registry/history/sqlite"
	registrymigration "github.com/wippyai/runtime/system/registry/migration"
	"go.uber.org/zap"
)

func TestApplyIgnoresUnpersistedHistory(t *testing.T) {
	history := memory.New()
	require.NoError(t, registrymigration.Apply(t.Context(), history, nil))
}

func TestApplyMarksStoredResolutionRootsOutsideBaseline(t *testing.T) {
	history, err := sqlite.NewSQLite(filepath.Join(t.TempDir(), "registry.db"), zap.NewNop())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, history.Close()) })

	declaredID := regapi.NewID("app.deps", "declared")
	dynamicID := regapi.NewID("app.deps", "dynamic")
	v1 := version.FromParent(version.New(0), 1)
	resolution := (&regapi.DependencyResolution{
		InputDigest: "sha256:input",
		Roots: []regapi.DependencyRoot{
			{ID: declaredID.String(), Component: "example/declared", Version: ">=1.0.0"},
			{ID: dynamicID.String(), Component: "example/dynamic", Version: ">=1.0.0"},
		},
		Modules: []regapi.ResolvedModule{
			{Name: "example/declared", Version: "1.0.0", Digest: "sha256:declared"},
			{Name: "example/dynamic", Version: "1.0.0", Digest: "sha256:dynamic"},
		},
	}).Canonical()
	require.NoError(t, history.SaveWithDependencyResolution(v1, regapi.ChangeSet{
		{Kind: regapi.EntryCreate, Entry: regapi.Entry{ID: declaredID, Kind: regapi.NamespaceDependency}},
		{Kind: regapi.EntryCreate, Entry: regapi.Entry{ID: dynamicID, Kind: regapi.NamespaceDependency}},
	}, resolution, true))

	baseline := regapi.State{{
		ID:       declaredID,
		Registry: regapi.EntryMetadata{Owner: "example/application"},
	}}
	require.NoError(t, registrymigration.Apply(context.Background(), history, baseline))
	changes, err := history.Get(v1)
	require.NoError(t, err)
	require.Equal(t, regapi.EntryMetadata{Owner: "example/application", Root: true}, changes[0].Entry.Registry)
	require.Equal(t, regapi.EntryMetadata{Root: true}, changes[1].Entry.Registry)
}
