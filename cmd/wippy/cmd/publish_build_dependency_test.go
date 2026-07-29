// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/boot/deps/lock"
)

func TestStripBuildDependenciesRecordsLockedProvenance(t *testing.T) {
	ctx := setupLoaderContext(t)
	lockObj, err := lock.New(filepath.Join(t.TempDir(), lock.DefaultFilename))
	require.NoError(t, err)
	lockObj.SetModule(lock.Module{
		Name:      "acme/frontend",
		Version:   "2.0.0",
		Hash:      "sha256:frontend",
		BuildOnly: true,
	})
	entries := []regapi.Entry{
		{
			ID:   regapi.NewID("app.deps", "runtime"),
			Kind: regapi.NamespaceDependency,
			Data: payload.New(map[string]any{"component": "acme/runtime", "version": "1.0.0"}),
		},
		{
			ID:   regapi.NewID("app.deps", "frontend"),
			Kind: regapi.NamespaceBuildDependency,
			Data: payload.New(map[string]any{"component": "acme/frontend", "version": "2.0.0"}),
		},
	}

	filtered, provenance, err := stripBuildDependencies(ctx, payload.GetTranscoder(ctx), entries, lockObj)
	require.NoError(t, err)
	require.Len(t, filtered, 1)
	require.Equal(t, regapi.NamespaceDependency, filtered[0].Kind)
	require.Equal(t, frontendProvenance{
		ManifestVersion: 1,
		Imports: []frontendImportProvenance{
			{
				Entry:   "app.deps:frontend",
				Module:  "acme/frontend",
				Version: "2.0.0",
				Digest:  "sha256:frontend",
			},
		},
	}, provenance)
}
