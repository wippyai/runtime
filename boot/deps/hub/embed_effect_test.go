// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/wapp"
	"go.uber.org/zap"
)

// stubPackRegistry records pack lifecycle calls for assertions.
type stubPackRegistry struct {
	registered     map[string]*wapp.Reader
	files          map[string]*os.File
	unregistered   []string
	modulesDropped []droppedModule

	registerErr error
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
	eff := newEffect(reg, []stagedPack{{packPath: packPath, module: "org/mod", version: "1.0.0"}}, nil)

	require.NoError(t, eff.Prepare(context.Background()))
	assert.Contains(t, reg.registered, packPath)
	require.Len(t, eff.prepared, 1)

	require.NoError(t, eff.Commit(context.Background()))
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
	eff := newEffect(reg,
		[]stagedPack{{packPath: newPack, module: "org/mod", version: "2.0.0"}},
		[]obsoletePack{{module: "org/mod", version: "1.0.0"}},
	)

	require.NoError(t, eff.Prepare(context.Background()))
	assert.Contains(t, reg.registered, newPack)

	require.NoError(t, eff.Commit(context.Background()))
	// Old version dropped on commit; new pack remains registered.
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
	require.NoError(t, eff.Rollback(context.Background()))

	// New pack removed; old version never dropped because Commit did not run.
	assert.Contains(t, reg.unregistered, newPack)
	assert.Empty(t, reg.modulesDropped)
}

func TestEmbedPackEffect_Uninstall(t *testing.T) {
	reg := newStubPackRegistry()
	eff := newEffect(reg, nil, []obsoletePack{{module: "org/mod", version: "1.0.0"}})

	require.NoError(t, eff.Prepare(context.Background()))
	assert.Empty(t, reg.registered)

	require.NoError(t, eff.Commit(context.Background()))
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

func TestObsoletePacksFor(t *testing.T) {
	snapshot := regapi.State{
		moduleEntry("ui", "old-app", "org/mod", "1.0.0"),
		moduleEntry("ui", "stable", "org/other", "3.0.0"),
		moduleEntry("ui", "noversion", "org/nover", ""),
	}

	t.Run("update marks changed version obsolete", func(t *testing.T) {
		desired := map[string]string{"org/mod": "2.0.0", "org/other": "3.0.0"}
		obs := obsoletePacksFor(snapshot, desired)
		require.Len(t, obs, 1)
		assert.Equal(t, obsoletePack{module: "org/mod", version: "1.0.0"}, obs[0])
	})

	t.Run("removal marks dropped module obsolete", func(t *testing.T) {
		desired := map[string]string{"org/other": "3.0.0"}
		obs := obsoletePacksFor(snapshot, desired)
		require.Len(t, obs, 1)
		assert.Equal(t, obsoletePack{module: "org/mod", version: "1.0.0"}, obs[0])
	})

	t.Run("unchanged versions are not obsolete", func(t *testing.T) {
		desired := map[string]string{"org/mod": "1.0.0", "org/other": "3.0.0"}
		obs := obsoletePacksFor(snapshot, desired)
		assert.Empty(t, obs)
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
	eff, err := handler.buildEmbedPackEffect(newTestContext(), nil, nil)
	require.NoError(t, err)
	assert.Nil(t, eff)
}

func moduleEntry(ns, name, module, version string) regapi.Entry {
	meta := attrs.NewBag()
	meta.Set(metaModuleKey, module)
	if version != "" {
		meta.Set(metaModuleVersionKey, version)
	}
	return regapi.Entry{ID: regapi.NewID(ns, name), Meta: meta}
}
