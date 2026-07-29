// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	depconfig "github.com/wippyai/runtime/boot/deps/config"
	"github.com/wippyai/runtime/boot/deps/hub"
)

func TestValidateBuildDependencyRuntimeRequiresCompatibleDeclaration(t *testing.T) {
	dependencies := []dependencyRequest{{Org: "acme", Module: "frontend", BuildOnly: true}}
	require.EqualError(t,
		validateBuildDependencyRuntime(&depconfig.ModuleConfig{}, dependencies, "1.2.0"),
		"ns.build_dependency requires requires_wippy in wippy.yaml",
	)
	require.NoError(t, validateBuildDependencyRuntime(&depconfig.ModuleConfig{RequiresWippy: ">=1.2.0"}, dependencies, "1.2.0"))
	require.EqualError(t,
		validateBuildDependencyRuntime(&depconfig.ModuleConfig{RequiresWippy: ">=1.3.0"}, dependencies, "1.2.0"),
		"wippy 1.2.0 does not satisfy requires_wippy >=1.3.0",
	)
}

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
