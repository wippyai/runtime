package directory

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"testing"
	"time"

	"github.com/ponyruntime/pony/api/event"
	fsapi "github.com/ponyruntime/pony/api/fs"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	dirapi "github.com/ponyruntime/pony/api/service/directory"
	"github.com/ponyruntime/pony/system/eventbus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// MockFSFactory is a mock implementation of FSFactoryAPI for testing
type MockFSFactory struct {
	mockFS fsapi.FS
	err    error
}

func NewMockFSFactory(mockFS fsapi.FS, err error) *MockFSFactory {
	return &MockFSFactory{
		mockFS: mockFS,
		err:    err,
	}
}

func (f *MockFSFactory) CreateFS(t, dirPath string, mode fs.FileMode) (fsapi.FS, error) {
	return f.mockFS, f.err
}

// MockFS implements fsapi.FS for testing
type MockFS struct{}

func (m *MockFS) Open(name string) (fs.File, error) {
	return nil, errors.New("not implemented")
}

func (m *MockFS) Stat(name string) (fs.FileInfo, error) {
	return nil, errors.New("not implemented")
}

func (m *MockFS) ReadDir(name string) ([]fs.DirEntry, error) {
	return nil, errors.New("not implemented")
}

func (m *MockFS) OpenFile(name string, flag int, perm fs.FileMode) (fsapi.File, error) {
	return nil, errors.New("not implemented")
}

func (m *MockFS) Remove(name string) error {
	return errors.New("not implemented")
}

func (m *MockFS) Mkdir(name string, perm fs.FileMode) error {
	return errors.New("not implemented")
}

// MockPayload implements payload.Payload for testing
type MockPayload struct {
	data   interface{}
	format payload.Format
}

func (p *MockPayload) Data() interface{} {
	return p.data
}

func (p *MockPayload) Format() payload.Format {
	return p.format
}

// Transcode method for the MockPayload
func (p *MockPayload) Transcode(format payload.Format) (payload.Payload, error) {
	return &MockPayload{data: p.data, format: format}, nil
}

// Function to create mock payloads
func NewMockPayload(data interface{}) payload.Payload {
	return &MockPayload{data: data, format: payload.Golang}
}

// MockTranscoder implements payload.Transcoder for testing
type MockTranscoder struct {
	marshalError   error
	unmarshalError error
	mockData       []byte
}

func (m *MockTranscoder) Marshal(v any) ([]byte, error) {
	if m.marshalError != nil {
		return nil, m.marshalError
	}
	return m.mockData, nil
}

func (m *MockTranscoder) Unmarshal(data payload.Payload, v any) error {
	if m.unmarshalError != nil {
		return m.unmarshalError
	}

	// For simplicity, mock implementation that sets predefined values
	if cfg, ok := v.(*dirapi.Config); ok {
		cfg.Directory = "/tmp/test"
		cfg.Mode = "0755"
	}

	return nil
}

// Add the Transcode method to implement the complete interface
func (m *MockTranscoder) Transcode(p payload.Payload, format payload.Format) (payload.Payload, error) {
	return p, nil
}

func newTestDirectoryManager(t *testing.T) (*Manager, event.Bus) {
	logger := zap.NewNop()
	bus := eventbus.NewBus()

	// Create a mock transcoder
	transcoder := &MockTranscoder{mockData: []byte(`{"directory":"/tmp/test","mode":"0755"}`)}

	// Create a mock filesystem
	mockFS := &MockFS{}

	// Create a factory that returns our mock
	factory := NewMockFSFactory(mockFS, nil)

	manager := NewDirectoryManager(bus, transcoder, factory, logger)
	return manager, bus
}

func TestManager_Add(t *testing.T) {
	ctx := context.Background()
	manager, bus := newTestDirectoryManager(t)

	// Setup event listener for filesystem registration events
	fsEvents := make(chan event.Event, 1)
	sub, err := eventbus.NewSubscriber(
		ctx,
		bus,
		fsapi.System,
		fsapi.Register,
		func(evt event.Event) {
			fsEvents <- evt
		},
	)
	require.NoError(t, err)
	defer sub.Close()

	testID := registry.ID{NS: "test", Name: "dir1"}

	t.Run("successful directory addition", func(t *testing.T) {
		entry := registry.Entry{
			ID:   testID,
			Kind: dirapi.Kind,
			Data: NewMockPayload(&dirapi.Config{
				Directory: "/tmp/test",
				Mode:      "0755",
			}),
		}

		err := manager.Add(ctx, entry)
		require.NoError(t, err)

		// Verify directory was registered
		var stored interface{}
		var exists bool

		manager.directories.Range(func(key, value interface{}) bool {
			if key.(string) == testID.String() {
				stored = value
				exists = true
				return false
			}
			return true
		})

		assert.True(t, exists)
		assert.NotNil(t, stored)

		// Verify FS registration event was sent
		select {
		case evt := <-fsEvents:
			assert.Equal(t, fsapi.Register, evt.Kind)
			assert.Equal(t, testID.String(), evt.Path)
			assert.NotNil(t, evt.Data)
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for fs registration event")
		}
	})

	t.Run("wrong entry kind", func(t *testing.T) {
		entry := registry.Entry{
			ID:   registry.ID{NS: "test", Name: "invalid"},
			Kind: "invalid.kind",
			Data: NewMockPayload(&dirapi.Config{
				Directory: "/tmp/test",
				Mode:      "0755",
			}),
		}

		err := manager.Add(ctx, entry)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported entry kind")
	})

	t.Run("unmarshal error", func(t *testing.T) {
		// Configure transcoder to return error
		manager.dtt = &MockTranscoder{unmarshalError: assert.AnError}

		entry := registry.Entry{
			ID:   registry.ID{NS: "test", Name: "error"},
			Kind: dirapi.Kind,
			Data: NewMockPayload("invalid json"),
		}

		err := manager.Add(ctx, entry)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to unmarshal config")

		// Reset transcoder for other tests
		manager.dtt = &MockTranscoder{mockData: []byte(`{"directory":"/tmp/test","mode":"0755"}`)}
	})

	t.Run("duplicate directory", func(t *testing.T) {
		entry := registry.Entry{
			ID:   testID, // Same as in successful test
			Kind: dirapi.Kind,
			Data: NewMockPayload(&dirapi.Config{
				Directory: "/tmp/test",
				Mode:      "0755",
			}),
		}

		err := manager.Add(ctx, entry)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "already exists")
	})
}

func TestManager_Update(t *testing.T) {
	ctx := context.Background()
	manager, bus := newTestDirectoryManager(t)

	// Setup event listener for filesystem registration events
	fsEvents := make(chan event.Event, 1)
	sub, err := eventbus.NewSubscriber(
		ctx,
		bus,
		fsapi.System,
		fsapi.Register,
		func(evt event.Event) {
			fsEvents <- evt
		},
	)
	require.NoError(t, err)
	defer sub.Close()

	testID := registry.ID{NS: "test", Name: "dir1"}

	// First add a directory
	entry := registry.Entry{
		ID:   testID,
		Kind: dirapi.Kind,
		Data: NewMockPayload(&dirapi.Config{
			Directory: "/tmp/test",
			Mode:      "0755",
		}),
	}

	err = manager.Add(ctx, entry)
	require.NoError(t, err)

	// Drain event from the add operation
	select {
	case <-fsEvents:
		// Ignore this event
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for initial fs registration event")
	}

	t.Run("successful update", func(t *testing.T) {
		// Update the same directory
		err := manager.Update(ctx, entry)
		require.NoError(t, err)

		// Verify FS registration event was sent again
		select {
		case evt := <-fsEvents:
			assert.Equal(t, fsapi.Register, evt.Kind)
			assert.Equal(t, testID.String(), evt.Path)
			assert.NotNil(t, evt.Data)
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for fs update event")
		}
	})

	t.Run("directory not found", func(t *testing.T) {
		nonExistentEntry := registry.Entry{
			ID:   registry.ID{NS: "test", Name: "nonexistent"},
			Kind: dirapi.Kind,
			Data: NewMockPayload(&dirapi.Config{
				Directory: "/tmp/test2",
				Mode:      "0755",
			}),
		}

		err := manager.Update(ctx, nonExistentEntry)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("wrong entry kind", func(t *testing.T) {
		invalidEntry := registry.Entry{
			ID:   testID,
			Kind: "invalid.kind",
			Data: NewMockPayload(&dirapi.Config{
				Directory: "/tmp/test",
				Mode:      "0755",
			}),
		}

		err := manager.Update(ctx, invalidEntry)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported entry kind")
	})

	t.Run("unmarshal error", func(t *testing.T) {
		// Configure transcoder to return error
		manager.dtt = &MockTranscoder{unmarshalError: assert.AnError}

		entry := registry.Entry{
			ID:   testID,
			Kind: dirapi.Kind,
			Data: NewMockPayload("invalid json"),
		}

		err := manager.Update(ctx, entry)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to unmarshal config")

		// Reset transcoder for other tests
		manager.dtt = &MockTranscoder{mockData: []byte(`{"directory":"/tmp/test","mode":"0755"}`)}
	})
}

func TestManager_Delete(t *testing.T) {
	ctx := context.Background()
	manager, bus := newTestDirectoryManager(t)

	// Setup event listener for filesystem deletion events
	fsEvents := make(chan event.Event, 1)
	sub, err := eventbus.NewSubscriber(
		ctx,
		bus,
		fsapi.System,
		fsapi.Delete,
		func(evt event.Event) {
			fsEvents <- evt
		},
	)
	require.NoError(t, err)
	defer sub.Close()

	testID := registry.ID{NS: "test", Name: "dir1"}

	// First add a directory
	entry := registry.Entry{
		ID:   testID,
		Kind: dirapi.Kind,
		Data: NewMockPayload(&dirapi.Config{
			Directory: "/tmp/test",
			Mode:      "0755",
		}),
	}

	err = manager.Add(ctx, entry)
	require.NoError(t, err)

	t.Run("successful deletion", func(t *testing.T) {
		err := manager.Delete(ctx, entry)
		require.NoError(t, err)

		// Verify directory was removed
		var exists bool
		manager.directories.Range(func(key, value interface{}) bool {
			if key.(string) == testID.String() {
				exists = true
				return false
			}
			return true
		})
		assert.False(t, exists)

		// Verify FS deletion event was sent
		select {
		case evt := <-fsEvents:
			assert.Equal(t, fsapi.Delete, evt.Kind)
			assert.Equal(t, testID.String(), evt.Path)
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for fs deletion event")
		}
	})

	t.Run("directory not found", func(t *testing.T) {
		err := manager.Delete(ctx, entry) // Try to delete again
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("wrong entry kind", func(t *testing.T) {
		invalidEntry := registry.Entry{
			ID:   testID,
			Kind: "invalid.kind",
			Data: NewMockPayload(&dirapi.Config{
				Directory: "/tmp/test",
				Mode:      "0755",
			}),
		}

		err := manager.Delete(ctx, invalidEntry)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported entry kind")
	})
}

func TestManager_RegisterFS(t *testing.T) {
	ctx := context.Background()
	manager, bus := newTestDirectoryManager(t)

	// Setup event listener for filesystem registration events
	fsEvents := make(chan event.Event, 1)
	sub, err := eventbus.NewSubscriber(
		ctx,
		bus,
		fsapi.System,
		fsapi.Register,
		func(evt event.Event) {
			fsEvents <- evt
		},
	)
	require.NoError(t, err)
	defer sub.Close()

	testID := registry.ID{NS: "test", Name: "dir1"}
	cfg := &dirapi.Config{
		Directory: "/tmp/test",
		Mode:      "0755",
	}

	err = manager.registerFS(ctx, testID, cfg)
	require.NoError(t, err)

	// Verify FS was stored
	stored, exists := manager.directories.Load(testID.String())
	assert.True(t, exists)
	assert.NotNil(t, stored)

	// Verify FS registration event was sent
	select {
	case evt := <-fsEvents:
		assert.Equal(t, fsapi.Register, evt.Kind)
		assert.Equal(t, testID.String(), evt.Path)
		assert.NotNil(t, evt.Data)
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for fs registration event")
	}
}

// Add test for factory error handling
func TestManager_FactoryError(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()
	bus := eventbus.NewBus()
	transcoder := &MockTranscoder{}

	expectedErr := fmt.Errorf("factory error")
	factory := NewMockFSFactory(nil, expectedErr)

	manager := NewDirectoryManager(bus, transcoder, factory, logger)

	testID := registry.ID{NS: "test", Name: "error"}
	cfg := &dirapi.Config{
		Directory: "/tmp/test",
		Mode:      "0755",
	}

	err := manager.registerFS(ctx, testID, cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), expectedErr.Error())
}
