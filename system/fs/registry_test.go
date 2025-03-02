package fs

import (
	"context"
	"testing"
	"time"

	"github.com/ponyruntime/pony/api/event"
	fsapi "github.com/ponyruntime/pony/api/fs"
	"github.com/ponyruntime/pony/system/eventbus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"io/fs"
)

// mockFS implements FS interface
type mockFS struct{}

// Implement ReadFS methods
func (m *mockFS) Open(name string) (fs.File, error) {
	return &mockFile{}, nil
}

func (m *mockFS) Stat(name string) (fs.FileInfo, error) {
	return &mockFileInfo{}, nil
}

func (m *mockFS) ReadDir(name string) ([]fs.DirEntry, error) {
	return []fs.DirEntry{&mockDirEntry{}}, nil
}

// Implement WriteFS methods
func (m *mockFS) OpenFile(name string, flag int, perm fs.FileMode) (fsapi.File, error) {
	return &mockFile{}, nil
}

func (m *mockFS) Remove(name string) error {
	return nil
}

func (m *mockFS) Mkdir(name string, perm fs.FileMode) error {
	return nil
}

// mockFile implements File interface
type mockFile struct{}

func (m *mockFile) Stat() (fs.FileInfo, error) {
	return &mockFileInfo{}, nil
}

func (m *mockFile) Read(p []byte) (int, error) {
	return len(p), nil
}

func (m *mockFile) Close() error {
	return nil
}

func (m *mockFile) Write(p []byte) (int, error) {
	return len(p), nil
}

func (m *mockFile) Seek(offset int64, whence int) (int64, error) {
	return 0, nil
}

func (m *mockFile) Sync() error {
	return nil
}

// mockFileInfo implements fs.FileInfo
type mockFileInfo struct{}

func (m *mockFileInfo) Name() string {
	return "mockfile"
}

func (m *mockFileInfo) Size() int64 {
	return 0
}

func (m *mockFileInfo) Mode() fs.FileMode {
	return 0644
}

func (m *mockFileInfo) ModTime() time.Time {
	return time.Now()
}

func (m *mockFileInfo) IsDir() bool {
	return false
}

func (m *mockFileInfo) Sys() interface{} {
	return nil
}

// mockDirEntry implements fs.DirEntry
type mockDirEntry struct{}

func (m *mockDirEntry) Name() string {
	return "mockdir"
}

func (m *mockDirEntry) IsDir() bool {
	return true
}

func (m *mockDirEntry) Type() fs.FileMode {
	return fs.ModeDir
}

func (m *mockDirEntry) Info() (fs.FileInfo, error) {
	return &mockFileInfo{}, nil
}

func newTestFSRegistry(t *testing.T) (*Registry, event.Bus) {
	logger := zap.NewNop()
	bus := eventbus.NewBus()
	registry := NewFSRegistry(bus, logger)
	return registry, bus
}

func TestFSRegistry_StartStop(t *testing.T) {
	ctx := context.Background()
	registry, _ := newTestFSRegistry(t)

	err := registry.Start(ctx)
	require.NoError(t, err)
	assert.NotNil(t, registry.subscriber)

	err = registry.Stop()
	require.NoError(t, err)
}

func TestFSRegistry_RegisterFS(t *testing.T) {
	ctx := context.Background()
	fsRegistry, bus := newTestFSRegistry(t)
	require.NoError(t, fsRegistry.Start(ctx))
	defer func() { assert.NoError(t, fsRegistry.Stop()) }()

	responses := make(chan event.Event, 1)
	sub, err := eventbus.NewSubscriber(
		ctx,
		bus,
		fsapi.System,
		"fs.*",
		func(evt event.Event) {
			if evt.Kind == fsapi.Accept || evt.Kind == fsapi.Reject {
				responses <- evt
			}
		},
	)
	require.NoError(t, err)
	defer sub.Close()

	t.Run("register filesystem", func(t *testing.T) {
		f := &mockFS{}
		bus.Send(ctx, event.Event{
			System: fsapi.System,
			Kind:   fsapi.Register,
			Path:   "test:mock-fs",
			Data:   f,
		})

		select {
		case resp := <-responses:
			assert.Equal(t, fsapi.Accept, resp.Kind)
			assert.Equal(t, "test:mock-fs", resp.Path)
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for response")
		}

		// Verify filesystem was registered
		registeredFS, exists := fsRegistry.GetFS("test:mock-fs")
		assert.True(t, exists)
		assert.NotNil(t, registeredFS)
	})

	t.Run("register with invalid payload", func(t *testing.T) {
		bus.Send(ctx, event.Event{
			System: fsapi.System,
			Kind:   fsapi.Register,
			Path:   "test:invalid-payload",
			Data:   "invalid data",
		})

		select {
		case resp := <-responses:
			assert.Equal(t, fsapi.Reject, resp.Kind)
			assert.Equal(t, "test:invalid-payload", resp.Path)
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for response")
		}
	})
}

func TestFSRegistry_DeleteFS(t *testing.T) {
	ctx := context.Background()
	fsRegistry, bus := newTestFSRegistry(t)
	require.NoError(t, fsRegistry.Start(ctx))
	defer func() { assert.NoError(t, fsRegistry.Stop()) }()

	responses := make(chan event.Event, 1)
	sub, err := eventbus.NewSubscriber(
		ctx,
		bus,
		fsapi.System,
		"fs.*",
		func(evt event.Event) {
			if evt.Kind == fsapi.Accept || evt.Kind == fsapi.Reject {
				responses <- evt
			}
		},
	)
	require.NoError(t, err)
	defer sub.Close()

	// Register a test filesystem first
	fsID := "test:mock-fs"
	f := &mockFS{}
	bus.Send(ctx, event.Event{
		System: fsapi.System,
		Kind:   fsapi.Register,
		Path:   fsID,
		Data:   f,
	})

	select {
	case resp := <-responses:
		assert.Equal(t, fsapi.Accept, resp.Kind)
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for registration response")
	}

	t.Run("successful deletion", func(t *testing.T) {
		bus.Send(ctx, event.Event{
			System: fsapi.System,
			Kind:   fsapi.Delete,
			Path:   fsID,
		})

		select {
		case resp := <-responses:
			assert.Equal(t, fsapi.Accept, resp.Kind)
			assert.Equal(t, fsID, resp.Path)
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for response")
		}

		// Verify filesystem was deleted
		_, exists := fsRegistry.GetFS(fsID)
		assert.False(t, exists)
	})

	t.Run("delete non-existent filesystem", func(t *testing.T) {
		bus.Send(ctx, event.Event{
			System: fsapi.System,
			Kind:   fsapi.Delete,
			Path:   "test:nonexistent",
		})

		select {
		case resp := <-responses:
			assert.Equal(t, fsapi.Reject, resp.Kind)
			assert.Equal(t, "test:nonexistent", resp.Path)
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for response")
		}
	})
}

func TestFSRegistry_GetFS(t *testing.T) {
	ctx := context.Background()
	fsRegistry, bus := newTestFSRegistry(t)
	require.NoError(t, fsRegistry.Start(ctx))
	defer func() { assert.NoError(t, fsRegistry.Stop()) }()

	// Register test filesystems
	f := &mockFS{}
	bus.Send(ctx, event.Event{
		System: fsapi.System,
		Kind:   fsapi.Register,
		Path:   "test:mock-fs",
		Data:   f,
	})

	// Wait for registration to complete
	time.Sleep(100 * time.Millisecond)

	tests := []struct {
		name     string
		fsID     string
		expectFS bool
	}{
		{
			name:     "existing filesystem",
			fsID:     "test:mock-fs",
			expectFS: true,
		},
		{
			name:     "non-existent filesystem",
			fsID:     "test:nonexistent",
			expectFS: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, exists := fsRegistry.GetFS(tt.fsID)
			assert.Equal(t, tt.expectFS, exists)
			if tt.expectFS {
				assert.NotNil(t, f)
			} else {
				assert.Nil(t, f)
			}
		})
	}
}

func TestFSRegistry_WithContext(t *testing.T) {
	ctx := context.Background()
	fsRegistry, _ := newTestFSRegistry(t)

	// Test adding registry to context
	ctxWithReg := fsapi.WithFSRegistry(ctx, fsRegistry)

	// Test retrieving registry from context
	retrievedRegistry := fsapi.GetRegistry(ctxWithReg)
	assert.NotNil(t, retrievedRegistry)
	assert.Equal(t, fsRegistry, retrievedRegistry)

	// Test retrieving from context without registry
	emptyRegistry := fsapi.GetRegistry(ctx)
	assert.Nil(t, emptyRegistry)
}
