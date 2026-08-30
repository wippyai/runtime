// SPDX-License-Identifier: MPL-2.0

package embed

import (
	"context"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	embedapi "github.com/wippyai/runtime/api/service/fs/embed"
	"github.com/wippyai/runtime/system/eventbus"
	"go.uber.org/zap"
)

func TestLiveInstall_RegisteredPackServesResource(t *testing.T) {
	reg := NewRegistry()
	reader := createReaderWithResource(t, "ui", "app", map[string]string{"index.html": "<html>live</html>"})
	require.NoError(t, reg.RegisterPack("org/mod-v1.0.0.wapp", "org/mod", "1.0.0", reader, nil))

	manager := NewManager(eventbus.NewBus(), &mockDTT{}, reg, zap.NewNop())
	entry := registry.Entry{
		ID:   registry.NewID("ui", "app"),
		Kind: embedapi.Kind,
		Data: payload.New(&embedapi.Config{}),
	}
	require.NoError(t, manager.Add(context.Background(), entry))
	assert.Equal(t, "<html>live</html>", readManagerFile(t, manager, entry.ID, "index.html"))
}

func TestLiveInstall_StagedResourceBecomesReadableAtCommit(t *testing.T) {
	reg := NewRegistry()
	reader := createReaderWithResource(t, "ui", "app", map[string]string{"index.html": "<html>live</html>"})
	require.NoError(t, reg.StagePack("org/mod-v1.0.0.wapp", "org/mod", "1.0.0", reader, nil))

	manager := NewManager(eventbus.NewBus(), &mockDTT{}, reg, zap.NewNop())
	entry := registry.Entry{
		ID:   registry.NewID("ui", "app"),
		Kind: embedapi.Kind,
		Data: payload.New(&embedapi.Config{}),
	}
	// A transition listener can install and use the newly staged filesystem.
	require.NoError(t, manager.Add(context.Background(), entry))
	manager.mu.RLock()
	fsys := manager.filesystems[entry.ID]
	manager.mu.RUnlock()
	file, err := fsys.Open("index.html")
	require.NoError(t, err)
	_ = file.Close()

	require.NoError(t, reg.ActivatePack("org/mod-v1.0.0.wapp"))
	assert.Equal(t, "<html>live</html>", readManagerFile(t, manager, entry.ID, "index.html"))
}

func TestLiveUpdate_ActiveResourceMovesAndRollsBack(t *testing.T) {
	reg := NewRegistry()
	oldReader := createReaderWithResource(t, "ui", "app", map[string]string{"v.txt": "1"})
	newReader := createReaderWithResource(t, "ui", "app", map[string]string{"v.txt": "2"})
	require.NoError(t, reg.RegisterPack("org/mod-v1.0.0.wapp", "org/mod", "1.0.0", oldReader, nil))

	manager := NewManager(eventbus.NewBus(), &mockDTT{}, reg, zap.NewNop())
	entry := registry.Entry{
		ID:   registry.NewID("ui", "app"),
		Kind: embedapi.Kind,
		Data: payload.New(&embedapi.Config{}),
	}
	require.NoError(t, manager.Add(context.Background(), entry))
	assert.Equal(t, "1", readManagerFile(t, manager, entry.ID, "v.txt"))

	// Existing service handles remain on the committed pack through Prepare.
	require.NoError(t, reg.StagePack("org/mod-v2.0.0.wapp", "org/mod", "2.0.0", newReader, nil))
	assert.Equal(t, "1", readManagerFile(t, manager, entry.ID, "v.txt"))
	staged, err := reg.GetFS(entry.ID)
	require.NoError(t, err)
	stagedFile, err := staged.Open("v.txt")
	require.NoError(t, err)
	data, err := io.ReadAll(stagedFile)
	require.NoError(t, err)
	require.NoError(t, stagedFile.Close())
	assert.Equal(t, "2", string(data))

	// Commit switches the active resource. The existing service filesystem
	// follows it even when the entry bytes did not change.
	require.NoError(t, reg.ActivatePack("org/mod-v2.0.0.wapp"))
	assert.Equal(t, "2", readManagerFile(t, manager, entry.ID, "v.txt"))

	// A failed transition removes the staged pack and restores the prior pack.
	require.NoError(t, reg.UnregisterPack("org/mod-v2.0.0.wapp"))
	assert.Equal(t, "1", readManagerFile(t, manager, entry.ID, "v.txt"))

	// A successful retry moves the active resource again; finalizing the old
	// module leaves the active mapping untouched.
	require.NoError(t, reg.StagePack("org/mod-v2.0.0.wapp", "org/mod", "2.0.0", newReader, nil))
	require.NoError(t, reg.ActivatePack("org/mod-v2.0.0.wapp"))
	require.NoError(t, reg.UnregisterModule("org/mod", "1.0.0"))
	assert.Equal(t, "2", readManagerFile(t, manager, entry.ID, "v.txt"))
}

func TestUninstall_RemovesActiveResource(t *testing.T) {
	reg := NewRegistry()
	reader := createReaderWithResource(t, "ui", "app", map[string]string{"index.html": "x"})
	require.NoError(t, reg.RegisterPack("org/mod-v1.0.0.wapp", "org/mod", "1.0.0", reader, nil))

	manager := NewManager(eventbus.NewBus(), &mockDTT{}, reg, zap.NewNop())
	entry := registry.Entry{
		ID:   registry.NewID("ui", "app"),
		Kind: embedapi.Kind,
		Data: payload.New(&embedapi.Config{}),
	}
	require.NoError(t, manager.Add(context.Background(), entry))
	require.NoError(t, reg.UnregisterModule("org/mod", "1.0.0"))

	manager.mu.RLock()
	fsys := manager.filesystems[entry.ID]
	manager.mu.RUnlock()
	_, err := fsys.Open("index.html")
	require.Error(t, err)
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
