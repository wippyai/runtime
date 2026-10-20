// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/env"
	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	dirapi "github.com/wippyai/runtime/api/service/fs/directory"
	embedapi "github.com/wippyai/runtime/api/service/fs/embed"
	"github.com/wippyai/runtime/boot/deps/lock"
	"github.com/wippyai/runtime/boot/deps/wappextract"
	"github.com/wippyai/wapp"
	"go.uber.org/zap"
)

func TestModuleRootRefreshedEntriesSelectsReplacedModuleDirectories(t *testing.T) {
	ctx := newTestContext()
	targetDir := filepath.Join(t.TempDir(), "vendor", "acme", "ui")
	cwd, err := os.Getwd()
	require.NoError(t, err)
	projectRelative, err := filepath.Rel(cwd, filepath.Join(targetDir, "assets"))
	require.NoError(t, err)
	ctx = env.WithRegistry(ctx, mapEnvRegistry{vars: map[string]string{"ACME_UI_ASSETS": filepath.Join(targetDir, "assets")}})

	state := regapi.State{
		moduleDirectoryEntry("acme.ui", "assets", map[string]any{"directory": "./assets", "base": "module"}, "a"),
		moduleDirectoryEntry("acme.ui", "data", map[string]any{"directory": "./data"}, "b"),
		moduleDirectoryEntry("acme.ui", "host", map[string]any{"directory": "/var/lib/acme"}, "c"),
		moduleDirectoryEntry("acme.ui", "project", map[string]any{"directory": "./assets", "base": "project"}, "d"),
		moduleDirectoryEntry("acme.ui", "env", map[string]any{"directory": "${env:ACME_UI_UNSET}"}, "e"),
		moduleDirectoryEntry("other.mod", "assets", map[string]any{"directory": "./assets", "base": "module"}, "f"),
		appDirectoryEntry("absolute", map[string]any{"directory": filepath.Join(targetDir, "assets")}, "g"),
		appDirectoryEntry("root", map[string]any{"directory": targetDir}, "h"),
		appDirectoryEntry("relative", map[string]any{"directory": projectRelative, "base": "project"}, "i"),
		appDirectoryEntry("placeholder", map[string]any{"directory": "${env:ACME_UI_ASSETS}"}, "j"),
		appDirectoryEntry("sibling", map[string]any{"directory": targetDir + "-backup"}, "k"),
		appDirectoryEntry("parent", map[string]any{"directory": filepath.Dir(targetDir)}, "l"),
		appDirectoryEntry("outside", map[string]any{"directory": "/var/lib/app"}, "m"),
		{ID: regapi.NewID("acme.ui", "service"), Kind: "service", Registry: regapi.EntryMetadata{Owner: "acme.ui"}},
	}

	keys := moduleRootRefreshedEntries(ctx, payload.GetTranscoder(ctx), []stagedModuleDirectory{
		{module: "acme.ui", targetDir: targetDir},
	}, state)

	assert.Equal(t, map[string]struct{}{
		idKey(regapi.NewID("acme.ui", "assets")):  {},
		idKey(regapi.NewID("acme.ui", "data")):    {},
		idKey(regapi.NewID("acme.ui", "host")):    {},
		idKey(regapi.NewID("acme.ui", "project")): {},
		idKey(regapi.NewID("acme.ui", "env")):     {},
		idKey(regapi.NewID("app", "absolute")):    {},
		idKey(regapi.NewID("app", "root")):        {},
		idKey(regapi.NewID("app", "relative")):    {},
		idKey(regapi.NewID("app", "placeholder")): {},
	}, keys)

	ids := moduleRootRefreshIDs(state, keys)
	assert.Len(t, ids, len(keys))
	assert.Nil(t, moduleRootRefreshIDs(state, nil))
}

func TestModuleRootRefreshedEntriesIgnoresUnreplacedModules(t *testing.T) {
	ctx := newTestContext()
	state := regapi.State{
		moduleDirectoryEntry("acme.ui", "assets", map[string]any{"directory": "./assets", "base": "module"}, "a"),
	}

	keys := moduleRootRefreshedEntries(ctx, payload.GetTranscoder(ctx), nil, state)
	assert.Empty(t, keys)

	keys = moduleRootRefreshedEntries(ctx, payload.GetTranscoder(ctx), []stagedModuleDirectory{
		{module: "other.mod", targetDir: "/tmp/other/mod"},
	}, state)
	assert.Empty(t, keys)
}

// A module update replaces the whole module tree. An fs.directory entry whose
// path resolves inside that tree has the same configuration in both versions,
// so the entry diff alone plans nothing and the registered filesystem keeps
// the handle of the removed directory. The update must re-register it.
func TestDependencyHandlerUpdateRefreshesModuleRootFilesystemEntry(t *testing.T) {
	ctx := newTestContext()
	root := t.TempDir()
	lockPath := filepath.Join(root, lock.DefaultFilename)
	vendorDir := filepath.Join(root, "vendor")
	targetDir := filepath.Join(vendorDir, "acme", "ui")

	versions := map[string][]byte{
		"1.0.0": buildModuleWapp(t, "1.0.0", "<html>v1</html>"),
		"2.0.0": buildModuleWapp(t, "2.0.0", "<html>v2</html>"),
	}
	digests := make(map[string]string, len(versions))
	for version, artifact := range versions {
		sum := sha256.Sum256(artifact)
		digests[version] = "sha256:" + hex.EncodeToString(sum[:])
		writeModuleWapp(t, filepath.Join(vendorDir, "acme", "ui-"+version+".wapp"), artifact)
	}
	require.NoError(t, wappextract.ExtractWappToDirKeepSource(
		filepath.Join(vendorDir, "acme", "ui-1.0.0.wapp"), targetDir,
	))

	lockObj, err := lock.New(lockPath)
	require.NoError(t, err)
	lockObj.SetDirectories(lock.Directories{Modules: "vendor", Src: "."})
	lockObj.SetOptions(lock.Options{UnpackModules: true})
	lockObj.SetModule(lock.Module{Name: "acme/ui", Version: "1.0.0", Hash: digests["1.0.0"]})
	lockObj.SetRootModule("acme/ui")
	require.NoError(t, lockObj.Write())

	handler, err := NewDependencyHandler(DependencyHandlerOptions{
		Hub: &fakeHub{
			getManifest: func(_ context.Context, org, module, version string) (*ModuleManifest, error) {
				artifact, ok := versions[version]
				if !ok {
					return nil, NewDependencyIntegrityError(org+"/"+module, os.ErrNotExist, version, 0)
				}
				return &ModuleManifest{
					Org: org, Name: module, Version: version, VersionID: version,
					Digest: digests[version], SizeBytes: uint64(len(artifact)),
				}, nil
			},
		},
		Logger:    zap.NewNop(),
		LockPath:  lockPath,
		VendorDir: vendorDir,
	})
	require.NoError(t, err)

	installed, err := loadRawEntriesFromPaths(ctx, []string{targetDir}, zap.NewNop(), payload.GetTranscoder(ctx))
	require.NoError(t, err)
	snapshot := make(regapi.State, 0, len(installed)+1)
	for _, entry := range installed {
		snapshot = append(snapshot, markModuleEntry(entry, "acme/ui"))
	}
	rootDep := regapi.Entry{
		ID: regapi.NewID("app.deps", "ui"), Kind: regapi.NamespaceDependency,
		Registry: regapi.EntryMetadata{Root: true},
		Data:     payload.New(map[string]any{"component": "acme/ui", "version": "1.0.0"}),
	}
	snapshot = append(snapshot, rootDep)

	updated := rootDep
	updated.Data = payload.New(map[string]any{"component": "acme/ui", "version": "2.0.0"})
	result, err := handler.Expand(ctx, regapi.Operation{Kind: regapi.EntryUpdate, Entry: updated}, snapshot)
	require.NoError(t, err)

	assetsID := regapi.NewID("acme.ui", "assets")
	var assets *regapi.Operation
	var definition *regapi.Operation
	var service *regapi.Operation
	for i := range result.Additional {
		op := result.Additional[i].Operation
		switch op.Entry.ID {
		case assetsID:
			assets = &op
		case regapi.NewID("acme.ui", "definition"):
			definition = &op
		case regapi.NewID("acme.ui", "service"):
			service = &op
		}
	}

	require.NotNil(t, service, "the updated module entry must be planned")
	assert.Equal(t, regapi.EntryUpdate, service.Kind)
	require.NotNil(t, assets, "a module-relative filesystem entry must be re-registered after its tree is replaced")
	assert.Equal(t, regapi.EntryUpdate, assets.Kind)
	assert.Nil(t, definition, "an unchanged entry outside a replaced directory must not be re-registered")
	assert.Equal(t, []regapi.ID{assetsID}, result.Refresh, "a version transition must re-register the entry after its state delta")
}

func buildModuleWapp(t *testing.T, version, page string) []byte {
	t.Helper()
	entries := []wapp.Entry{
		{ID: wapp.NewID("acme.ui", "definition"), Kind: regapi.NamespaceDefinition},
		{ID: wapp.NewID("acme.ui", "service"), Kind: "service", Data: map[string]any{"version": version}},
		{ID: wapp.NewID("acme.ui", "assets"), Kind: embedapi.Kind},
	}
	resource := fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte(page), Mode: fs.FileMode(0o644)},
	}
	var buf bytes.Buffer
	require.NoError(t, wapp.NewWriter().PackWithResources(
		wapp.Metadata{"version": version},
		entries,
		[]wapp.ResourceSpec{{ID: wapp.NewID("acme.ui", "assets"), FS: resource}},
		&buf,
	))
	return buf.Bytes()
}

func writeModuleWapp(t *testing.T, path string, artifact []byte) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, artifact, 0o600))
}

func moduleDirectoryEntry(module, name string, data map[string]any, meta string) regapi.Entry {
	return regapi.Entry{
		ID:       regapi.NewID(module, name),
		Kind:     dirapi.Kind,
		Registry: regapi.EntryMetadata{Owner: module},
		Meta:     attrs.NewBagFrom(map[string]any{"test": meta}),
		Data:     payload.New(data),
	}
}

func appDirectoryEntry(name string, data map[string]any, meta string) regapi.Entry {
	entry := moduleDirectoryEntry("app", name, data, meta)
	entry.Registry.Owner = ""
	return entry
}

type mapEnvRegistry struct {
	env.Registry
	vars map[string]string
}

func (m mapEnvRegistry) Get(_ context.Context, name string) (string, error) {
	if value, ok := m.vars[name]; ok {
		return value, nil
	}
	return "", env.ErrVariableNotFound
}
