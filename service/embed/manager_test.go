package embed

import (
	"context"
	"errors"
	"io/fs"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	embedapi "github.com/wippyai/runtime/api/service/embed"
	"github.com/wippyai/runtime/system/eventbus"
	"go.uber.org/zap"
)

func TestManager_Add(t *testing.T) {
	ctx := context.Background()
	bus := eventbus.NewBus()
	embedReg := &mockEmbedRegistry{
		filesystems: map[string]fs.ReadDirFS{
			"test:fs": &mockReadDirFS{},
		},
	}
	dtt := &mockDTT{}

	manager := NewManager(bus, dtt, embedReg, zap.NewNop())

	config := &embedapi.Config{}
	entry := registry.Entry{
		ID:   registry.ID{NS: "test", Name: "fs"},
		Kind: embedapi.Kind,
		Data: payload.New(config),
	}

	err := manager.Add(ctx, entry)
	require.NoError(t, err)

	// Verify filesystem was stored
	val, ok := manager.filesystems.Load(entry.ID.String())
	assert.True(t, ok)
	assert.NotNil(t, val)
}

func TestManager_Add_DuplicateID(t *testing.T) {
	ctx := context.Background()
	bus := eventbus.NewBus()
	embedReg := &mockEmbedRegistry{
		filesystems: map[string]fs.ReadDirFS{
			"test:fs": &mockReadDirFS{},
		},
	}
	dtt := &mockDTT{}

	manager := NewManager(bus, dtt, embedReg, zap.NewNop())

	config := &embedapi.Config{}
	entry := registry.Entry{
		ID:   registry.ID{NS: "test", Name: "fs"},
		Kind: embedapi.Kind,
		Data: payload.New(config),
	}

	// Add first time
	err := manager.Add(ctx, entry)
	require.NoError(t, err)

	// Try to add again
	err = manager.Add(ctx, entry)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")
}

func TestManager_Add_InvalidKind(t *testing.T) {
	ctx := context.Background()
	bus := eventbus.NewBus()
	embedReg := NewRegistry()
	dtt := &mockDTT{}

	manager := NewManager(bus, dtt, embedReg, zap.NewNop())

	entry := registry.Entry{
		ID:   registry.ID{NS: "test", Name: "embed"},
		Kind: "invalid.kind",
		Data: payload.New(&embedapi.Config{}),
	}

	err := manager.Add(ctx, entry)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported entry kind")
}

func TestManager_Add_DecodeFailure(t *testing.T) {
	ctx := context.Background()
	bus := eventbus.NewBus()
	embedReg := NewRegistry()
	dtt := &mockDTT{unmarshalErr: assert.AnError}

	manager := NewManager(bus, dtt, embedReg, zap.NewNop())

	entry := registry.Entry{
		ID:   registry.ID{NS: "test", Name: "embed"},
		Kind: embedapi.Kind,
		Data: payload.New(&embedapi.Config{}),
	}

	err := manager.Add(ctx, entry)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to decode config")
}

func TestManager_Add_FSNotFound(t *testing.T) {
	ctx := context.Background()
	bus := eventbus.NewBus()
	embedReg := NewRegistry()
	dtt := &mockDTT{}

	manager := NewManager(bus, dtt, embedReg, zap.NewNop())

	config := &embedapi.Config{}
	entry := registry.Entry{
		ID:   registry.ID{NS: "test", Name: "notfound"},
		Kind: embedapi.Kind,
		Data: payload.New(config),
	}

	err := manager.Add(ctx, entry)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get embedded filesystem")
}

func TestManager_Update(t *testing.T) {
	ctx := context.Background()
	bus := eventbus.NewBus()
	embedReg := &mockEmbedRegistry{
		filesystems: map[string]fs.ReadDirFS{
			"test:fs": &mockReadDirFS{},
		},
	}
	dtt := &mockDTT{}

	manager := NewManager(bus, dtt, embedReg, zap.NewNop())

	config := &embedapi.Config{}
	entry := registry.Entry{
		ID:   registry.ID{NS: "test", Name: "fs"},
		Kind: embedapi.Kind,
		Data: payload.New(config),
	}

	// Add first
	err := manager.Add(ctx, entry)
	require.NoError(t, err)

	// Update
	err = manager.Update(ctx, entry)
	assert.NoError(t, err)

	// Verify still exists
	val, ok := manager.filesystems.Load(entry.ID.String())
	assert.True(t, ok)
	assert.NotNil(t, val)
}

func TestManager_Update_NotFound(t *testing.T) {
	ctx := context.Background()
	bus := eventbus.NewBus()
	embedReg := NewRegistry()
	dtt := &mockDTT{}

	manager := NewManager(bus, dtt, embedReg, zap.NewNop())

	entry := registry.Entry{
		ID:   registry.ID{NS: "test", Name: "notfound"},
		Kind: embedapi.Kind,
		Data: payload.New(&embedapi.Config{}),
	}

	err := manager.Update(ctx, entry)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestManager_Delete(t *testing.T) {
	ctx := context.Background()
	bus := eventbus.NewBus()
	embedReg := &mockEmbedRegistry{
		filesystems: map[string]fs.ReadDirFS{
			"test:fs": &mockReadDirFS{},
		},
	}
	dtt := &mockDTT{}

	manager := NewManager(bus, dtt, embedReg, zap.NewNop())

	config := &embedapi.Config{}
	entry := registry.Entry{
		ID:   registry.ID{NS: "test", Name: "fs"},
		Kind: embedapi.Kind,
		Data: payload.New(config),
	}

	// Add first
	err := manager.Add(ctx, entry)
	require.NoError(t, err)

	// Delete
	err = manager.Delete(ctx, entry)
	assert.NoError(t, err)

	// Verify removed
	_, ok := manager.filesystems.Load(entry.ID.String())
	assert.False(t, ok)
}

func TestManager_Delete_NotFound(t *testing.T) {
	ctx := context.Background()
	bus := eventbus.NewBus()
	embedReg := NewRegistry()
	dtt := &mockDTT{}

	manager := NewManager(bus, dtt, embedReg, zap.NewNop())

	entry := registry.Entry{
		ID:   registry.ID{NS: "test", Name: "notfound"},
		Kind: embedapi.Kind,
		Data: payload.New(&embedapi.Config{}),
	}

	err := manager.Delete(ctx, entry)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// Mock implementations

type mockEmbedRegistry struct {
	filesystems map[string]fs.ReadDirFS
}

func (r *mockEmbedRegistry) GetFS(id registry.ID) (fs.ReadDirFS, error) {
	if fsys, ok := r.filesystems[id.String()]; ok {
		return fsys, nil
	}
	return nil, errors.New("filesystem not found")
}

func (r *mockEmbedRegistry) Close() error {
	return nil
}

func (r *mockEmbedRegistry) Register(_ string, _ interface{}) error {
	return nil
}

type mockDTT struct {
	unmarshalErr error
}

func (m *mockDTT) Unmarshal(p payload.Payload, v interface{}) error {
	if m.unmarshalErr != nil {
		return m.unmarshalErr
	}
	if config, ok := v.(*embedapi.Config); ok {
		if src, ok := p.Data().(*embedapi.Config); ok {
			*config = *src
			return nil
		}
	}
	return nil
}

func (m *mockDTT) Transcode(p payload.Payload, _ payload.Format) (payload.Payload, error) {
	return p, nil
}

type mockReadDirFS struct{}

func (m *mockReadDirFS) Open(_ string) (fs.File, error) {
	return nil, fs.ErrNotExist
}

func (m *mockReadDirFS) ReadDir(_ string) ([]fs.DirEntry, error) {
	return nil, nil
}
