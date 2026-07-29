// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/boot/deps/hub"
)

func TestConvertResolvedToLockPreservesBuildOnlyRole(t *testing.T) {
	lockObj, err := convertResolvedToLock(filepath.Join(t.TempDir(), "wippy.lock"), []hub.ResolvedModule{
		{Org: "acme", Name: "frontend", Version: "2.0.0", BuildOnly: true},
	}, ".wippy", ".")
	require.NoError(t, err)

	module, ok := lockObj.GetModule("acme/frontend")
	require.True(t, ok)
	require.True(t, module.BuildOnly)
}

func TestExtractRootDependenciesPreservesBuildRole(t *testing.T) {
	ctx := setupLoaderContext(t)
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

	dependencies, err := extractRootDependencies(entries, payload.GetTranscoder(ctx))
	require.NoError(t, err)
	require.Equal(t, []dependencyRequest{
		{Org: "acme", Module: "runtime", Constraint: "1.0.0"},
		{Org: "acme", Module: "frontend", Constraint: "2.0.0", BuildOnly: true},
	}, dependencies)
}
