// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"bytes"
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	ctxapi "github.com/wippyai/runtime/api/context"
	moduleapi "github.com/wippyai/runtime/api/modules"
	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	embedapi "github.com/wippyai/runtime/api/service/fs/embed"
	"github.com/wippyai/runtime/boot/deps/graph"
	"github.com/wippyai/runtime/boot/deps/lock"
	embedpkg "github.com/wippyai/runtime/service/fs/embed"
	yamlpayload "github.com/wippyai/runtime/system/payload/yaml"
	"github.com/wippyai/wapp"
	"go.uber.org/zap"
)

// stubPackRegistry records pack lifecycle calls for assertions.
type stubPackRegistry struct {
	registerErr    error
	registered     map[string]*wapp.Reader
	files          map[string]*os.File
	unregistered   []string
	modulesDropped []droppedModule
}

type droppedModule struct {
	module  string
	version string
}

func newStubPackRegistry() *stubPackRegistry {
	return &stubPackRegistry{
		registered: make(map[string]*wapp.Reader),
		files:      make(map[string]*os.File),
	}
}

func (s *stubPackRegistry) RegisterPack(packPath, _, _ string, reader *wapp.Reader, file *os.File) error {
	if s.registerErr != nil {
		return s.registerErr
	}
	s.registered[packPath] = reader
	s.files[packPath] = file
	return nil
}

func (s *stubPackRegistry) UnregisterPack(packPath string) error {
	s.unregistered = append(s.unregistered, packPath)
	if f := s.files[packPath]; f != nil {
		_ = f.Close()
	}
	delete(s.registered, packPath)
	delete(s.files, packPath)
	return nil
}

func (s *stubPackRegistry) UnregisterModule(module, version string) error {
	s.modulesDropped = append(s.modulesDropped, droppedModule{module: module, version: version})
	return nil
}

func (s *stubPackRegistry) Close() error {
	var firstErr error
	for packPath, f := range s.files {
		if f != nil {
			if err := f.Close(); err != nil && firstErr == nil {
				firstErr = err
			}
		}
		delete(s.files, packPath)
		delete(s.registered, packPath)
	}
	return firstErr
}

// writeResourceWapp writes a .wapp containing one filesystem resource.
func writeResourceWapp(t *testing.T, path, ns, name string, files map[string]string) {
	t.Helper()

	mapFS := fstest.MapFS{}
	for p, content := range files {
		mapFS[p] = &fstest.MapFile{Data: []byte(content), Mode: 0644}
	}

	var buf bytes.Buffer
	writer := wapp.NewWriter()
	require.NoError(t, writer.PackWithResources(
		wapp.Metadata{},
		nil,
		[]wapp.ResourceSpec{{ID: wapp.NewID(ns, name), FS: mapFS}},
		&buf,
	))

	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0755))
	require.NoError(t, os.WriteFile(path, buf.Bytes(), 0600))
}

func newEffect(reg embedPackRegistry, staged []stagedPack, obsolete []obsoletePack) *embedPackEffect {
	return &embedPackEffect{
		reg:      reg,
		staged:   staged,
		obsolete: obsolete,
		logger:   zap.NewNop(),
	}
}

func TestEmbedPackEffect_PrepareCommit(t *testing.T) {
	dir := t.TempDir()
	packPath := filepath.Join(dir, "org", "mod-v1.0.0.wapp")
	writeResourceWapp(t, packPath, "ui", "app", map[string]string{"index.html": "<html>"})

	reg := newStubPackRegistry()
	defer func() { require.NoError(t, reg.Close()) }()
	eff := newEffect(reg, []stagedPack{{packPath: packPath, module: "org/mod", version: "1.0.0"}}, nil)

	require.NoError(t, eff.Prepare(context.Background()))
	assert.Contains(t, reg.registered, packPath)
	require.Len(t, eff.prepared, 1)

	require.NoError(t, eff.Commit(context.Background()))
	require.NoError(t, eff.Finalize(context.Background()))
	// Commit must not unregister the staged pack on a fresh install.
	assert.Empty(t, reg.unregistered)
	assert.Empty(t, reg.modulesDropped)
}

func TestEmbedPackEffect_Rollback(t *testing.T) {
	dir := t.TempDir()
	packPath := filepath.Join(dir, "org", "mod-v1.0.0.wapp")
	writeResourceWapp(t, packPath, "ui", "app", map[string]string{"index.html": "<html>"})

	reg := newStubPackRegistry()
	eff := newEffect(reg, []stagedPack{{packPath: packPath, module: "org/mod", version: "1.0.0"}}, nil)

	require.NoError(t, eff.Prepare(context.Background()))
	require.NoError(t, eff.Rollback(context.Background()))

	assert.Contains(t, reg.unregistered, packPath)
	assert.NotContains(t, reg.registered, packPath)
	assert.Nil(t, eff.prepared)
}

func TestEmbedPackEffect_Update(t *testing.T) {
	dir := t.TempDir()
	newPack := filepath.Join(dir, "org", "mod-v2.0.0.wapp")
	writeResourceWapp(t, newPack, "ui", "app", map[string]string{"index.html": "<html>v2"})

	reg := newStubPackRegistry()
	defer func() { require.NoError(t, reg.Close()) }()
	eff := newEffect(reg,
		[]stagedPack{{packPath: newPack, module: "org/mod", version: "2.0.0"}},
		[]obsoletePack{{module: "org/mod", version: "1.0.0"}},
	)

	require.NoError(t, eff.Prepare(context.Background()))
	assert.Contains(t, reg.registered, newPack)

	require.NoError(t, eff.Commit(context.Background()))
	require.NoError(t, eff.Finalize(context.Background()))
	// Old version is dropped only after durable finalization; new pack remains registered.
	require.Len(t, reg.modulesDropped, 1)
	assert.Equal(t, droppedModule{module: "org/mod", version: "1.0.0"}, reg.modulesDropped[0])
	assert.Contains(t, reg.registered, newPack)
}

func TestEmbedPackEffect_UpdateRollbackKeepsOld(t *testing.T) {
	dir := t.TempDir()
	newPack := filepath.Join(dir, "org", "mod-v2.0.0.wapp")
	writeResourceWapp(t, newPack, "ui", "app", map[string]string{"index.html": "<html>v2"})

	reg := newStubPackRegistry()
	eff := newEffect(reg,
		[]stagedPack{{packPath: newPack, module: "org/mod", version: "2.0.0"}},
		[]obsoletePack{{module: "org/mod", version: "1.0.0"}},
	)

	require.NoError(t, eff.Prepare(context.Background()))
	require.NoError(t, eff.Commit(context.Background()))
	require.NoError(t, eff.Rollback(context.Background()))

	// Commit remains reversible: the new pack is removed and the old version is
	// not dropped until the separate durable Finalize phase.
	assert.Contains(t, reg.unregistered, newPack)
	assert.Empty(t, reg.modulesDropped)
}

func TestEmbedPackEffect_Uninstall(t *testing.T) {
	reg := newStubPackRegistry()
	eff := newEffect(reg, nil, []obsoletePack{{module: "org/mod", version: "1.0.0"}})

	require.NoError(t, eff.Prepare(context.Background()))
	assert.Empty(t, reg.registered)

	require.NoError(t, eff.Commit(context.Background()))
	require.NoError(t, eff.Finalize(context.Background()))
	require.Len(t, reg.modulesDropped, 1)
	assert.Equal(t, droppedModule{module: "org/mod", version: "1.0.0"}, reg.modulesDropped[0])
}

func TestEmbedPackEffect_PrepareMissingPackFails(t *testing.T) {
	reg := newStubPackRegistry()
	eff := newEffect(reg, []stagedPack{{packPath: "/does/not/exist.wapp", module: "org/mod", version: "1.0.0"}}, nil)

	err := eff.Prepare(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "open embedded pack failed")
}

func TestEmbedPackEffect_PreparePartialFailureRollsBackStagedPacks(t *testing.T) {
	dir := t.TempDir()
	firstPack := filepath.Join(dir, "org", "mod-v1.0.0.wapp")
	missingPack := filepath.Join(dir, "org", "missing-v1.0.0.wapp")
	writeResourceWapp(t, firstPack, "ui", "app", map[string]string{"index.html": "<html>"})

	reg := newStubPackRegistry()
	eff := newEffect(reg, []stagedPack{
		{packPath: firstPack, module: "org/mod", version: "1.0.0"},
		{packPath: missingPack, module: "org/missing", version: "1.0.0"},
	}, nil)

	err := eff.Prepare(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "open embedded pack failed")
	assert.Contains(t, reg.unregistered, firstPack)
	assert.NotContains(t, reg.registered, firstPack)
	assert.Nil(t, eff.prepared)
}

func TestObsoletePacksFor(t *testing.T) {
	snapshot := regapi.State{
		moduleEntry("ui", "old-app", "org/mod", "1.0.0"),
		moduleEntry("ui", "stable", "org/other", "3.0.0"),
		moduleEntry("ui", "noversion", "org/nover", ""),
	}

	t.Run("update marks changed version obsolete", func(t *testing.T) {
		desired := map[string]string{"org/mod": "2.0.0", "org/other": "3.0.0"}
		controlled := map[string]struct{}{"org/mod": {}, "org/other": {}}
		obs := obsoletePacksFor(snapshot, desired, controlled)
		require.Len(t, obs, 1)
		assert.Equal(t, obsoletePack{module: "org/mod", version: "1.0.0"}, obs[0])
	})

	t.Run("removal marks dropped module obsolete", func(t *testing.T) {
		desired := map[string]string{"org/other": "3.0.0"}
		controlled := map[string]struct{}{"org/mod": {}, "org/other": {}}
		obs := obsoletePacksFor(snapshot, desired, controlled)
		require.Len(t, obs, 1)
		assert.Equal(t, obsoletePack{module: "org/mod", version: "1.0.0"}, obs[0])
	})

	t.Run("unchanged versions are not obsolete", func(t *testing.T) {
		desired := map[string]string{"org/mod": "1.0.0", "org/other": "3.0.0"}
		controlled := map[string]struct{}{"org/mod": {}, "org/other": {}}
		obs := obsoletePacksFor(snapshot, desired, controlled)
		assert.Empty(t, obs)
	})

	t.Run("unrelated modules remain live", func(t *testing.T) {
		desired := map[string]string{"org/mod": "2.0.0"}
		controlled := map[string]struct{}{"org/mod": {}}
		obs := obsoletePacksFor(snapshot, desired, controlled)
		require.Len(t, obs, 1)
		assert.Equal(t, obsoletePack{module: "org/mod", version: "1.0.0"}, obs[0])
	})
}

func TestBuildEmbedPackEffect_NoRegistry(t *testing.T) {
	handler, err := NewDependencyHandler(DependencyHandlerOptions{
		Hub:       &fakeHub{},
		Logger:    zap.NewNop(),
		VendorDir: t.TempDir(),
	})
	require.NoError(t, err)

	// No embed registry installed in context: effect is skipped.
	eff, err := handler.buildEmbedPackEffect(newTestContext(), nil, nil, nil)
	require.NoError(t, err)
	assert.Nil(t, eff)
}

func TestBuildEmbedPackEffect_SkipsUnchangedResolvedPack(t *testing.T) {
	reg := embedpkg.NewRegistry()
	defer func() { require.NoError(t, reg.Close()) }()
	ctx := embedapi.WithRegistry(newTestContext(), reg)
	vendorDir := t.TempDir()
	packPath := filepath.Join(vendorDir, "org", "mod-1.0.0.wapp")
	writeResourceWapp(t, packPath, "ui", "app", map[string]string{"v.txt": "1"})
	require.NoError(t, reg.RegisterPack(packPath, "org/mod", "1.0.0",
		createHubResourceReader(t, "ui", "app", map[string]string{"v.txt": "1"}), nil))

	handler, err := NewDependencyHandler(DependencyHandlerOptions{
		Hub: &fakeHub{
			getDownload: func(context.Context, *DownloadParams) (*DownloadInfo, error) {
				t.Fatal("unchanged registered module should not download")
				return nil, nil
			},
		},
		Logger:    zap.NewNop(),
		VendorDir: vendorDir,
	})
	require.NoError(t, err)

	resolved := []ResolvedModule{{Org: "org", Name: "mod", Version: "1.0.0"}}
	snapshot := regapi.State{moduleEntry("ui", "app", "org/mod", "1.0.0")}

	eff, err := handler.buildEmbedPackEffect(ctx, resolved, snapshot, map[string]struct{}{"org/mod": {}})
	require.NoError(t, err)
	assert.Nil(t, eff)
}

func TestBuildEmbedPackEffect_RejectsSameVersionDifferentDigest(t *testing.T) {
	reg := embedpkg.NewRegistry()
	defer func() { require.NoError(t, reg.Close()) }()
	ctx := embedapi.WithRegistry(newTestContext(), reg)
	vendorDir := t.TempDir()
	packPath := filepath.Join(vendorDir, "org", "mod-1.0.0.wapp")
	writeResourceWapp(t, packPath, "ui", "app", map[string]string{"v.txt": "old"})
	require.NoError(t, reg.RegisterPack(packPath, "org/mod", "1.0.0",
		createHubResourceReader(t, "ui", "app", map[string]string{"v.txt": "old"}), nil))

	handler, err := NewDependencyHandler(DependencyHandlerOptions{
		Hub: &fakeHub{
			getDownload: func(context.Context, *DownloadParams) (*DownloadInfo, error) {
				t.Fatal("unsafe same-version replacement must fail before materialization")
				return nil, nil
			},
		},
		Logger:    zap.NewNop(),
		VendorDir: vendorDir,
	})
	require.NoError(t, err)

	const oldDigest = "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	const newDigest = "sha256:2222222222222222222222222222222222222222222222222222222222222222"
	snapshotEntry := markModuleIdentity(moduleEntry("ui", "app", "org/mod", "1.0.0"), "org/mod", "1.0.0", oldDigest)
	resolved := []ResolvedModule{{
		Org: "org", Name: "mod", Version: "1.0.0", Source: moduleSourceHub, Digest: newDigest,
	}}

	eff, err := handler.buildEmbedPackEffect(ctx, resolved, regapi.State{snapshotEntry}, map[string]struct{}{"org/mod": {}})
	require.Error(t, err)
	assert.Nil(t, eff)
	assert.Equal(t, "old", readHubResource(t, reg, moduleEntry("ui", "app", "org/mod", "1.0.0"), "v.txt"))
}

func TestBuildEmbedPackEffectAcceptsEquivalentDigestEncoding(t *testing.T) {
	reg := embedpkg.NewRegistry()
	defer func() { require.NoError(t, reg.Close()) }()
	ctx := embedapi.WithRegistry(newTestContext(), reg)
	require.NoError(t, reg.RegisterPack("org/mod-1.0.0.wapp", "org/mod", "1.0.0",
		createHubResourceReader(t, "ui", "app", map[string]string{"v.txt": "old"}), nil))
	handler := &DependencyHandler{logger: zap.NewNop()}
	digestValue := strings.Repeat("1", 64)
	snapshotEntry := markModuleIdentity(
		moduleEntry("ui", "app", "org/mod", "1.0.0"),
		"org/mod",
		"1.0.0",
		digestValue,
	)

	effect, err := handler.buildEmbedPackEffect(ctx, []ResolvedModule{{
		Org: "org", Name: "mod", Version: "1.0.0", Digest: "sha256:" + digestValue,
	}}, regapi.State{snapshotEntry}, map[string]struct{}{"org/mod": {}})
	require.NoError(t, err)
	assert.Nil(t, effect)
}

func TestBuildEmbedPackEffect_StagesUnchangedPackWhenRegistryMissing(t *testing.T) {
	reg := embedpkg.NewRegistry()
	defer func() { require.NoError(t, reg.Close()) }()
	ctx := embedapi.WithRegistry(newTestContext(), reg)
	vendorDir := t.TempDir()
	packPath := filepath.Join(vendorDir, "org", "mod-1.0.0.wapp")
	writeResourceWapp(t, packPath, "ui", "app", map[string]string{"v.txt": "1"})
	digest, _, err := artifactIdentityFromPath(packPath)
	require.NoError(t, err)
	immutableRelative, err := immutableWappRelativePath(graph.MustParseName("org/mod"), "1.0.0", digest)
	require.NoError(t, err)
	immutablePackPath := filepath.Join(vendorDir, immutableRelative)

	handler, err := NewDependencyHandler(DependencyHandlerOptions{
		Hub: &fakeHub{
			getDownload: func(context.Context, *DownloadParams) (*DownloadInfo, error) {
				t.Fatal("test pack already exists in the vendor cache")
				return nil, nil
			},
		},
		Logger:    zap.NewNop(),
		VendorDir: vendorDir,
	})
	require.NoError(t, err)

	resolved := []ResolvedModule{{Org: "org", Name: "mod", Version: "1.0.0"}}
	snapshot := regapi.State{moduleEntry("ui", "app", "org/mod", "1.0.0")}

	eff, err := handler.buildEmbedPackEffect(ctx, resolved, snapshot, map[string]struct{}{"org/mod": {}})
	require.NoError(t, err)
	require.NotNil(t, eff)
	assert.Equal(t, []stagedPack{{packPath: immutablePackPath, module: "org/mod", version: "1.0.0"}}, eff.staged)
	assert.Empty(t, eff.obsolete)
}

func TestBuildEmbedPackEffect_DropsPackWhenResolvedModuleIsDirectory(t *testing.T) {
	reg := embedpkg.NewRegistry()
	defer func() { require.NoError(t, reg.Close()) }()
	ctx := embedapi.WithRegistry(newTestContext(), reg)
	projectDir := t.TempDir()
	vendorDir := filepath.Join(projectDir, ".wippy", "vendor")
	lockPath := filepath.Join(projectDir, lock.DefaultFilename)
	replacementDir := filepath.Join(projectDir, "local", "mod")
	require.NoError(t, os.MkdirAll(replacementDir, 0755))

	lockObj, err := lock.New(lockPath)
	require.NoError(t, err)
	lockObj.SetDirectories(lock.Directories{Modules: ".wippy", Src: "."})
	lockObj.SetModule(lock.Module{Name: "org/mod", Version: "1.0.0"})
	lockObj.SetReplacement(lock.Replacement{From: "org/mod", To: "local/mod"})
	require.NoError(t, lockObj.Write())

	oldReader := createHubResourceReader(t, "ui", "app", map[string]string{"v.txt": "old"})
	require.NoError(t, reg.RegisterPack("org/mod-v1.0.0.wapp", "org/mod", "1.0.0", oldReader, nil))

	handler, err := NewDependencyHandler(DependencyHandlerOptions{
		Hub:       &fakeHub{},
		Logger:    zap.NewNop(),
		LockPath:  lockPath,
		VendorDir: vendorDir,
	})
	require.NoError(t, err)

	resolved := []ResolvedModule{{Org: "org", Name: "mod", Version: "1.0.0"}}
	snapshot := regapi.State{moduleEntry("ui", "app", "org/mod", "1.0.0")}

	eff, err := handler.buildEmbedPackEffect(ctx, resolved, snapshot, map[string]struct{}{"org/mod": {}})
	require.NoError(t, err)
	require.NotNil(t, eff)
	assert.Empty(t, eff.staged)
	assert.Equal(t, []obsoletePack{{module: "org/mod", version: "1.0.0"}}, eff.obsolete)

	require.NoError(t, eff.Commit(context.Background()))
	require.NoError(t, eff.Finalize(context.Background()))
	_, err = reg.GetFSForEntry(moduleEntry("ui", "app", "org/mod", "1.0.0"))
	require.Error(t, err)
}

func TestBuildEmbedPackEffect_DropsPackWhenUnpackModulesEnabled(t *testing.T) {
	reg := embedpkg.NewRegistry()
	defer func() { require.NoError(t, reg.Close()) }()
	ctx := embedapi.WithRegistry(newTestContext(), reg)
	projectDir := t.TempDir()
	vendorDir := filepath.Join(projectDir, ".wippy", "vendor")
	lockPath := filepath.Join(projectDir, lock.DefaultFilename)
	dirPath := filepath.Join(vendorDir, "org", "mod")
	require.NoError(t, os.MkdirAll(dirPath, 0755))
	digest := "sha256:0000000000000000000000000000000000000000000000000000000000000000"
	require.NoError(t, writeExtractedModuleMeta(dirPath, digest, 0))

	lockObj, err := lock.New(lockPath)
	require.NoError(t, err)
	lockObj.SetDirectories(lock.Directories{Modules: ".wippy", Src: "."})
	lockObj.SetOptions(lock.Options{UnpackModules: true})
	lockObj.SetModule(lock.Module{Name: "org/mod", Version: "1.0.0"})
	require.NoError(t, lockObj.Write())

	oldReader := createHubResourceReader(t, "ui", "app", map[string]string{"v.txt": "old"})
	require.NoError(t, reg.RegisterPack("org/mod-v1.0.0.wapp", "org/mod", "1.0.0", oldReader, nil))

	handler, err := NewDependencyHandler(DependencyHandlerOptions{
		Hub: &fakeHub{
			getDownload: func(context.Context, *DownloadParams) (*DownloadInfo, error) {
				t.Fatal("same-version unpacked module should use existing directory without downloading")
				return nil, nil
			},
		},
		Logger:    zap.NewNop(),
		LockPath:  lockPath,
		VendorDir: vendorDir,
	})
	require.NoError(t, err)

	resolved := []ResolvedModule{{Org: "org", Name: "mod", Version: "1.0.0", Source: moduleSourceHub, Digest: digest}}
	snapshot := regapi.State{moduleEntry("ui", "app", "org/mod", "1.0.0")}

	eff, err := handler.buildEmbedPackEffect(ctx, resolved, snapshot, map[string]struct{}{"org/mod": {}})
	require.NoError(t, err)
	require.NotNil(t, eff)
	assert.Empty(t, eff.staged)
	assert.Equal(t, []obsoletePack{{module: "org/mod", version: "1.0.0"}}, eff.obsolete)

	require.NoError(t, eff.Commit(context.Background()))
	require.NoError(t, eff.Finalize(context.Background()))
	_, err = reg.GetFSForEntry(moduleEntry("ui", "app", "org/mod", "1.0.0"))
	require.Error(t, err)
}

func TestBuildEmbedPackEffect_StagesOnlyChangedPacks(t *testing.T) {
	reg := embedpkg.NewRegistry()
	defer func() { require.NoError(t, reg.Close()) }()
	ctx := embedapi.WithRegistry(newTestContext(), reg)
	vendorDir := t.TempDir()

	oldReader := createHubResourceReader(t, "ui", "app", map[string]string{"v.txt": "1"})
	stableReader := createHubResourceReader(t, "ui", "stable", map[string]string{"v.txt": "stable"})
	removedReader := createHubResourceReader(t, "ui", "removed", map[string]string{"v.txt": "removed"})
	require.NoError(t, reg.RegisterPack("org/mod-v1.0.0.wapp", "org/mod", "1.0.0", oldReader, nil))
	require.NoError(t, reg.RegisterPack("org/stable-v1.0.0.wapp", "org/stable", "1.0.0", stableReader, nil))
	require.NoError(t, reg.RegisterPack("org/removed-v3.0.0.wapp", "org/removed", "3.0.0", removedReader, nil))

	newPack := filepath.Join(vendorDir, "org", "mod-2.0.0.wapp")
	writeResourceWapp(t, newPack, "ui", "app", map[string]string{"v.txt": "2"})
	newDigest, _, err := artifactIdentityFromPath(newPack)
	require.NoError(t, err)
	newImmutableRelative, err := immutableWappRelativePath(graph.MustParseName("org/mod"), "2.0.0", newDigest)
	require.NoError(t, err)
	newImmutablePack := filepath.Join(vendorDir, newImmutableRelative)

	handler, err := NewDependencyHandler(DependencyHandlerOptions{
		Hub: &fakeHub{
			getDownload: func(context.Context, *DownloadParams) (*DownloadInfo, error) {
				t.Fatal("test pack already exists in the vendor cache")
				return nil, nil
			},
		},
		Logger:    zap.NewNop(),
		VendorDir: vendorDir,
	})
	require.NoError(t, err)

	resolved := []ResolvedModule{
		{Org: "org", Name: "mod", Version: "2.0.0"},
		{Org: "org", Name: "stable", Version: "1.0.0"},
	}
	snapshot := regapi.State{
		moduleEntry("ui", "app", "org/mod", "1.0.0"),
		moduleEntry("ui", "stable", "org/stable", "1.0.0"),
		moduleEntry("ui", "removed", "org/removed", "3.0.0"),
	}

	eff, err := handler.buildEmbedPackEffect(ctx, resolved, snapshot, map[string]struct{}{
		"org/mod": {}, "org/stable": {}, "org/removed": {},
	})
	require.NoError(t, err)
	require.NotNil(t, eff)
	assert.Equal(t, []stagedPack{{packPath: newImmutablePack, module: "org/mod", version: "2.0.0"}}, eff.staged)
	assert.ElementsMatch(t, []obsoletePack{
		{module: "org/mod", version: "1.0.0"},
		{module: "org/removed", version: "3.0.0"},
	}, eff.obsolete)

	require.NoError(t, eff.Prepare(context.Background()))
	assert.Equal(t, "1", readHubResource(t, reg, moduleEntry("ui", "app", "org/mod", "1.0.0"), "v.txt"))
	assert.Equal(t, "2", readHubResource(t, reg, moduleEntry("ui", "app", "org/mod", "2.0.0"), "v.txt"))
	assert.Equal(t, "stable", readHubResource(t, reg, moduleEntry("ui", "stable", "org/stable", "1.0.0"), "v.txt"))

	require.NoError(t, eff.Commit(context.Background()))
	// Commit is still reversible; obsolete packs remain until history/head is durable.
	assert.Equal(t, "1", readHubResource(t, reg, moduleEntry("ui", "app", "org/mod", "1.0.0"), "v.txt"))
	require.NoError(t, eff.Finalize(context.Background()))
	assert.Equal(t, "2", readHubResource(t, reg, moduleEntry("ui", "app", "org/mod", "2.0.0"), "v.txt"))
	assert.Equal(t, "stable", readHubResource(t, reg, moduleEntry("ui", "stable", "org/stable", "1.0.0"), "v.txt"))
	_, err = reg.GetFS(regapi.NewID("ui", "removed"))
	require.Error(t, err)
}

func TestBuildEmbedPackEffect_InstallPreservesUnrelatedApplicationPack(t *testing.T) {
	reg := embedpkg.NewRegistry()
	defer func() { require.NoError(t, reg.Close()) }()
	ctx := embedapi.WithRegistry(newTestContext(), reg)
	vendorDir := t.TempDir()

	appPack := filepath.Join(t.TempDir(), "kickside-0.1.63.wapp")
	writeResourceWapp(t, appPack, "app", "app_fs", map[string]string{"index.js": "app"})
	appFile, err := os.Open(appPack)
	require.NoError(t, err)
	appReader, err := wapp.NewReader(appFile)
	require.NoError(t, err)
	require.NoError(t, reg.RegisterPack(appPack, "kickside/kickside", "0.1.63", appReader, appFile))

	crmPack := filepath.Join(vendorDir, "spiralscout", "crm-0.1.18.wapp")
	writeResourceWapp(t, crmPack, "crm", "ui_fs", map[string]string{"index.js": "crm"})

	handler, err := NewDependencyHandler(DependencyHandlerOptions{
		Hub: &fakeHub{
			getDownload: func(context.Context, *DownloadParams) (*DownloadInfo, error) {
				t.Fatal("test pack already exists in the vendor cache")
				return nil, nil
			},
		},
		Logger:    zap.NewNop(),
		VendorDir: vendorDir,
	})
	require.NoError(t, err)

	resolved := []ResolvedModule{{Org: "spiralscout", Name: "crm", Version: "0.1.18"}}
	snapshot := regapi.State{moduleEntry("app", "app_fs", "kickside/kickside", "0.1.63")}
	controlled := map[string]struct{}{"spiralscout/crm": {}}

	eff, err := handler.buildEmbedPackEffect(ctx, resolved, snapshot, controlled)
	require.NoError(t, err)
	require.NotNil(t, eff)
	assert.Empty(t, eff.obsolete)

	require.NoError(t, eff.Prepare(context.Background()))
	require.NoError(t, eff.Commit(context.Background()))
	assert.Equal(t, "app", readHubResource(t, reg,
		moduleEntry("app", "app_fs", "kickside/kickside", "0.1.63"), "index.js"))
	assert.Equal(t, "crm", readHubResource(t, reg,
		moduleEntry("crm", "ui_fs", "spiralscout/crm", "0.1.18"), "index.js"))
}

func TestEmbedPackEffect_UpdateRollbackKeepsLiveOldVersion(t *testing.T) {
	reg := embedpkg.NewRegistry()
	oldReader := createHubResourceReader(t, "ui", "app", map[string]string{"v.txt": "1"})
	require.NoError(t, reg.RegisterPack("org/mod-v1.0.0.wapp", "org/mod", "1.0.0", oldReader, nil))

	dir := t.TempDir()
	newPack := filepath.Join(dir, "org", "mod-v2.0.0.wapp")
	writeResourceWapp(t, newPack, "ui", "app", map[string]string{"v.txt": "2"})

	eff := newEffect(reg,
		[]stagedPack{{packPath: newPack, module: "org/mod", version: "2.0.0"}},
		[]obsoletePack{{module: "org/mod", version: "1.0.0"}},
	)

	require.NoError(t, eff.Prepare(context.Background()))
	assert.Equal(t, "2", readHubResource(t, reg, moduleEntry("ui", "app", "org/mod", "2.0.0"), "v.txt"))

	require.NoError(t, eff.Rollback(context.Background()))
	assert.Equal(t, "1", readHubResource(t, reg, moduleEntry("ui", "app", "org/mod", "1.0.0"), "v.txt"))
}

func TestMaterializeModuleForLoad_StagesCachedWappUntilPrepare(t *testing.T) {
	projectDir := t.TempDir()
	vendorDir := filepath.Join(projectDir, ".wippy", "vendor")
	lockPath := filepath.Join(projectDir, lock.DefaultFilename)
	packPath := filepath.Join(vendorDir, "org", "mod-1.0.0.wapp")
	dirPath := filepath.Join(vendorDir, "org", "mod")
	writeEmbeddedFSWapp(t, packPath, "ui", "app", map[string]string{"asset.txt": "new"})
	require.NoError(t, os.MkdirAll(dirPath, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dirPath, "stale.txt"), []byte("old"), 0644))

	lockObj, err := lock.New(lockPath)
	require.NoError(t, err)
	lockObj.SetDirectories(lock.Directories{Modules: ".wippy", Src: "."})
	lockObj.SetOptions(lock.Options{UnpackModules: true})
	lockObj.SetModule(lock.Module{Name: "org/mod", Version: "1.0.0"})
	require.NoError(t, lockObj.Write())

	handler, err := NewDependencyHandler(DependencyHandlerOptions{
		Hub: &fakeHub{
			getDownload: func(context.Context, *DownloadParams) (*DownloadInfo, error) {
				t.Fatal("cached pack should be extracted without downloading")
				return nil, nil
			},
		},
		Logger:    zap.NewNop(),
		LockPath:  lockPath,
		VendorDir: vendorDir,
	})
	require.NoError(t, err)
	digest, size, err := artifactIdentityFromPath(packPath)
	require.NoError(t, err)

	gotPath, staged, err := handler.materializeModuleForLoad(
		context.Background(),
		ResolvedModule{Org: "org", Name: "mod", Version: "1.0.0", Digest: digest, SizeBytes: size},
	)
	require.NoError(t, err)
	require.NotNil(t, staged)
	assert.Equal(t, staged.stagingDir, gotPath)
	assert.FileExists(t, packPath)
	assert.FileExists(t, filepath.Join(dirPath, "stale.txt"), "planning must leave the active tree untouched")
	assert.FileExists(t, filepath.Join(gotPath, "app", "asset.txt"))

	effect := &moduleFilesystemEffect{staged: []stagedModuleDirectory{*staged}}
	require.NoError(t, effect.Prepare(context.Background()))
	assert.NoFileExists(t, filepath.Join(dirPath, "stale.txt"))
	assert.FileExists(t, filepath.Join(dirPath, "app", "asset.txt"))
	require.NoError(t, effect.Commit(context.Background()))
	require.NoError(t, effect.Finalize(context.Background()))

	indexData, err := os.ReadFile(filepath.Join(dirPath, "_index.yaml"))
	require.NoError(t, err)
	assert.True(t, strings.Contains(string(indexData), "kind: fs.directory"), string(indexData))
}

func TestMaterializeModuleForLoad_DownloadsPrivatelyUntilPrepare(t *testing.T) {
	projectDir := t.TempDir()
	vendorDir := filepath.Join(projectDir, ".wippy", "vendor")
	lockPath := filepath.Join(projectDir, lock.DefaultFilename)
	dirPath := filepath.Join(vendorDir, "org", "mod")
	require.NoError(t, os.MkdirAll(dirPath, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dirPath, "stale.txt"), []byte("old"), 0644))

	lockObj, err := lock.New(lockPath)
	require.NoError(t, err)
	lockObj.SetDirectories(lock.Directories{Modules: ".wippy", Src: "."})
	lockObj.SetOptions(lock.Options{UnpackModules: true})
	lockObj.SetModule(lock.Module{Name: "org/mod", Version: "1.0.0"})
	require.NoError(t, lockObj.Write())

	sourcePack := filepath.Join(t.TempDir(), "mod-2.0.0.wapp")
	writeEmbeddedFSWapp(t, sourcePack, "ui", "app", map[string]string{"asset.txt": "new"})
	downloadBytes, err := os.ReadFile(sourcePack)
	require.NoError(t, err)
	digest, size, err := artifactIdentityFromPath(sourcePack)
	require.NoError(t, err)
	downloaded := false
	handler, err := NewDependencyHandler(DependencyHandlerOptions{
		Hub: &fakeHub{
			getDownload: func(context.Context, *DownloadParams) (*DownloadInfo, error) {
				return &DownloadInfo{URL: "memory://org/mod-2.0.0.wapp"}, nil
			},
			downloadFile: func(_ context.Context, _ string, destPath string) error {
				downloaded = true
				if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
					return err
				}
				return os.WriteFile(destPath, downloadBytes, 0600)
			},
		},
		Logger:    zap.NewNop(),
		LockPath:  lockPath,
		VendorDir: vendorDir,
	})
	require.NoError(t, err)

	gotPath, staged, err := handler.materializeModuleForLoad(
		context.Background(),
		ResolvedModule{Org: "org", Name: "mod", Version: "2.0.0", Digest: digest, SizeBytes: size},
	)
	require.NoError(t, err)
	assert.True(t, downloaded)
	require.NotNil(t, staged)
	assert.Equal(t, staged.stagingDir, gotPath)
	assert.FileExists(t, filepath.Join(dirPath, "stale.txt"), "planning must leave the active tree untouched")
	assert.FileExists(t, filepath.Join(gotPath, "app", "asset.txt"))
	assert.NoFileExists(t, filepath.Join(vendorDir, "org", "mod-2.0.0.wapp"))

	effect := &moduleFilesystemEffect{staged: []stagedModuleDirectory{*staged}}
	require.NoError(t, effect.Prepare(context.Background()))
	assert.NoFileExists(t, filepath.Join(dirPath, "stale.txt"))
	assert.FileExists(t, filepath.Join(dirPath, "app", "asset.txt"))
	require.NoError(t, effect.Rollback(context.Background()))
	assert.FileExists(t, filepath.Join(dirPath, "stale.txt"), "history failure must restore the old tree")
}

func TestSourceEffectUnpackedModulePrepareAndRollback(t *testing.T) {
	projectDir := t.TempDir()
	vendorDir := filepath.Join(projectDir, ".wippy", "vendor")
	lockPath := filepath.Join(projectDir, lock.DefaultFilename)
	packPath := filepath.Join(vendorDir, "org", "mod-1.0.0.wapp")
	dirPath := filepath.Join(vendorDir, "org", "mod")
	writeEmbeddedFSWapp(t, packPath, "ui", "app", map[string]string{"asset.txt": "new"})
	packDigest, packSize, err := artifactIdentityFromPath(packPath)
	require.NoError(t, err)

	lockObj, err := lock.New(lockPath)
	require.NoError(t, err)
	lockObj.SetDirectories(lock.Directories{Modules: ".wippy", Src: "."})
	lockObj.SetOptions(lock.Options{UnpackModules: true})
	lockObj.SetModule(lock.Module{Name: "org/mod", Version: "1.0.0", Root: true})
	require.NoError(t, lockObj.Write())

	handler, err := NewDependencyHandler(DependencyHandlerOptions{
		Hub: &fakeHub{
			getDownload: func(context.Context, *DownloadParams) (*DownloadInfo, error) {
				t.Fatal("cached pack should be extracted without downloading")
				return nil, nil
			},
		},
		Logger:    zap.NewNop(),
		LockPath:  lockPath,
		VendorDir: vendorDir,
	})
	require.NoError(t, err)

	ctx := newTestContext()
	sourceRegistry := moduleapi.NewSourceRegistry()
	ctx = moduleapi.WithSourceRegistry(ctx, sourceRegistry)
	transcoder := payload.GetTranscoder(ctx)
	register, ok := transcoder.(payload.TranscoderRegister)
	require.True(t, ok)
	yamlpayload.Register(register)
	ac := ctxapi.AppFromContext(ctx)
	require.NotNil(t, ac)
	ac.Seal()
	sourceRegistry.Set(moduleapi.Sources{
		"org/removed":   {LoadPath: "/old/removed", ResourceRoot: "/old/removed", Owner: "org/removed", Sequence: 1},
		"org/unrelated": {LoadPath: "/old/unrelated", ResourceRoot: "/old/unrelated", Owner: "org/unrelated", Sequence: 2},
	})
	var loadedSources moduleapi.Sources
	sourceRegistry.SetLoader(func(_ context.Context, sources moduleapi.Sources) ([]regapi.Entry, error) {
		loadedSources = sources
		return nil, nil
	})

	resolved := ResolvedModule{Org: "org", Name: "mod", Version: "1.0.0", Digest: packDigest, SizeBytes: packSize}
	entries, err := handler.loadEntriesForModule(
		ctx,
		transcoder,
		resolved,
	)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, regapi.NewID("ui", "app"), entries[0].ID)
	assert.Equal(t, regapi.Kind("fs.directory"), entries[0].Kind)
	_, ok = sourceRegistry.ResourceRoot("org/mod")
	require.False(t, ok, "planning must not publish a source root before effect preparation")

	effect, err := handler.buildSourceEffect(
		[]ResolvedModule{resolved},
		map[string]struct{}{"org/mod": {}, "org/removed": {}},
	)
	require.NoError(t, err)
	require.NotNil(t, effect)
	require.NoError(t, effect.Prepare(ctx))

	root, ok := sourceRegistry.ResourceRoot("org/mod")
	require.True(t, ok)
	assert.Equal(t, dirPath, root)
	_, err = sourceRegistry.Load(ctx)
	require.NoError(t, err)
	assert.Equal(t, moduleapi.Source{
		LoadPath:       dirPath,
		ResourceRoot:   dirPath,
		Owner:          "org/mod",
		Version:        "1.0.0",
		Digest:         packDigest,
		Sequence:       3,
		DeploymentRoot: true,
	}, loadedSources["org/mod"])
	_, ok = sourceRegistry.ResourceRoot("org/removed")
	require.False(t, ok)
	root, ok = sourceRegistry.ResourceRoot("org/unrelated")
	require.True(t, ok)
	assert.Equal(t, "/old/unrelated", root)

	require.NoError(t, effect.Commit(ctx))
	require.NoError(t, effect.Rollback(ctx))
	_, ok = sourceRegistry.ResourceRoot("org/mod")
	require.False(t, ok)
	root, ok = sourceRegistry.ResourceRoot("org/removed")
	require.True(t, ok)
	assert.Equal(t, "/old/removed", root)
	root, ok = sourceRegistry.ResourceRoot("org/unrelated")
	require.True(t, ok)
	assert.Equal(t, "/old/unrelated", root)
	_, err = sourceRegistry.Load(ctx)
	require.NoError(t, err)
	assert.NotContains(t, loadedSources, "org/mod")
	assert.Equal(t, moduleapi.Source{LoadPath: "/old/removed", ResourceRoot: "/old/removed", Owner: "org/removed", Sequence: 1}, loadedSources["org/removed"])

	// Lifecycle cleanup is idempotent; a duplicate rollback must not remove the
	// restored roots.
	require.NoError(t, effect.Rollback(ctx))
	root, ok = sourceRegistry.ResourceRoot("org/removed")
	require.True(t, ok)
	assert.Equal(t, "/old/removed", root)
}

func TestSourceEffectPackedModuleTracksLoadIdentityWithoutExposingRoot(t *testing.T) {
	vendorDir := t.TempDir()
	handler := &DependencyHandler{vendorDir: vendorDir}
	ctx := newTestContext()
	sourceRegistry := moduleapi.NewSourceRegistry()
	ctx = moduleapi.WithSourceRegistry(ctx, sourceRegistry)
	var loadedSources moduleapi.Sources
	sourceRegistry.SetLoader(func(_ context.Context, sources moduleapi.Sources) ([]regapi.Entry, error) {
		loadedSources = sources
		return nil, nil
	})

	packPath := filepath.Join(vendorDir, "org", "mod-1.0.0.wapp")
	writeEmbeddedFSWapp(t, packPath, "ui", "app", map[string]string{"asset.txt": "packed"})
	packDigest, packSize, err := artifactIdentityFromPath(packPath)
	require.NoError(t, err)
	immutablePath, err := handler.immutableArtifactPath(graph.Name{Organization: "org", Module: "mod"}, "1.0.0", packDigest)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(immutablePath), 0o755))
	require.NoError(t, os.Rename(packPath, immutablePath))
	resolved := ResolvedModule{Org: "org", Name: "mod", Version: "1.0.0", Digest: packDigest, SizeBytes: packSize}
	effect, err := handler.buildSourceEffect([]ResolvedModule{resolved}, map[string]struct{}{"org/mod": {}})
	require.NoError(t, err)
	require.NotNil(t, effect)
	require.NoError(t, effect.Prepare(ctx))

	_, ok := sourceRegistry.ResourceRoot("org/mod")
	assert.False(t, ok)
	_, err = sourceRegistry.Load(ctx)
	require.NoError(t, err)
	assert.Equal(t, moduleapi.Source{
		LoadPath: immutablePath,
		Version:  "1.0.0",
		Digest:   packDigest,
		Sequence: 1,
	}, loadedSources["org/mod"])

	require.NoError(t, effect.Rollback(ctx))
	_, err = sourceRegistry.Load(ctx)
	require.NoError(t, err)
	assert.NotContains(t, loadedSources, "org/mod")
}

func moduleEntry(ns, name, module, version string) regapi.Entry {
	meta := attrs.NewBag()
	meta.Set(metaModuleKey, module)
	if version != "" {
		meta.Set(metaModuleVersionKey, version)
	}
	return regapi.Entry{ID: regapi.NewID(ns, name), Meta: meta}
}

func createHubResourceReader(t *testing.T, ns, name string, files map[string]string) *wapp.Reader {
	t.Helper()

	path := filepath.Join(t.TempDir(), name+".wapp")
	writeResourceWapp(t, path, ns, name, files)
	file, err := os.Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = file.Close() })

	reader, err := wapp.NewReader(file)
	require.NoError(t, err)
	return reader
}

func writeEmbeddedFSWapp(t *testing.T, path, ns, name string, files map[string]string) {
	t.Helper()

	mapFS := fstest.MapFS{}
	for p, content := range files {
		mapFS[p] = &fstest.MapFile{Data: []byte(content), Mode: 0644}
	}

	var buf bytes.Buffer
	writer := wapp.NewWriter()
	require.NoError(t, writer.PackWithResources(
		wapp.Metadata{},
		[]wapp.Entry{{
			ID:   wapp.NewID(ns, name),
			Kind: embedapi.Kind,
			Meta: wapp.Metadata{},
			Data: map[string]any{},
		}},
		[]wapp.ResourceSpec{{ID: wapp.NewID(ns, name), FS: mapFS}},
		&buf,
	))

	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0755))
	require.NoError(t, os.WriteFile(path, buf.Bytes(), 0600))
}

func readHubResource(t *testing.T, reg *embedpkg.Registry, entry regapi.Entry, name string) string {
	t.Helper()

	fsys, err := reg.GetFSForEntry(entry)
	require.NoError(t, err)
	data, err := fs.ReadFile(fsys, name)
	require.NoError(t, err)
	return string(data)
}
