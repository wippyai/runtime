// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/boot/build"
	"github.com/wippyai/runtime/boot/deps/lock"
	"github.com/wippyai/wapp"
	"go.uber.org/zap"
)

func TestModulePackDoesNotBakeHostProfileMutations(t *testing.T) {
	hostConfig := boot.NewConfig(
		boot.WithSection("override", map[string]any{"acme.ui:worker:value": "host-value"}),
		boot.WithSection("disable", map[string]any{"entries": []string{"acme.ui:extra"}}),
	)
	ctx, _, _, embedReg, err := bootstrapPackRuntimeWithDefaults(nil, zap.NewNop(), hostConfig)
	require.NoError(t, err)
	t.Cleanup(func() { _ = embedReg.Close() })

	newItems := func() []registry.Entry {
		return []registry.Entry{
			{ID: registry.NewID("acme.ui", "worker"), Kind: "service.test", Registry: registry.EntryMetadata{Owner: "acme/ui"}, Data: payload.New(map[string]any{"value": "module-value"})},
			{ID: registry.NewID("acme.ui", "extra"), Kind: "service.test", Registry: registry.EntryMetadata{Owner: "acme/ui"}},
		}
	}

	moduleItems := newItems()
	require.NoError(t, build.New(packNormalizationStages("acme/ui", nil, nil)...).Execute(ctx, &moduleItems))
	require.Len(t, moduleItems, 2)
	require.Equal(t, "module-value", moduleItems[0].Data.Data().(map[string]any)["value"])

	wholeGraphItems := newItems()
	require.NoError(t, build.New(packNormalizationStages("", nil, nil)...).Execute(ctx, &wholeGraphItems))
	require.Len(t, wholeGraphItems, 1)
	require.Equal(t, "host-value", wholeGraphItems[0].Data.Data().(map[string]any)["value"])
}

func TestResolvePackModuleUsesEffectiveSelectedPath(t *testing.T) {
	t.Parallel()

	paths := []lock.ModuleLoadPath{
		{Path: "/workspace/src", Root: true},
		{Path: "/workspace/replacement", Module: "acme/ui", Version: "1.2.3", Replacement: true},
	}

	selected, err := resolvePackModule("acme/ui", paths, nil)
	require.NoError(t, err)
	require.Equal(t, "/workspace/replacement", selected.Path)
	require.True(t, selected.Replacement)

	_, err = resolvePackModule("acme/missing", paths, nil)
	require.ErrorContains(t, err, "not selected by the lock file")

	_, err = resolvePackModule("acme", paths, nil)
	require.ErrorContains(t, err, "invalid --module")

	_, err = resolvePackModule(" acme/ui", paths, nil)
	require.ErrorContains(t, err, "--module must be a selected module")
}

func TestResolvePackModuleRejectsAmbiguousEffectivePaths(t *testing.T) {
	_, err := resolvePackModule("acme/ui", []lock.ModuleLoadPath{
		{Path: "/workspace/one", Module: "acme/ui", Version: "1.0.0"},
		{Path: "/workspace/two", Module: "acme/ui", Version: "1.0.0"},
	}, nil)
	require.ErrorContains(t, err, "multiple effective load paths")
}

func TestResolvePackModuleSelectsLockedRootApplicationSource(t *testing.T) {
	paths := []lock.ModuleLoadPath{{Path: "/workspace/src", Root: true}}

	selected, err := resolvePackModule("acme/app", paths, []string{"acme/app"})
	require.NoError(t, err)
	require.Equal(t, "/workspace/src", selected.Path)
	require.Equal(t, "acme/app", selected.Module)
	require.True(t, isRootModulePackSource(selected, paths))

	_, err = resolvePackModule("acme/app", paths, nil)
	require.ErrorContains(t, err, "not selected by the lock file")

	items := []registry.Entry{
		{ID: registry.NewID("app", "root")},
		{ID: registry.NewID("acme.ui", "dependency"), Registry: registry.EntryMetadata{Owner: "acme/ui"}},
	}
	assignRootModulePackOwner(items, "acme/app")
	require.Equal(t, "acme/app", items[0].Registry.Owner)
	require.Equal(t, "acme/ui", items[1].Registry.Owner)
}

func TestSelectModulePackEntriesUsesLoaderOwnership(t *testing.T) {
	items := []registry.Entry{
		{ID: registry.NewID("app", "root")},
		{ID: registry.NewID("acme.ui", "owned"), Registry: registry.EntryMetadata{Owner: "acme/ui"}},
		{ID: registry.NewID("other", "owned"), Registry: registry.EntryMetadata{Owner: "other/lib"}},
	}

	selected, err := selectModulePackEntries(items, "acme/ui")
	require.NoError(t, err)
	require.Len(t, selected, 1)
	require.Equal(t, "acme.ui:owned", selected[0].ID.String())

	_, err = selectModulePackEntries(items, "acme/missing")
	require.ErrorContains(t, err, "has no entries")
}

func TestModulePackLinkLeavesRootCompositionForDeployment(t *testing.T) {
	ctx, _, _, embedReg, err := bootstrapPackRuntime(nil, zap.NewNop())
	require.NoError(t, err)
	t.Cleanup(func() { _ = embedReg.Close() })

	items := []registry.Entry{
		{
			ID:       registry.NewID("acme.ui", "definition"),
			Kind:     registry.NamespaceDefinition,
			Registry: registry.EntryMetadata{Owner: "acme/ui"},
		},
		{
			ID:       registry.NewID("acme.ui", "setting"),
			Kind:     registry.NamespaceRequirement,
			Registry: registry.EntryMetadata{Owner: "acme/ui"},
			Data: payload.New(map[string]any{
				"default": "module-default",
				"targets": []any{map[string]any{"entry": "worker", "path": "value"}},
			}),
		},
		{
			ID:       registry.NewID("acme.ui", "worker"),
			Kind:     "service.test",
			Registry: registry.EntryMetadata{Owner: "acme/ui"},
			Data:     payload.New(map[string]any{"value": "before-link"}),
		},
		{
			ID:   registry.NewID("app.dependencies", "root"),
			Kind: registry.NamespaceDependency,
			Data: payload.New(map[string]any{
				"component":  "acme/ui",
				"parameters": []any{map[string]any{"name": "setting", "value": "root-composition"}},
			}),
		},
		{
			ID:       registry.NewID("acme.consumer", "dependency"),
			Kind:     registry.NamespaceDependency,
			Registry: registry.EntryMetadata{Owner: "acme/consumer"},
			Data: payload.New(map[string]any{
				"component":  "acme/ui",
				"parameters": []any{map[string]any{"name": "setting", "value": "consumer-composition"}},
			}),
		},
	}

	pipeline := build.New(packNormalizationStages("acme/ui", nil, nil)...)
	require.NoError(t, pipeline.Execute(ctx, &items))

	workerData, ok := items[2].Data.Data().(map[string]any)
	require.True(t, ok)
	require.Equal(t, "module-default", workerData["value"])
}

func TestModulePackMetadataFromManifestPreservesIdentityAndPublicationFields(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "wippy.yaml"), []byte(`
organization: acme
module: ui
version: 2.0.0
description: module description
metadata:
  provider_kind: lua
`), 0o644))

	metadata, err := modulePackMetadata(lock.ModuleLoadPath{
		Path:       filepath.Join(root, "src"),
		SourceRoot: root,
		Module:     "acme/ui",
		Version:    "1.9.0",
	}, "acme/ui")
	require.NoError(t, err)
	require.Equal(t, "ui", metadata["name"])
	require.Equal(t, "acme.ui", metadata["namespace"])
	require.Equal(t, "2.0.0", metadata["version"])
	require.Equal(t, "module description", metadata["description"])
	require.Equal(t, "lua", metadata["provider_kind"])
}

func TestModulePackMetadataRejectsManifestOwnerMismatch(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "wippy.yaml"), []byte("organization: other\nmodule: ui\n"), 0o644))

	_, err := modulePackMetadata(lock.ModuleLoadPath{Path: root, SourceRoot: root, Module: "acme/ui"}, "acme/ui")
	require.ErrorContains(t, err, "identifies")
}

func TestModulePackMetadataFromWappPreservesMetadataAndRejectsWrongIdentity(t *testing.T) {
	packPath := filepath.Join(t.TempDir(), "ui.wapp")
	file, err := os.Create(packPath)
	require.NoError(t, err)
	require.NoError(t, wapp.NewWriter().PackEntries(wapp.Metadata{
		"name":      "ui",
		"namespace": "acme.ui",
		"version":   "3.0.0",
		"provider":  "lua",
	}, nil, file))
	require.NoError(t, file.Close())

	metadata, err := modulePackMetadata(lock.ModuleLoadPath{Path: packPath, Module: "acme/ui", Version: "2.0.0"}, "acme/ui")
	require.NoError(t, err)
	require.Equal(t, "3.0.0", metadata["version"])
	require.Equal(t, "lua", metadata["provider"])

	wrongPack := filepath.Join(t.TempDir(), "wrong.wapp")
	file, err = os.Create(wrongPack)
	require.NoError(t, err)
	require.NoError(t, wapp.NewWriter().PackEntries(wapp.Metadata{
		"name": "other", "namespace": "other.lib", "version": "1.0.0",
	}, nil, file))
	require.NoError(t, file.Close())
	_, err = modulePackMetadata(lock.ModuleLoadPath{Path: wrongPack, Module: "acme/ui"}, "acme/ui")
	require.ErrorContains(t, err, "does not match")
}

func TestModulePackMetadataRejectsIdentityOverride(t *testing.T) {
	selected := attrs.Bag{"name": "ui", "namespace": "acme.ui", "version": "1.0.0"}
	metadata := attrs.Bag{"name": "other", "namespace": "acme.ui", "version": "1.0.0"}

	err := validateModulePackIdentityOverride(metadata, selected)
	require.ErrorContains(t, err, "cannot override")
}

func TestFilterModulePackResourcesUsesSelectedEntryIDs(t *testing.T) {
	resources := []wapp.ResourceSpec{
		{ID: wapp.NewID("acme.ui", "assets")},
		{ID: wapp.NewID("other.lib", "assets")},
	}
	items := []registry.Entry{{ID: registry.NewID("acme.ui", "assets")}}

	filtered := filterModulePackResources(resources, items)
	require.Len(t, filtered, 1)
	require.Equal(t, "acme.ui:assets", filtered[0].ID.String())
}

func TestCollectEmbeddedPackResourcesForModuleScopesWappResources(t *testing.T) {
	tmpDir := t.TempDir()
	selectedRoot := filepath.Join(tmpDir, "selected")
	otherRoot := filepath.Join(tmpDir, "other")
	require.NoError(t, os.MkdirAll(selectedRoot, 0o755))
	require.NoError(t, os.MkdirAll(otherRoot, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(selectedRoot, "selected.txt"), []byte("selected"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(otherRoot, "other.txt"), []byte("other"), 0o644))

	selectedPack := filepath.Join(tmpDir, "selected.wapp")
	otherPack := filepath.Join(tmpDir, "other.wapp")
	writeResourcePackForModuleTest(t, selectedPack, "acme/ui", "acme.ui:assets", selectedRoot)
	writeResourcePackForModuleTest(t, otherPack, "other/lib", "other.lib:assets", otherRoot)

	resources, handles, err := collectEmbeddedPackResourcesForModule([]lock.ModuleLoadPath{
		{Path: selectedPack, Module: "acme/ui", Version: "1.0.0"},
		{Path: otherPack, Module: "other/lib", Version: "1.0.0"},
	}, nil, "acme/ui")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, closeEmbeddedPackResourceHandles(handles)) })
	require.Len(t, resources, 1)
	require.Equal(t, "acme.ui:assets", resources[0].ID.String())
	content, err := fs.ReadFile(resources[0].FS, "selected.txt")
	require.NoError(t, err)
	require.Equal(t, "selected", string(content))
}

func writeResourcePackForModuleTest(t *testing.T, path, module, resourceID, root string) {
	t.Helper()
	parts := stringsSplitResourceID(resourceID)
	file, err := os.Create(path)
	require.NoError(t, err)
	require.NoError(t, wapp.NewWriter().PackWithResources(wapp.Metadata{
		"name":      parts[1],
		"namespace": parts[0],
		"version":   "1.0.0",
		"module":    module,
	}, []wapp.Entry{{ID: wapp.NewID(parts[0], parts[1]), Kind: "fs.embed"}}, []wapp.ResourceSpec{{
		ID: wapp.NewID(parts[0], parts[1]),
		FS: os.DirFS(root),
	}}, file))
	require.NoError(t, file.Close())
}

func stringsSplitResourceID(value string) [2]string {
	var result [2]string
	for i, part := range strings.SplitN(value, ":", 2) {
		if i < len(result) {
			result[i] = part
		}
	}
	return result
}
