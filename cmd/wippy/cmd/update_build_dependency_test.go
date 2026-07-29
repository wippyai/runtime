// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	depconfig "github.com/wippyai/runtime/boot/deps/config"
	"github.com/wippyai/runtime/boot/deps/hub"
	"github.com/wippyai/runtime/boot/deps/lock"
)

func TestValidateBuildDependencyRuntimeRequiresCompatibleDeclaration(t *testing.T) {
	entries := []regapi.Entry{{Kind: regapi.NamespaceBuildDependency}}
	require.EqualError(t,
		validateBuildDependencyRuntime(&depconfig.ModuleConfig{}, entries, "1.2.0"),
		"ns.build_dependency requires requires_wippy in wippy.yaml",
	)
	require.NoError(t, validateBuildDependencyRuntime(&depconfig.ModuleConfig{RequiresWippy: ">=1.2.0"}, entries, "1.2.0"))
	require.EqualError(t,
		validateBuildDependencyRuntime(&depconfig.ModuleConfig{RequiresWippy: ">=1.3.0"}, entries, "1.2.0"),
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

	dependencies, err := extractRootDependencies(entries, payload.GetTranscoder(ctx), nil)
	require.NoError(t, err)
	require.Equal(t, []dependencyRequest{
		{Org: "acme", Module: "runtime", Constraint: "1.0.0"},
		{Org: "acme", Module: "frontend", Constraint: "2.0.0", BuildOnly: true},
	}, dependencies)
}

func TestExtractRootDependenciesPropagatesBuildRoleThroughReplacement(t *testing.T) {
	ctx := setupLoaderContext(t)
	entries := []regapi.Entry{
		{
			ID:   regapi.NewID("app.deps", "frontend"),
			Kind: regapi.NamespaceBuildDependency,
			Data: payload.New(map[string]any{"component": "local/frontend", "version": "1.0.0"}),
		},
		{
			ID:   regapi.NewID("local.frontend", "runtime"),
			Kind: regapi.NamespaceDependency,
			Data: payload.New(map[string]any{"component": "acme/runtime", "version": "2.0.0"}),
			Meta: attrs.NewBagFrom(map[string]any{"module": "local/frontend"}),
		},
	}

	graph, err := resolveDependencyGraph(entries, payload.GetTranscoder(ctx), map[string]bool{"local/frontend": true})
	require.NoError(t, err)
	require.Equal(t, []dependencyRequest{{
		Org: "acme", Module: "runtime", Constraint: "2.0.0", BuildOnly: true,
	}}, graph.dependencies)
	require.Equal(t, []dependencyRequest{{
		Org: "local", Module: "frontend", Constraint: "1.0.0", BuildOnly: true,
	}}, graph.replacements)
}

func TestExtractRootDependenciesRuntimeWinsThroughReplacement(t *testing.T) {
	ctx := setupLoaderContext(t)
	entries := []regapi.Entry{
		{
			ID:   regapi.NewID("app.deps", "frontend-runtime"),
			Kind: regapi.NamespaceDependency,
			Data: payload.New(map[string]any{"component": "local/frontend", "version": "1.0.0"}),
		},
		{
			ID:   regapi.NewID("app.deps", "frontend-build"),
			Kind: regapi.NamespaceBuildDependency,
			Data: payload.New(map[string]any{"component": "local/frontend", "version": "2.0.0"}),
		},
		{
			ID:   regapi.NewID("local.frontend", "runtime"),
			Kind: regapi.NamespaceDependency,
			Data: payload.New(map[string]any{"component": "acme/runtime", "version": "3.0.0"}),
			Meta: attrs.NewBagFrom(map[string]any{"module": "local/frontend"}),
		},
	}

	graph, err := resolveDependencyGraph(entries, payload.GetTranscoder(ctx), map[string]bool{"local/frontend": true})
	require.NoError(t, err)
	require.Equal(t, []dependencyRequest{{
		Org: "acme", Module: "runtime", Constraint: "3.0.0",
	}}, graph.dependencies)
	require.Equal(t, []dependencyRequest{{
		Org: "local", Module: "frontend", Constraint: "1.0.0",
	}}, graph.replacements)
}

func TestPreserveBuildOnlyReplacementModulesRetainsRoleAndRejectsRootTransition(t *testing.T) {
	oldLock, err := lock.New(filepath.Join(t.TempDir(), "old.lock"))
	require.NoError(t, err)
	oldLock.SetModule(lock.Module{Name: "local/frontend", Version: "1.0.0", Root: true})
	newLock, err := lock.New(filepath.Join(t.TempDir(), "new.lock"))
	require.NoError(t, err)

	preserveBuildOnlyReplacementModules(newLock, oldLock, []dependencyRequest{{
		Org: "local", Module: "frontend", Constraint: "2.0.0", BuildOnly: true,
	}})

	module, ok := newLock.GetModule("local/frontend")
	require.True(t, ok)
	require.Equal(t, "1.0.0", module.Version)
	require.True(t, module.BuildOnly)
	require.True(t, module.Root)
	require.EqualError(t, lock.Validate(newLock), "deployment root local/frontend cannot be build-only")
}

func TestTargetedResolveOptionsPinsOnlyNonTargets(t *testing.T) {
	lockObj, err := lock.New(filepath.Join(t.TempDir(), lock.DefaultFilename))
	require.NoError(t, err)
	lockObj.SetModule(lock.Module{Name: "acme/target", Version: "2.0.0", Hash: "sha256:target", BuildOnly: true})
	lockObj.SetModule(lock.Module{Name: "acme/frozen", Version: "1.0.0", Hash: "sha256:frozen"})
	lockObj.SetModule(lock.Module{Name: "local/replaced", Version: "3.0.0", Hash: "sha256:replaced"})

	options := targetedResolveOptions(lockObj, map[string]bool{"acme/target": true}, map[string]bool{"local/replaced": true})
	require.Equal(t, map[string]string{"acme/frozen": "1.0.0"}, options.LockedVersions)
	require.Equal(t, map[string]string{"acme/frozen": "sha256:frozen"}, options.LockedDigests)
}

func TestExtractRootDependenciesRejectsMalformedBuildComponent(t *testing.T) {
	ctx := setupLoaderContext(t)
	entries := []regapi.Entry{{
		ID:   regapi.NewID("app.deps", "frontend"),
		Kind: regapi.NamespaceBuildDependency,
		Data: payload.New(map[string]any{"component": "frontend", "version": "1.0.0"}),
	}}

	_, err := extractRootDependencies(entries, payload.GetTranscoder(ctx), nil)
	require.EqualError(t, err, "build dependency app.deps:frontend has invalid component frontend")
}

func TestExtractRootDependenciesRejectsBuildVersionRanges(t *testing.T) {
	ctx := setupLoaderContext(t)
	entries := []regapi.Entry{{
		ID:   regapi.NewID("app.deps", "frontend"),
		Kind: regapi.NamespaceBuildDependency,
		Data: payload.New(map[string]any{"component": "acme/frontend", "version": ">=1.0.0"}),
	}}

	_, err := extractRootDependencies(entries, payload.GetTranscoder(ctx), nil)
	require.EqualError(t, err, "build dependency app.deps:frontend must use an exact semver version: >=1.0.0")
}

func TestContainsBuildDependencyChecksDeclarationsBeforeRuntimeWins(t *testing.T) {
	entries := []regapi.Entry{
		{Kind: regapi.NamespaceDependency},
		{Kind: regapi.NamespaceBuildDependency},
	}
	require.True(t, containsBuildDependency(entries))
}
