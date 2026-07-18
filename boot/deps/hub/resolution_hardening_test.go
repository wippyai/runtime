// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/boot/deps/graph"
	"github.com/wippyai/runtime/boot/deps/lock"
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

func TestReplacementResolutionRevalidatesCurrentTree(t *testing.T) {
	tmpDir := t.TempDir()
	lockPath := filepath.Join(tmpDir, lock.DefaultFilename)
	replacementPath := filepath.Join(tmpDir, "local-http")
	require.NoError(t, os.MkdirAll(replacementPath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(replacementPath, "entry.yaml"), []byte("first"), 0o600))
	require.NoError(t, os.WriteFile(lockPath, []byte("directories:\n  modules: .wippy\n  src: ./src\n"), 0o600))

	handler, err := NewDependencyHandler(DependencyHandlerOptions{
		Hub:      &fakeHub{},
		Logger:   zap.NewNop(),
		LockPath: lockPath,
		WorkspaceReplacements: []lock.Replacement{
			{From: "acme/http", To: replacementPath},
		},
	})
	require.NoError(t, err)

	digest, size, err := digestDirectoryTree(replacementPath)
	require.NoError(t, err)
	module := ResolvedModule{
		Org:       "acme",
		Name:      "http",
		Version:   "v1.0.0",
		Source:    moduleSourceReplacementTreeV1,
		Digest:    digest,
		SizeBytes: size,
	}
	require.True(t, handler.hasCurrentUnpackedModule(module))

	require.NoError(t, os.WriteFile(filepath.Join(replacementPath, "entry.yaml"), []byte("changed"), 0o600))
	require.False(t, handler.hasCurrentUnpackedModule(module), "history must not trust stale entry metadata after replacement content changes")
}

func hardeningModuleEntry(id, module, version string) regapi.Entry {
	return regapi.Entry{
		ID:   regapi.ParseID(id),
		Kind: regapi.EntryKind,
		Meta: attrs.NewBagFrom(map[string]any{
			metaModuleKey:        module,
			metaModuleVersionKey: version,
			metaModuleDigestKey:  hardeningDigest,
		}),
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
		modules = append(modules, ResolvedModule{
			Org: name.Organization, Name: name.Module, Version: "v1.0.0", Digest: hardeningDigest,
		})
	}
	return dependencyResolution(desired, nil, modules)
}

const hardeningDigest = "sha256:0000000000000000000000000000000000000000000000000000000000000000"

func TestArtifactDigestsEqualNormalizesLegacyLockHashes(t *testing.T) {
	require.True(t, artifactDigestsEqual(strings.TrimPrefix(hardeningDigest, "sha256:"), hardeningDigest))
	require.False(t, artifactDigestsEqual("", hardeningDigest))
	require.False(t, artifactDigestsEqual("sha256:deadbeef", hardeningDigest))
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

func TestDependencyHandler_ExpandChangesRetaggingRootStillRemovesOwnedModule(t *testing.T) {
	ctx := newTestContext()
	handler, err := NewDependencyHandler(DependencyHandlerOptions{
		Hub: &fakeHub{}, Logger: zap.NewNop(), VendorDir: t.TempDir(),
	})
	require.NoError(t, err)

	root := hardeningRoot("app.deps:a", "acme/a", "v1.0.0")
	owned := hardeningModuleEntry("acme.a:entry", "acme/a", "v1.0.0")
	retagged := root
	retagged.Meta = attrs.NewBagFrom(map[string]any{
		metaModuleKey: "host/owner",
	})

	result, err := handler.ExpandChanges(ctx, regapi.ChangeSet{{
		Kind: regapi.EntryUpdate, Entry: retagged,
	}}, regapi.State{root, owned})
	require.NoError(t, err)
	require.True(t, result.Applied)
	require.Contains(t, result.Additional, regapi.ScopedOperation{
		Operation: regapi.Operation{Kind: regapi.EntryDelete, Entry: regapi.Entry{ID: owned.ID}},
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

	_, err = handler.ReconcileResolution(ctx, regapi.State{rootA, rootB}, regapi.State{rootA, rootB}, hardeningResolution(rootA))
	require.ErrorContains(t, err, "root set")

	duplicate := hardeningResolution(rootA, rootB)
	duplicate.Roots[1] = duplicate.Roots[0]
	duplicate = duplicate.Canonical()
	_, err = handler.ReconcileResolution(ctx, regapi.State{rootA, rootB}, regapi.State{rootA, rootB}, duplicate)
	require.ErrorContains(t, err, "stored dependency resolution is invalid")

	rootBDuplicateComponent := hardeningRoot("app.deps:b", "acme/a", "v1.0.0")
	_, err = handler.ReconcileResolution(ctx, regapi.State{rootA, rootBDuplicateComponent}, regapi.State{rootA, rootBDuplicateComponent}, hardeningResolution(rootA, rootBDuplicateComponent))
	require.ErrorContains(t, err, "stored dependency resolution is invalid")
}

func TestDependencyHandler_ReconcileAcceptsStoredLabelSelectionOffline(t *testing.T) {
	ctx := newTestContext()
	root := hardeningRoot("app.deps:a", "acme/a", "@latest")
	module := hardeningModuleEntry("acme.a:entry", "acme/a", "v1.0.0")
	handler, err := NewDependencyHandler(DependencyHandlerOptions{Hub: &fakeHub{}, Logger: zap.NewNop(), VendorDir: t.TempDir()})
	require.NoError(t, err)
	resolution := hardeningResolution(root)
	result, err := handler.ReconcileResolution(ctx, regapi.State{root, module}, regapi.State{root, module}, resolution)
	require.NoError(t, err)
	require.Equal(t, resolution.Digest, result.Resolution.Digest)
}

func TestDependencyHandler_ReconcileReloadsOnlyModuleWithChangedRootParameters(t *testing.T) {
	ctx := newTestContext()
	vendorDir := t.TempDir()
	artifact := buildWappBytes(t, []wapp.Entry{
		{
			ID:   wapp.NewID("acme.feature", "scope"),
			Kind: regapi.NamespaceRequirement,
			Data: map[string]any{
				"targets": []any{map[string]any{"entry": "policy", "path": ".groups +="}},
			},
		},
		{
			ID:   wapp.NewID("acme.feature", "policy"),
			Kind: "security.policy",
			Data: map[string]any{"groups": []any{}},
		},
	})
	require.NoError(t, os.MkdirAll(filepath.Join(vendorDir, "acme"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(vendorDir, "acme", "feature-v1.0.0.wapp"), artifact, 0o600))
	sum := sha256.Sum256(artifact)
	digest := "sha256:" + hex.EncodeToString(sum[:])

	root := func(value string) regapi.Entry {
		return regapi.Entry{
			ID:             regapi.NewID("app.deps", "feature"),
			Kind:           regapi.NamespaceDependency,
			DependencyRoot: true,
			Data: payload.New(map[string]any{
				"component": "acme/feature",
				"version":   "v1.0.0",
				"parameters": []any{
					map[string]any{"name": "scope", "value": value},
				},
			}),
		}
	}
	moduleMeta := attrs.NewBagFrom(map[string]any{
		metaModuleKey:        "acme/feature",
		metaModuleVersionKey: "v1.0.0",
		metaModuleDigestKey:  digest,
	})
	requirement := regapi.Entry{
		ID:   regapi.NewID("acme.feature", "scope"),
		Kind: regapi.NamespaceRequirement,
		Meta: moduleMeta,
		Data: payload.New(map[string]any{
			"targets": []any{map[string]any{"entry": "policy", "path": ".groups +="}},
		}),
	}
	policy := regapi.Entry{
		ID:   regapi.NewID("acme.feature", "policy"),
		Kind: "security.policy",
		Meta: moduleMeta,
		Data: payload.New(map[string]any{"groups": []any{"scope:old"}}),
	}
	beforeRoot, targetRoot := root("scope:old"), root("scope:new")
	current := regapi.State{beforeRoot, requirement, policy}
	target := regapi.State{targetRoot, requirement, policy}
	resolution := hardeningResolution(targetRoot)
	resolution.Modules[0].Digest = digest
	resolution.Modules[0].SizeBytes = uint64(len(artifact))
	resolution = resolution.Canonical()

	handler, err := NewDependencyHandler(DependencyHandlerOptions{
		Hub:       &fakeHub{},
		Logger:    zap.NewNop(),
		VendorDir: vendorDir,
	})
	require.NoError(t, err)
	result, err := handler.ReconcileResolution(ctx, current, target, resolution)
	require.NoError(t, err)

	var updated *regapi.Entry
	for _, scoped := range result.Additional {
		if scoped.Operation.Kind == regapi.EntryUpdate && scoped.Operation.Entry.ID == policy.ID {
			entry := scoped.Operation.Entry
			updated = &entry
		}
	}
	require.NotNil(t, updated, "changed root parameter must update its linked target")
	data, ok := updated.Data.Data().(map[string]any)
	require.True(t, ok)
	require.Equal(t, []any{"scope:new"}, data["groups"], "reconciliation must link from raw artifact without duplicating append values")
}

func TestDependencyHandler_ReconcileRejectsUnsafeStoredArtifactIdentity(t *testing.T) {
	ctx := newTestContext()
	root := hardeningRoot("app.deps:a", "../escape", "v1.0.0")
	resolution := hardeningResolution(root)
	handler, err := NewDependencyHandler(DependencyHandlerOptions{Hub: &fakeHub{}, Logger: zap.NewNop(), VendorDir: t.TempDir()})
	require.NoError(t, err)
	_, err = handler.ReconcileResolution(ctx, regapi.State{root}, regapi.State{root}, resolution)
	require.ErrorContains(t, err, "invalid module name")

	safeRoot := hardeningRoot("app.deps:safe", "acme/safe", "v1.0.0")
	badDigest := hardeningResolution(safeRoot)
	badDigest.Modules[0].Digest = "sha256:deadbeef"
	badDigest = badDigest.Canonical()
	_, err = handler.ReconcileResolution(ctx, regapi.State{safeRoot}, regapi.State{safeRoot}, badDigest)
	require.ErrorContains(t, err, "invalid sha256 digest")

	require.Error(t, validateModuleArtifactIdentity(graph.Name{Organization: "acme", Module: "safe"}, "1.0.0/../../escape", ""))
	_, err = containedPath(t.TempDir(), filepath.Join("..", "escape.wapp"))
	require.ErrorContains(t, err, "escapes vendor")
	vendor := t.TempDir()
	require.NoError(t, os.Symlink(t.TempDir(), filepath.Join(vendor, "linked")))
	_, err = containedPath(vendor, filepath.Join("linked", "module.wapp"))
	require.ErrorContains(t, err, "traverses symlink")
}

func TestValidateDownloadInfoRejectsStoredIdentityDrift(t *testing.T) {
	mod := ResolvedModule{Org: "acme", Name: "safe", Version: "v1.0.0", Digest: "sha256:" + string(make([]byte, 64)), SizeBytes: 10}
	require.ErrorContains(t, validateDownloadInfo(mod, &DownloadInfo{Version: "v2.0.0"}), "version mismatch")
	require.ErrorContains(t, validateDownloadInfo(mod, &DownloadInfo{Version: "v1.0.0", Digest: "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"}), "digest mismatch")
	require.ErrorContains(t, validateDownloadInfo(mod, &DownloadInfo{Version: "v1.0.0", Size: 11}), "size mismatch")
}

func TestVerifyExtractedModuleRejectsContentTampering(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "module.yaml")
	require.NoError(t, os.WriteFile(file, []byte("value: original\n"), 0o600))
	digest := "sha256:0000000000000000000000000000000000000000000000000000000000000000"
	require.NoError(t, writeExtractedModuleMeta(dir, digest, 10))
	require.NoError(t, verifyExtractedModule(dir, digest, 10))

	require.NoError(t, os.WriteFile(file, []byte("value: tampered\n"), 0o600))
	require.ErrorContains(t, verifyExtractedModule(dir, digest, 10), "tree digest mismatch")
}
