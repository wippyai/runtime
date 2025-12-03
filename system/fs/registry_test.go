package fs

import (
	"context"
	"fmt"
	"io/fs"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/event"
	fsapi "github.com/wippyai/runtime/api/fs"
	"github.com/wippyai/runtime/system/eventbus"
	"go.uber.org/zap"
)

// mockFS implements FS interface
type mockFS struct{}

// Implement ReadFS methods
func (m *mockFS) Open(_ string) (fs.File, error) {
	return &mockFile{}, nil
}

func (m *mockFS) Stat(_ string) (fs.FileInfo, error) {
	return &mockFileInfo{}, nil
}

func (m *mockFS) ReadDir(_ string) ([]fs.DirEntry, error) {
	return []fs.DirEntry{&mockDirEntry{}}, nil
}

// Implement WriteFS methods
func (m *mockFS) OpenFile(_ string, _ int, _ fs.FileMode) (fsapi.File, error) {
	return &mockFile{}, nil
}

func (m *mockFS) Remove(_ string) error {
	return nil
}

func (m *mockFS) Mkdir(_ string, _ fs.FileMode) error {
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

func (m *mockFile) Seek(_ int64, _ int) (int64, error) {
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

func newTestFSRegistry(_ *testing.T) (*Registry, event.Bus) {
	logger := zap.NewNop()
	bus := eventbus.NewBus()
	reg := NewFSRegistry(bus, logger)
	return reg, bus
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
	ctx := ctxapi.NewRootContext()
	fsRegistry, _ := newTestFSRegistry(t)

	// Test adding registry to context
	ctxWithReg := fsapi.WithRegistry(ctx, fsRegistry)

	// Test retrieving registry from context
	retrievedRegistry := fsapi.GetRegistry(ctxWithReg)
	assert.NotNil(t, retrievedRegistry)
	assert.Equal(t, fsRegistry, retrievedRegistry)

	// Test retrieving from original context also works (AppContext is shared)
	retrievedFromOriginal := fsapi.GetRegistry(ctx)
	assert.NotNil(t, retrievedFromOriginal)
	assert.Equal(t, fsRegistry, retrievedFromOriginal)

	// Test retrieving from context without registry
	emptyCtx := ctxapi.NewRootContext()
	emptyRegistry := fsapi.GetRegistry(emptyCtx)
	assert.Nil(t, emptyRegistry)
}

func TestFSRegistry_ConcurrentOperations(t *testing.T) {
	ctx := context.Background()
	fsRegistry, bus := newTestFSRegistry(t)
	require.NoError(t, fsRegistry.Start(ctx))
	defer func() { assert.NoError(t, fsRegistry.Stop()) }()

	const numGoroutines = 10
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	// Test concurrent registration and deletion
	for i := 0; i < numGoroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			fsID := fmt.Sprintf("test:concurrent-fs-%d", idx)
			f := &mockFS{}

			// Register
			bus.Send(ctx, event.Event{
				System: fsapi.System,
				Kind:   fsapi.Register,
				Path:   fsID,
				Data:   f,
			})

			// Verify registration
			time.Sleep(10 * time.Millisecond)
			registeredFS, exists := fsRegistry.GetFS(fsID)
			assert.True(t, exists)
			assert.NotNil(t, registeredFS)

			// Delete
			bus.Send(ctx, event.Event{
				System: fsapi.System,
				Kind:   fsapi.Delete,
				Path:   fsID,
			})

			// Verify deletion
			time.Sleep(10 * time.Millisecond)
			_, exists = fsRegistry.GetFS(fsID)
			assert.False(t, exists)
		}(i)
	}

	wg.Wait()
}

func TestFSRegistry_ErrorHandling(t *testing.T) {
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

	t.Run("delete non-existent filesystem", func(t *testing.T) {
		bus.Send(ctx, event.Event{
			System: fsapi.System,
			Kind:   fsapi.Delete,
			Path:   "test:non-existent",
		})

		select {
		case resp := <-responses:
			assert.Equal(t, fsapi.Reject, resp.Kind)
			assert.Equal(t, "test:non-existent", resp.Path)
			assert.Equal(t, "filesystem not found", resp.Data)
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for response")
		}
	})

	t.Run("register with nil filesystem", func(t *testing.T) {
		bus.Send(ctx, event.Event{
			System: fsapi.System,
			Kind:   fsapi.Register,
			Path:   "test:nil-fs",
			Data:   nil,
		})

		select {
		case resp := <-responses:
			assert.Equal(t, fsapi.Reject, resp.Kind)
			assert.Equal(t, "test:nil-fs", resp.Path)
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for response")
		}
	})
}

func TestFSRegistry_EdgeCases(t *testing.T) {
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

	t.Run("register with empty path", func(t *testing.T) {
		f := &mockFS{}
		bus.Send(ctx, event.Event{
			System: fsapi.System,
			Kind:   fsapi.Register,
			Path:   "",
			Data:   f,
		})

		// Wait for registration response
		select {
		case resp := <-responses:
			assert.Equal(t, fsapi.Accept, resp.Kind)
			assert.Equal(t, "", resp.Path)
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for registration response")
		}

		// Verify filesystem was registered
		registeredFS, exists := fsRegistry.GetFS("")
		assert.True(t, exists)
		assert.NotNil(t, registeredFS)
	})

	t.Run("register same filesystem twice", func(t *testing.T) {
		fsID := "test:duplicate-fs"
		f := &mockFS{}

		// First registration
		bus.Send(ctx, event.Event{
			System: fsapi.System,
			Kind:   fsapi.Register,
			Path:   fsID,
			Data:   f,
		})

		// Wait for first registration response
		select {
		case resp := <-responses:
			assert.Equal(t, fsapi.Accept, resp.Kind)
			assert.Equal(t, fsID, resp.Path)
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for first registration response")
		}

		// Second registration
		bus.Send(ctx, event.Event{
			System: fsapi.System,
			Kind:   fsapi.Register,
			Path:   fsID,
			Data:   f,
		})

		// Wait for second registration response
		select {
		case resp := <-responses:
			assert.Equal(t, fsapi.Accept, resp.Kind)
			assert.Equal(t, fsID, resp.Path)
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for second registration response")
		}

		// Verify filesystem is still registered
		registeredFS, exists := fsRegistry.GetFS(fsID)
		assert.True(t, exists)
		assert.NotNil(t, registeredFS)
	})

	t.Run("get non-existent filesystem", func(t *testing.T) {
		fsi, exists := fsRegistry.GetFS("test:non-existent")
		assert.False(t, exists)
		assert.Nil(t, fsi)
	})
}
