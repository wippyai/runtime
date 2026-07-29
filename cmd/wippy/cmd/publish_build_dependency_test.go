// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	depconfig "github.com/wippyai/runtime/boot/deps/config"
	"github.com/wippyai/runtime/boot/deps/graph"
	"github.com/wippyai/runtime/boot/deps/lock"
	appinit "github.com/wippyai/runtime/cmd/internal/app"
	"github.com/wippyai/wapp"
)

const validBuildDigest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func TestPackModuleStripsBuildDependencies(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "src"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "src", "_index.yaml"), []byte(`version: "1.0"
namespace: app
entries:
  - name: module
    kind: ns.definition
  - name: frontend
    kind: ns.build_dependency
    component: acme/frontend
    version: "2.0.0"
`), 0o644))
	lockObj, err := lock.New(filepath.Join(root, lock.DefaultFilename))
	require.NoError(t, err)
	digest := writeBuildArtifact(t, lockObj, "acme/frontend", "2.0.0", []byte("frontend package"))
	lockObj.SetModule(lock.Module{Name: "acme/frontend", Version: "2.0.0", Hash: digest, BuildOnly: true})
	require.NoError(t, lockObj.Write())
	app, err := appinit.Init(context.Background(), false, false, false, true, time.Now())
	require.NoError(t, err)
	output := filepath.Join(root, "consumer.wapp")
	_, err = packModule(app.Ctx, app, &depconfig.ModuleConfig{
		Organization:  "acme",
		ModuleName:    "consumer",
		Version:       "1.0.0",
		RequiresWippy: ">=1.2.0",
	}, root, output, nil)
	require.NoError(t, err)

	file, err := os.Open(output)
	require.NoError(t, err)
	defer file.Close()
	reader, err := wapp.NewReader(file)
	require.NoError(t, err)
	packedEntries, err := reader.GetEntries()
	require.NoError(t, err)
	for _, entry := range packedEntries {
		require.NotEqual(t, regapi.NamespaceBuildDependency, entry.Kind)
	}
	metadata, err := reader.GetMetadata()
	require.NoError(t, err)
	require.Equal(t, ">=1.2.0", metadata["requires_wippy"])
	encoded, err := json.Marshal(metadata["fe_provenance"])
	require.NoError(t, err)
	require.JSONEq(t, fmt.Sprintf(`{"manifest_version":1,"imports":[{"entry":"app:frontend","module":"acme/frontend","version":"2.0.0","digest":%q}]}`, digest), string(encoded))
}

func TestLoadPublishRuntimeConfigRejectsWorkspaceBuildReplacement(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "src"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "local", "frontend"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "src", "_index.yaml"), []byte(`version: "1.0"
namespace: app
entries:
  - name: module
    kind: ns.definition
  - name: frontend
    kind: ns.build_dependency
    component: acme/frontend
    version: "2.0.0"
`), 0o644))
	lockObj, err := lock.New(filepath.Join(root, lock.DefaultFilename))
	require.NoError(t, err)
	digest := writeBuildArtifact(t, lockObj, "acme/frontend", "2.0.0", []byte("remote package"))
	lockObj.SetModule(lock.Module{Name: "acme/frontend", Version: "2.0.0", Hash: digest, BuildOnly: true, BuildDependency: true})
	require.NoError(t, lockObj.Write())
	runtimeConfigPath := filepath.Join(root, ".wippy.yaml")
	require.NoError(t, os.WriteFile(runtimeConfigPath, []byte(`version: "1.0"
workspace:
  replacements:
    acme/frontend: local/frontend
`), 0o644))
	setTestConfigFiles(t, runtimeConfigPath)
	app, err := appinit.Init(context.Background(), false, false, false, true, time.Now())
	require.NoError(t, err)
	require.NoError(t, loadPublishRuntimeConfig(&cobra.Command{}, app))

	_, err = packModule(app.Ctx, app, &depconfig.ModuleConfig{
		Organization: "acme", ModuleName: "consumer", Version: "1.0.0", RequiresWippy: ">=1.2.0",
	}, root, filepath.Join(root, "consumer.wapp"), nil)
	require.ErrorContains(t, err, "build dependency acme/frontend uses a local replacement")
}

func TestStripBuildDependenciesRejectsStaleLockVersion(t *testing.T) {
	ctx := setupLoaderContext(t)
	lockObj, err := lock.New(filepath.Join(t.TempDir(), lock.DefaultFilename))
	require.NoError(t, err)
	lockObj.SetModule(lock.Module{
		Name:      "acme/frontend",
		Version:   "1.0.0",
		Hash:      validBuildDigest,
		BuildOnly: true,
	})
	entries := []regapi.Entry{{
		ID:   regapi.NewID("app.deps", "frontend"),
		Kind: regapi.NamespaceBuildDependency,
		Data: payload.New(map[string]any{"component": "acme/frontend", "version": "2.0.0"}),
	}}

	_, _, err = stripBuildDependencies(ctx, payload.GetTranscoder(ctx), entries, lockObj)
	require.EqualError(t, err, "build dependency app.deps:frontend requires acme/frontend@2.0.0 but lock selects 1.0.0")
}

func TestStripBuildDependenciesRejectsVersionRanges(t *testing.T) {
	ctx := setupLoaderContext(t)
	lockObj, err := lock.New(filepath.Join(t.TempDir(), lock.DefaultFilename))
	require.NoError(t, err)
	lockObj.SetModule(lock.Module{
		Name: "acme/frontend", Version: "2.0.0", Hash: validBuildDigest, BuildOnly: true,
	})
	entries := []regapi.Entry{{
		ID:   regapi.NewID("app.deps", "frontend"),
		Kind: regapi.NamespaceBuildDependency,
		Data: payload.New(map[string]any{"component": "acme/frontend", "version": ">=2.0.0"}),
	}}

	_, _, err = stripBuildDependencies(ctx, payload.GetTranscoder(ctx), entries, lockObj)
	require.EqualError(t, err, "build dependency app.deps:frontend must use an exact semver version: >=2.0.0")
}

func TestStripBuildDependenciesRecordsLockedProvenance(t *testing.T) {
	ctx := setupLoaderContext(t)
	lockObj, err := lock.New(filepath.Join(t.TempDir(), lock.DefaultFilename))
	require.NoError(t, err)
	digest := writeBuildArtifact(t, lockObj, "acme/frontend", "2.0.0", []byte("frontend package"))
	lockObj.SetModule(lock.Module{
		Name:      "acme/frontend",
		Version:   "2.0.0",
		Hash:      digest,
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
				Digest:  digest,
			},
		},
	}, provenance)
}

func TestStripBuildDependenciesRejectsReplacement(t *testing.T) {
	ctx := setupLoaderContext(t)
	root := t.TempDir()
	lockObj, err := lock.New(filepath.Join(root, lock.DefaultFilename))
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Join(root, "local", "frontend"), 0o755))
	digest := writeBuildArtifact(t, lockObj, "acme/frontend", "2.0.0", []byte("frontend package"))
	lockObj.SetModule(lock.Module{Name: "acme/frontend", Version: "2.0.0", Hash: digest, BuildOnly: true})
	lockObj.SetReplacement(lock.Replacement{From: "acme/frontend", To: "local/frontend"})
	entries := []regapi.Entry{{
		ID:   regapi.NewID("app.deps", "frontend"),
		Kind: regapi.NamespaceBuildDependency,
		Data: payload.New(map[string]any{"component": "acme/frontend", "version": "2.0.0"}),
	}}

	_, _, err = stripBuildDependencies(ctx, payload.GetTranscoder(ctx), entries, lockObj)
	require.EqualError(t, err, "build dependency acme/frontend uses a local replacement")
}

func TestStripBuildDependenciesRejectsCorruptedArtifact(t *testing.T) {
	ctx := setupLoaderContext(t)
	lockObj, err := lock.New(filepath.Join(t.TempDir(), lock.DefaultFilename))
	require.NoError(t, err)
	digest := writeBuildArtifact(t, lockObj, "acme/frontend", "2.0.0", []byte("expected"))
	lockObj.SetModule(lock.Module{Name: "acme/frontend", Version: "2.0.0", Hash: digest, BuildOnly: true})
	writeBuildArtifact(t, lockObj, "acme/frontend", "2.0.0", []byte("corrupted"))
	entries := []regapi.Entry{{
		ID:   regapi.NewID("app.deps", "frontend"),
		Kind: regapi.NamespaceBuildDependency,
		Data: payload.New(map[string]any{"component": "acme/frontend", "version": "2.0.0"}),
	}}

	_, _, err = stripBuildDependencies(ctx, payload.GetTranscoder(ctx), entries, lockObj)
	require.ErrorContains(t, err, "verify build dependency acme/frontend@2.0.0")
	require.ErrorContains(t, err, "digest mismatch")
}

func writeBuildArtifact(t *testing.T, lockObj *lock.Lock, moduleName, moduleVersion string, content []byte) string {
	t.Helper()
	name, err := graph.ParseName(moduleName)
	require.NoError(t, err)
	path := filepath.Join(filepath.Dir(lockObj.Path()), lockObj.GetVendorPath(), lock.WappPath(name, moduleVersion))
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, content, 0o644))
	digest := sha256.Sum256(content)
	return fmt.Sprintf("sha256:%x", digest)
}
