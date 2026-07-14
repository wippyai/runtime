// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/boot/deps/graph"
	"github.com/wippyai/wapp"
	"go.uber.org/zap"
)

func hardeningRoot(id, component, version string) regapi.Entry {
	return regapi.Entry{
		ID:   regapi.ParseID(id),
		Kind: regapi.NamespaceDependency,
		Data: payload.New(map[string]any{"component": component, "version": version}),
	}
}

func hardeningModuleEntry(id, module, version string) regapi.Entry {
	return regapi.Entry{
		ID:   regapi.ParseID(id),
		Kind: regapi.EntryKind,
		Meta: attrs.NewBagFrom(map[string]any{metaModuleKey: module, metaModuleVersionKey: version}),
		Data: payload.New(map[string]any{"module": module}),
	}
}

func hardeningResolution(roots ...regapi.Entry) *regapi.DependencyResolution {
	desired := make([]desiredDependency, 0, len(roots))
	modules := make([]ResolvedModule, 0, len(roots))
	seen := make(map[string]struct{})
	for _, root := range roots {
		def, _ := root.Data.Data().(map[string]any)
		component, _ := def["component"].(string)
		version, _ := def["version"].(string)
		desired = append(desired, desiredDependency{entry: root, definition: DependencyDefinition{Component: component, Version: version}})
		if _, ok := seen[component]; ok {
			continue
		}
		seen[component] = struct{}{}
		name, _ := graph.ParseName(component)
		modules = append(modules, ResolvedModule{Org: name.Organization, Name: name.Module, Version: "v1.0.0"})
	}
	return dependencyResolution(desired, modules)
}

func TestDependencyHandler_ExpandChangesDeletesEarlierRootModulesAndReturnsResolution(t *testing.T) {
	ctx := newTestContext()
	vendorDir := t.TempDir()
	artifact := buildWappBytes(t, []wapp.Entry{{
		ID: wapp.NewID("acme.b", "entry"), Kind: regapi.EntryKind, Data: map[string]any{"module": "b"},
	}})
	manifestCalls := 0
	hub := &fakeHub{
		getManifest: func(_ context.Context, org, module, _ string) (*ModuleManifest, error) {
			manifestCalls++
			return &ModuleManifest{Org: org, Name: module, Version: "v1.0.0", URL: "memory://b"}, nil
		},
		downloadFile: func(_ context.Context, _ string, dest string) error {
			require.NoError(t, os.MkdirAll(filepath.Dir(dest), 0o755))
			return os.WriteFile(dest, artifact, 0o600)
		},
	}
	handler, err := NewDependencyHandler(DependencyHandlerOptions{Hub: hub, Logger: zap.NewNop(), VendorDir: vendorDir})
	require.NoError(t, err)

	rootA := hardeningRoot("app.deps:a", "acme/a", "v1.0.0")
	rootB := hardeningRoot("app.deps:b", "acme/b", "v1.0.0")
	moduleA := hardeningModuleEntry("acme.a:entry", "acme/a", "v1.0.0")
	moduleB := hardeningModuleEntry("acme.b:entry", "acme/b", "v1.0.0")
	result, err := handler.ExpandChanges(ctx, regapi.ChangeSet{
		{Kind: regapi.EntryDelete, Entry: regapi.Entry{ID: rootA.ID}},
		{Kind: regapi.EntryUpdate, Entry: rootB}, // deliberately unchanged and last
	}, regapi.State{rootA, rootB, moduleA, moduleB})
	require.NoError(t, err)
	require.NotNil(t, result.Resolution, "the final no-op must not inherit the pre-batch graph")
	require.Len(t, result.Resolution.Roots, 1)
	require.Equal(t, rootB.ID.String(), result.Resolution.Roots[0].ID)
	require.Equal(t, 1, manifestCalls)
	require.Contains(t, result.Additional, regapi.ScopedOperation{
		Operation: regapi.Operation{Kind: regapi.EntryDelete, Entry: regapi.Entry{ID: moduleA.ID}},
		Scope:     regapi.ScopeBaseline,
	})
}

func TestDependencyHandler_ExpandChangesRetargetsEarlierRootWithoutLeavingOldModule(t *testing.T) {
	ctx := newTestContext()
	vendorDir := t.TempDir()
	artifacts := map[string][]byte{
		"b": buildWappBytes(t, []wapp.Entry{{ID: wapp.NewID("acme.b", "entry"), Kind: regapi.EntryKind}}),
		"c": buildWappBytes(t, []wapp.Entry{{ID: wapp.NewID("acme.c", "entry"), Kind: regapi.EntryKind}}),
	}
	hub := &fakeHub{
		getManifest: func(_ context.Context, org, module, _ string) (*ModuleManifest, error) {
			return &ModuleManifest{Org: org, Name: module, Version: "v1.0.0", URL: "memory://" + module}, nil
		},
		downloadFile: func(_ context.Context, url, dest string) error {
			require.NoError(t, os.MkdirAll(filepath.Dir(dest), 0o755))
			return os.WriteFile(dest, artifacts[url[len("memory://"):]], 0o600)
		},
	}
	handler, err := NewDependencyHandler(DependencyHandlerOptions{Hub: hub, Logger: zap.NewNop(), VendorDir: vendorDir})
	require.NoError(t, err)
	rootA := hardeningRoot("app.deps:a", "acme/a", "v1.0.0")
	rootC := hardeningRoot("app.deps:a", "acme/c", "v1.0.0")
	rootB := hardeningRoot("app.deps:b", "acme/b", "v1.0.0")
	moduleA := hardeningModuleEntry("acme.a:entry", "acme/a", "v1.0.0")
	moduleB := hardeningModuleEntry("acme.b:entry", "acme/b", "v1.0.0")
	result, err := handler.ExpandChanges(ctx, regapi.ChangeSet{
		{Kind: regapi.EntryUpdate, Entry: rootC},
		{Kind: regapi.EntryUpdate, Entry: rootB},
	}, regapi.State{rootA, rootB, moduleA, moduleB})
	require.NoError(t, err)
	require.Equal(t, "acme/c", result.Resolution.Roots[0].Component)
	require.Contains(t, result.Additional, regapi.ScopedOperation{
		Operation: regapi.Operation{Kind: regapi.EntryDelete, Entry: regapi.Entry{ID: moduleA.ID}},
		Scope:     regapi.ScopeBaseline,
	})
}

func TestDependencyHandler_ExpandUnchangedRootStillReturnsLegacyCheckpointGraph(t *testing.T) {
	ctx := newTestContext()
	root := hardeningRoot("app.deps:a", "acme/a", "v1.0.0")
	module := hardeningModuleEntry("acme.a:entry", "acme/a", "v1.0.0")
	artifact := buildWappBytes(t, []wapp.Entry{{ID: wapp.NewID("acme.a", "entry"), Kind: regapi.EntryKind}})
	hub := &fakeHub{
		getManifest: func(_ context.Context, org, name, _ string) (*ModuleManifest, error) {
			return &ModuleManifest{Org: org, Name: name, Version: "v1.0.0", URL: "memory://a"}, nil
		},
		downloadFile: func(_ context.Context, _ string, dest string) error {
			require.NoError(t, os.MkdirAll(filepath.Dir(dest), 0o755))
			return os.WriteFile(dest, artifact, 0o600)
		},
	}
	handler, err := NewDependencyHandler(DependencyHandlerOptions{Hub: hub, Logger: zap.NewNop(), VendorDir: t.TempDir()})
	require.NoError(t, err)
	result, err := handler.Expand(ctx, regapi.Operation{Kind: regapi.EntryUpdate, Entry: root}, regapi.State{root, module})
	require.NoError(t, err)
	require.NotNil(t, result.Resolution)
	require.Len(t, result.Resolution.Modules, 1)
}

func TestDependencyHandler_ReconcileRejectsRootSetDriftAndDuplicates(t *testing.T) {
	ctx := newTestContext()
	handler, err := NewDependencyHandler(DependencyHandlerOptions{Hub: &fakeHub{}, Logger: zap.NewNop(), VendorDir: t.TempDir()})
	require.NoError(t, err)
	rootA := hardeningRoot("app.deps:a", "acme/a", "v1.0.0")
	rootB := hardeningRoot("app.deps:b", "acme/b", "v1.0.0")

	_, err = handler.ReconcileResolution(ctx, regapi.State{rootA, rootB}, hardeningResolution(rootA))
	require.ErrorContains(t, err, "root set")

	duplicate := hardeningResolution(rootA, rootB)
	duplicate.Roots[1] = duplicate.Roots[0]
	duplicate = duplicate.Canonical()
	_, err = handler.ReconcileResolution(ctx, regapi.State{rootA, rootB}, duplicate)
	require.ErrorContains(t, err, "duplicate stored dependency root")

	rootBDuplicateComponent := hardeningRoot("app.deps:b", "acme/a", "v1.0.0")
	_, err = handler.ReconcileResolution(ctx, regapi.State{rootA, rootBDuplicateComponent}, hardeningResolution(rootA, rootBDuplicateComponent))
	require.ErrorContains(t, err, "duplicate stored dependency component")
}

func TestDependencyHandler_ReconcileAcceptsStoredLabelSelectionOffline(t *testing.T) {
	ctx := newTestContext()
	root := hardeningRoot("app.deps:a", "acme/a", "@latest")
	module := hardeningModuleEntry("acme.a:entry", "acme/a", "v1.0.0")
	handler, err := NewDependencyHandler(DependencyHandlerOptions{Hub: &fakeHub{}, Logger: zap.NewNop(), VendorDir: t.TempDir()})
	require.NoError(t, err)
	resolution := hardeningResolution(root)
	result, err := handler.ReconcileResolution(ctx, regapi.State{root, module}, resolution)
	require.NoError(t, err)
	require.Equal(t, resolution.Digest, result.Resolution.Digest)
}

func TestDependencyHandler_ReconcileRejectsUnsafeStoredArtifactIdentity(t *testing.T) {
	ctx := newTestContext()
	root := hardeningRoot("app.deps:a", "../escape", "v1.0.0")
	resolution := hardeningResolution(root)
	handler, err := NewDependencyHandler(DependencyHandlerOptions{Hub: &fakeHub{}, Logger: zap.NewNop(), VendorDir: t.TempDir()})
	require.NoError(t, err)
	_, err = handler.ReconcileResolution(ctx, regapi.State{root}, resolution)
	require.ErrorContains(t, err, "invalid module name")

	safeRoot := hardeningRoot("app.deps:safe", "acme/safe", "v1.0.0")
	badDigest := hardeningResolution(safeRoot)
	badDigest.Modules[0].Digest = "sha256:deadbeef"
	badDigest = badDigest.Canonical()
	_, err = handler.ReconcileResolution(ctx, regapi.State{safeRoot}, badDigest)
	require.ErrorContains(t, err, "invalid sha256 digest")

	require.Error(t, validateModuleArtifactIdentity(graph.Name{Organization: "acme", Module: "safe"}, "1.0.0/../../escape", ""))
	_, err = containedPath(t.TempDir(), filepath.Join("..", "escape.wapp"))
	require.ErrorContains(t, err, "escapes vendor")
}

func TestValidateDownloadInfoRejectsStoredIdentityDrift(t *testing.T) {
	mod := ResolvedModule{Org: "acme", Name: "safe", Version: "v1.0.0", Digest: "sha256:" + string(make([]byte, 64)), SizeBytes: 10}
	require.ErrorContains(t, validateDownloadInfo(mod, &DownloadInfo{Version: "v2.0.0"}), "version mismatch")
	require.ErrorContains(t, validateDownloadInfo(mod, &DownloadInfo{Version: "v1.0.0", Digest: "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"}), "digest mismatch")
	require.ErrorContains(t, validateDownloadInfo(mod, &DownloadInfo{Version: "v1.0.0", Size: 11}), "size mismatch")
}
