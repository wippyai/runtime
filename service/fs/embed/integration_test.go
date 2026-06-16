// SPDX-License-Identifier: MPL-2.0

package embed

import (
	"context"
	"io"
	"io/fs"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	embedapi "github.com/wippyai/runtime/api/service/fs/embed"
	"github.com/wippyai/runtime/system/eventbus"
	"go.uber.org/zap"
)

// TestLiveInstall_PackRegisteredResolvesForEntry reproduces the live hub-install
// path: a module pack is registered into the concrete Registry, then the embed
// Manager adds the module's fs.embed entry. The entry must resolve to the
// registered pack's filesystem — before the fix this failed because live installs
// never registered the pack reader.
func TestLiveInstall_PackRegisteredResolvesForEntry(t *testing.T) {
	reg := NewRegistry()
	reader := createReaderWithResource(t, "ui", "app", map[string]string{"index.html": "<html>live</html>"})
	require.NoError(t, reg.RegisterPack("org/mod-v1.0.0.wapp", "org/mod", "1.0.0", reader, nil))

	manager := NewManager(eventbus.NewBus(), &mockDTT{}, reg, zap.NewNop())

	entry := registry.Entry{
		ID:   registry.NewID("ui", "app"),
		Kind: embedapi.Kind,
		Meta: moduleMeta("org/mod", "1.0.0"),
		Data: payload.New(&embedapi.Config{}),
	}

	require.NoError(t, manager.Add(context.Background(), entry))

	fsys, err := manager.resolveFS(entry)
	require.NoError(t, err)
	data, err := fs.ReadFile(fsys, "index.html")
	require.NoError(t, err)
	assert.Equal(t, "<html>live</html>", string(data))
}

// TestLiveUpdate_NewVersionResolvesWhileOldStaged proves version-aware resolution
// during an update: both pack versions are registered simultaneously (as they are
// between Prepare and Commit), and each entry resolves to its own version.
func TestLiveUpdate_NewVersionResolvesWhileOldStaged(t *testing.T) {
	reg := NewRegistry()
	oldReader := createReaderWithResource(t, "ui", "app", map[string]string{"v.txt": "1"})
	newReader := createReaderWithResource(t, "ui", "app", map[string]string{"v.txt": "2"})
	require.NoError(t, reg.RegisterPack("org/mod-v1.0.0.wapp", "org/mod", "1.0.0", oldReader, nil))
	require.NoError(t, reg.RegisterPack("org/mod-v2.0.0.wapp", "org/mod", "2.0.0", newReader, nil))

	manager := NewManager(eventbus.NewBus(), &mockDTT{}, reg, zap.NewNop())
	oldEntry := registry.Entry{
		ID:   registry.NewID("ui", "app"),
		Kind: embedapi.Kind,
		Meta: moduleMeta("org/mod", "1.0.0"),
		Data: payload.New(&embedapi.Config{}),
	}
	require.NoError(t, manager.Add(context.Background(), oldEntry))
	assert.Equal(t, "1", readManagerFile(t, manager, oldEntry.ID, "v.txt"))

	newEntry := registry.Entry{
		ID:   registry.NewID("ui", "app"),
		Kind: embedapi.Kind,
		Meta: moduleMeta("org/mod", "2.0.0"),
		Data: payload.New(&embedapi.Config{}),
	}

	require.NoError(t, manager.Update(context.Background(), newEntry))
	assert.Equal(t, "2", readManagerFile(t, manager, newEntry.ID, "v.txt"))

	fsys, err := manager.resolveFS(newEntry)
	require.NoError(t, err)
	data, err := fs.ReadFile(fsys, "v.txt")
	require.NoError(t, err)
	assert.Equal(t, "2", string(data))

	// After the old pack is dropped on commit, resolution still works.
	require.NoError(t, reg.UnregisterModule("org/mod", "1.0.0"))
	fsys, err = manager.resolveFS(newEntry)
	require.NoError(t, err)
	data, err = fs.ReadFile(fsys, "v.txt")
	require.NoError(t, err)
	assert.Equal(t, "2", string(data))
}

// TestLiveUpdate_MissingNewVersionDoesNotSwapToOld proves update is strict
// about module version metadata. If the new pack was not staged, updating the
// fs.embed entry must fail and leave the current live filesystem in place.
func TestLiveUpdate_MissingNewVersionDoesNotSwapToOld(t *testing.T) {
	reg := NewRegistry()
	oldReader := createReaderWithResource(t, "ui", "app", map[string]string{"v.txt": "1"})
	require.NoError(t, reg.RegisterPack("org/mod-v1.0.0.wapp", "org/mod", "1.0.0", oldReader, nil))

	manager := NewManager(eventbus.NewBus(), &mockDTT{}, reg, zap.NewNop())
	oldEntry := registry.Entry{
		ID:   registry.NewID("ui", "app"),
		Kind: embedapi.Kind,
		Meta: moduleMeta("org/mod", "1.0.0"),
		Data: payload.New(&embedapi.Config{}),
	}
	require.NoError(t, manager.Add(context.Background(), oldEntry))
	assert.Equal(t, "1", readManagerFile(t, manager, oldEntry.ID, "v.txt"))

	newEntry := oldEntry
	newEntry.Meta = moduleMeta("org/mod", "2.0.0")
	err := manager.Update(context.Background(), newEntry)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get embedded filesystem")
	assert.Equal(t, "1", readManagerFile(t, manager, oldEntry.ID, "v.txt"))

	_, err = manager.resolveFS(newEntry)
	require.Error(t, err)
}

// TestUninstall_PackUnregisteredNoLongerResolves confirms that once a module pack
// is unregistered, its fs.embed entry no longer resolves.
func TestUninstall_PackUnregisteredNoLongerResolves(t *testing.T) {
	reg := NewRegistry()
	reader := createReaderWithResource(t, "ui", "app", map[string]string{"index.html": "x"})
	require.NoError(t, reg.RegisterPack("org/mod-v1.0.0.wapp", "org/mod", "1.0.0", reader, nil))

	manager := NewManager(eventbus.NewBus(), &mockDTT{}, reg, zap.NewNop())
	entry := registry.Entry{
		ID:   registry.NewID("ui", "app"),
		Kind: embedapi.Kind,
		Meta: moduleMeta("org/mod", "1.0.0"),
		Data: payload.New(&embedapi.Config{}),
	}

	_, err := manager.resolveFS(entry)
	require.NoError(t, err)

	require.NoError(t, reg.UnregisterModule("org/mod", "1.0.0"))
	_, err = manager.resolveFS(entry)
	require.Error(t, err)
}

func moduleMeta(module, version string) attrs.Bag {
	meta := attrs.NewBag()
	meta.Set(metaModuleKey, module)
	meta.Set(metaModuleVersionKey, version)
	return meta
}

func readManagerFile(t *testing.T, manager *Manager, id registry.ID, name string) string {
	t.Helper()

	manager.mu.RLock()
	fsys := manager.filesystems[id]
	manager.mu.RUnlock()
	require.NotNil(t, fsys)

	file, err := fsys.Open(name)
	require.NoError(t, err)
	defer func() { _ = file.Close() }()

	data, err := io.ReadAll(file)
	require.NoError(t, err)
	return string(data)
}
