// SPDX-License-Identifier: MPL-2.0

package entries

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	depconfig "github.com/wippyai/runtime/boot/deps/config"
	"github.com/wippyai/runtime/boot/deps/hub"
	"github.com/wippyai/runtime/boot/deps/lock"
	"github.com/wippyai/wapp"
	"go.uber.org/zap"
)

func TestExtractInstalledBuildModulePreservesVerifiedArtifact(t *testing.T) {
	root := t.TempDir()
	wappPath := createTestWappFile(t, root, "frontend", []wapp.Entry{{
		ID: wapp.NewID("frontend", "package"), Kind: "ns.definition",
	}})
	content, err := os.ReadFile(wappPath)
	require.NoError(t, err)
	digest := sha256.Sum256(content)
	dirPath := filepath.Join(root, "frontend")

	require.NoError(t, extractInstalledModule(wappPath, dirPath, true))
	require.FileExists(t, wappPath)
	require.DirExists(t, dirPath)
	valid, err := hub.VerifyCachedArtifact(wappPath, fmt.Sprintf("sha256:%x", digest))
	require.NoError(t, err)
	require.True(t, valid)

	tamperedPath := filepath.Join(dirPath, "tampered.txt")
	require.NoError(t, os.WriteFile(tamperedPath, []byte("tampered"), 0o644))
	require.NoError(t, extractInstalledModule(wappPath, dirPath, true))
	require.NoFileExists(t, tamperedPath)
	require.FileExists(t, wappPath)
}

func TestVerifyCachedArtifactRejectsCorruption(t *testing.T) {
	path := filepath.Join(t.TempDir(), "module.wapp")
	content := []byte("module")
	require.NoError(t, os.WriteFile(path, content, 0o644))
	digest := sha256.Sum256(content)
	pinned := fmt.Sprintf("sha256:%x", digest)

	valid, err := hub.VerifyCachedArtifact(path, pinned)
	require.NoError(t, err)
	require.True(t, valid)

	require.NoError(t, os.WriteFile(path, []byte("corrupted"), 0o644))
	valid, err = hub.VerifyCachedArtifact(path, pinned)
	require.False(t, valid)
	require.ErrorContains(t, err, "digest mismatch")
}

func TestPrepareRuntimeEntriesRejectsMissingRequiresWippy(t *testing.T) {
	entries := []regapi.Entry{{
		ID:   regapi.NewID("app.dependencies", "frontend"),
		Kind: regapi.NamespaceBuildDependency,
		Data: payload.New(map[string]any{"component": "acme/frontend", "version": "1.0.0"}),
	}}

	_, err := prepareRuntimeEntries(entries, &depconfig.ModuleConfig{}, "1.2.0")
	require.EqualError(t, err, "ns.build_dependency requires requires_wippy in wippy.yaml")
}

func TestLoadEntriesWithModuleMetaValidatesOwningManifest(t *testing.T) {
	ctx := setupTestContext(t)
	root := t.TempDir()
	appRoot := filepath.Join(root, "app")
	replacementRoot := filepath.Join(root, "replacement")
	require.NoError(t, os.MkdirAll(filepath.Join(appRoot, "src"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(replacementRoot, "src"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(appRoot, "wippy.yaml"), []byte("requires_wippy: '>=0.0.0'\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(appRoot, "src", "_index.yaml"), []byte(`version: "1.0"
namespace: app
entries:
  - name: runtime
    kind: function.lua
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(replacementRoot, "wippy.yaml"), []byte("organization: local\nmodule: frontend\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(replacementRoot, "src", "_index.yaml"), []byte(`version: "1.0"
namespace: local.frontend
entries:
  - name: package
    kind: ns.build_dependency
    component: acme/package
    version: 2.0.0
`), 0o644))
	paths := []lock.ModuleLoadPath{
		{Path: filepath.Join(appRoot, "src"), SourceRoot: appRoot, Root: true},
		{Path: filepath.Join(replacementRoot, "src"), SourceRoot: replacementRoot, Module: "local/frontend"},
	}

	_, err := loadEntriesWithModuleMeta(ctx, paths, zap.NewNop())
	require.EqualError(t, err, "ns.build_dependency requires requires_wippy in wippy.yaml")

	require.NoError(t, os.WriteFile(filepath.Join(replacementRoot, "wippy.yaml"), []byte("organization: local\nmodule: frontend\nrequires_wippy: '>=0.0.0'\n"), 0o644))
	loaded, err := loadEntriesWithModuleMeta(ctx, paths, zap.NewNop())
	require.NoError(t, err)
	require.Len(t, loaded, 1)
	require.Equal(t, regapi.Kind("function.lua"), loaded[0].Kind)
}

func TestValidateBuildOnlyReplacementSourcesUsesArtifactPaths(t *testing.T) {
	ctx := setupTestContext(t)
	root := t.TempDir()
	replacementRoot := filepath.Join(root, "replacement")
	require.NoError(t, os.MkdirAll(filepath.Join(replacementRoot, "src"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(replacementRoot, "wippy.yaml"), []byte("organization: local\nmodule: frontend\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(replacementRoot, "src", "_index.yaml"), []byte(`version: "1.0"
namespace: local.frontend
entries:
  - name: package
    kind: ns.build_dependency
    component: acme/package
    version: 2.0.0
`), 0o644))
	lockObj, err := lock.New(filepath.Join(root, lock.DefaultFilename))
	require.NoError(t, err)
	lockObj.SetModule(lock.Module{Name: "local/frontend", Version: "1.0.0", BuildOnly: true})
	lockObj.SetReplacement(lock.Replacement{From: "local/frontend", To: "replacement"})
	require.NoError(t, lock.Validate(lockObj))

	err = validateBuildOnlyReplacementSources(ctx, lockObj, zap.NewNop())
	require.EqualError(t, err, "ns.build_dependency requires requires_wippy in wippy.yaml")

	require.NoError(t, os.WriteFile(filepath.Join(replacementRoot, "wippy.yaml"), []byte("organization: local\nmodule: frontend\nrequires_wippy: '>=0.0.0'\n"), 0o644))
	require.NoError(t, validateBuildOnlyReplacementSources(ctx, lockObj, zap.NewNop()))
}

func TestPrepareRuntimeEntriesStripsCompatibleBuildDependencies(t *testing.T) {
	runtimeEntry := regapi.Entry{ID: regapi.NewID("app", "runtime"), Kind: "function.lua"}
	entries := []regapi.Entry{
		runtimeEntry,
		{
			ID:   regapi.NewID("app.dependencies", "frontend"),
			Kind: regapi.NamespaceBuildDependency,
			Data: payload.New(map[string]any{"component": "acme/frontend", "version": "1.0.0"}),
		},
	}

	prepared, err := prepareRuntimeEntries(entries, &depconfig.ModuleConfig{RequiresWippy: ">=1.2.0"}, "1.2.0")
	require.NoError(t, err)
	require.Equal(t, []regapi.Entry{runtimeEntry}, prepared)
}
