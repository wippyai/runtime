// SPDX-License-Identifier: MPL-2.0

package directory

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"testing"
	"time"

	"github.com/wippyai/runtime/api/attrs"
	ctxapi "github.com/wippyai/runtime/api/context"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/event"
	fsapi "github.com/wippyai/runtime/api/fs"
	moduleapi "github.com/wippyai/runtime/api/modules"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	dirapi "github.com/wippyai/runtime/api/service/fs/directory"
	"github.com/wippyai/runtime/system/eventbus"
	"go.uber.org/zap"
)

// MockFactory is a mock implementation of FactoryAPI for testing.
type MockFactory struct {
	mockFS  fsapi.FS
	err     error
	Configs []CreateFSConfig
}

func NewMockFactory(mockFS fsapi.FS, err error) *MockFactory {
	return &MockFactory{
		mockFS: mockFS,
		err:    err,
	}
}

func (f *MockFactory) CreateFS(cfg CreateFSConfig) (fsapi.FS, error) {
	f.Configs = append(f.Configs, cfg)
	return f.mockFS, f.err
}

// MockFS implements fsapi.FS for testing
type MockFS struct{}

func (m *MockFS) Open(_ string) (fs.File, error) {
	return nil, errors.New("not implemented")
}

func (m *MockFS) Stat(_ string) (fs.FileInfo, error) {
	return nil, errors.New("not implemented")
}

func (m *MockFS) ReadDir(_ string) ([]fs.DirEntry, error) {
	return nil, errors.New("not implemented")
}

func (m *MockFS) OpenFile(_ string, _ int, _ fs.FileMode) (fsapi.File, error) {
	return nil, errors.New("not implemented")
}

func (m *MockFS) Remove(_ string) error {
	return errors.New("not implemented")
}

func (m *MockFS) Mkdir(_ string, _ fs.FileMode) error {
	return errors.New("not implemented")
}

func (m *MockFS) Lstat(_ string) (fs.FileInfo, error) {
	return nil, errors.New("not implemented")
}

func (m *MockFS) Rename(_, _ string) error {
	return errors.New("not implemented")
}

func (m *MockFS) Truncate(_ string, _ int64) error {
	return errors.New("not implemented")
}

func (m *MockFS) Chtimes(_ string, _, _ time.Time) error {
	return errors.New("not implemented")
}

// MockPayload implements payload.Payload for testing
type MockPayload struct {
	data   any
	format payload.Format
}

func (p *MockPayload) Data() any {
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
func NewMockPayload(data any) payload.Payload {
	return &MockPayload{data: data, format: payload.Golang}
}

// MockTranscoder implements payload.Transcoder for testing
type MockTranscoder struct {
	marshalError   error
	unmarshalError error
	mockData       []byte
}

func (m *MockTranscoder) Marshal(_ any) ([]byte, error) {
	if m.marshalError != nil {
		return nil, m.marshalError
	}
	return m.mockData, nil
}

func (m *MockTranscoder) Unmarshal(p payload.Payload, v any) error {
	if m.unmarshalError != nil {
		return m.unmarshalError
	}

	// For simplicity, mock implementation that sets predefined values
	if cfg, ok := v.(*dirapi.Config); ok {
		switch data := p.Data().(type) {
		case *dirapi.Config:
			*cfg = *data
			return nil
		case dirapi.Config:
			*cfg = data
			return nil
		}
		cfg.Directory = "/tmp/test"
		cfg.Mode = "0755"
	}

	return nil
}

// Add the Transcode method to implement the complete interface
func (m *MockTranscoder) Transcode(p payload.Payload, _ payload.Format) (payload.Payload, error) {
	return p, nil
}

func newTestDirectoryManager(_ *testing.T) (*Manager, event.Bus) {
	logger := zap.NewNop()
	bus := eventbus.NewBus()

	// Create a mock transcoder
	transcoder := &MockTranscoder{mockData: []byte(`{"directory":"/tmp/test","mode":"0755"}`)}

	// Create a mock filesystem
	mockFS := &MockFS{}

	// Create a factory that returns our mock
	factory := NewMockFactory(mockFS, nil)

	manager := NewDirectoryManager(bus, transcoder, factory, logger)
	return manager, bus
}

func TestManager_Add(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	manager, bus := newTestDirectoryManager(t)

	// Setup event listener for filesystem registration events
	fsEvents := make(chan event.Event, 1)
	sub, err := eventbus.NewSubscriber(
		ctx,
		bus,
		fsapi.System,
		fsapi.FsRegister,
		func(evt event.Event) {
			fsEvents <- evt
		},
	)
	require.NoError(t, err)
	defer sub.Close()

	testID := registry.NewID("test", "dir1")

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
		manager.mu.RLock()
		stored, exists := manager.directories[testID]
		manager.mu.RUnlock()

		assert.True(t, exists)
		assert.NotNil(t, stored)

		// Verify FS registration event was sent
		select {
		case evt := <-fsEvents:
			assert.Equal(t, fsapi.FsRegister, evt.Kind)
			assert.Equal(t, testID.String(), evt.Path)
			assert.NotNil(t, evt.Data)
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for fs registration event")
		}
	})

	t.Run("wrong entry kind", func(t *testing.T) {
		entry := registry.Entry{
			Kind: "invalid.kind",
			Data: NewMockPayload(&dirapi.Config{
				Directory: "/tmp/test",
				Mode:      "0755",
			}),
		}

		err := manager.Add(ctx, entry)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported entry kind")
	})

	t.Run("unmarshal error", func(t *testing.T) {
		// Configure transcoder to return error
		manager.dtt = &MockTranscoder{unmarshalError: assert.AnError}

		entry := registry.Entry{
			Kind: dirapi.Kind,
			Data: NewMockPayload("invalid json"),
		}

		err := manager.Add(ctx, entry)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to decode config")

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
		require.Error(t, err)
		assert.Contains(t, err.Error(), "already exists")
	})
}

func TestManager_Update(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	manager, bus := newTestDirectoryManager(t)

	// Setup event listener for filesystem registration events
	fsEvents := make(chan event.Event, 1)
	sub, err := eventbus.NewSubscriber(
		ctx,
		bus,
		fsapi.System,
		fsapi.FsRegister,
		func(evt event.Event) {
			fsEvents <- evt
		},
	)
	require.NoError(t, err)
	defer sub.Close()

	testID := registry.NewID("test", "dir1")

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
			assert.Equal(t, fsapi.FsRegister, evt.Kind)
			assert.Equal(t, testID.String(), evt.Path)
			assert.NotNil(t, evt.Data)
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for fs update event")
		}
	})

	t.Run("directory not found", func(t *testing.T) {
		nonExistentEntry := registry.Entry{
			Kind: dirapi.Kind,
			Data: NewMockPayload(&dirapi.Config{
				Directory: "/tmp/test2",
				Mode:      "0755",
			}),
		}

		err := manager.Update(ctx, nonExistentEntry)
		require.Error(t, err)
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
		require.Error(t, err)
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
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to decode config")

		// Reset transcoder for other tests
		manager.dtt = &MockTranscoder{mockData: []byte(`{"directory":"/tmp/test","mode":"0755"}`)}
	})
}

func TestManager_Delete(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	manager, bus := newTestDirectoryManager(t)

	// Setup event listener for filesystem deletion events
	fsEvents := make(chan event.Event, 1)
	sub, err := eventbus.NewSubscriber(
		ctx,
		bus,
		fsapi.System,
		fsapi.FsDelete,
		func(evt event.Event) {
			fsEvents <- evt
		},
	)
	require.NoError(t, err)
	defer sub.Close()

	testID := registry.NewID("test", "dir1")

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
		manager.mu.RLock()
		_, exists := manager.directories[testID]
		manager.mu.RUnlock()
		assert.False(t, exists)

		// Verify FS deletion event was sent
		select {
		case evt := <-fsEvents:
			assert.Equal(t, fsapi.FsDelete, evt.Kind)
			assert.Equal(t, testID.String(), evt.Path)
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for fs deletion event")
		}
	})

	t.Run("directory not found", func(t *testing.T) {
		err := manager.Delete(ctx, entry) // Try to delete again
		require.Error(t, err)
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
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported entry kind")
	})
}

func TestManager_RegisterFS(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	manager, bus := newTestDirectoryManager(t)

	// Setup event listener for filesystem registration events
	fsEvents := make(chan event.Event, 1)
	sub, err := eventbus.NewSubscriber(
		ctx,
		bus,
		fsapi.System,
		fsapi.FsRegister,
		func(evt event.Event) {
			fsEvents <- evt
		},
	)
	require.NoError(t, err)
	defer sub.Close()

	testID := registry.NewID("test", "dir1")
	cfg := &dirapi.Config{
		Directory: "/tmp/test",
		Mode:      "0755",
	}

	err = manager.registerFS(ctx, testID, cfg)
	require.NoError(t, err)

	// Verify FS was stored
	manager.mu.RLock()
	stored, exists := manager.directories[testID]
	manager.mu.RUnlock()
	assert.True(t, exists)
	assert.NotNil(t, stored)

	// Verify FS registration event was sent
	select {
	case evt := <-fsEvents:
		assert.Equal(t, fsapi.FsRegister, evt.Kind)
		assert.Equal(t, testID.String(), evt.Path)
		assert.NotNil(t, evt.Data)
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for fs registration event")
	}
}

func TestResolveDirectoryPath(t *testing.T) {
	moduleRoot := t.TempDir()
	ctx := ctxapi.NewRootContext()
	ctx = moduleapi.WithSourceRoots(ctx, moduleapi.SourceRoots{
		"acme/ui": moduleRoot,
	})

	entry := registry.Entry{
		ID: registry.NewID("acme.ui", "static_fs"),
		Meta: attrs.NewBagFrom(map[string]any{
			"module": "acme/ui",
		}),
	}

	tests := []struct {
		name string
		cfg  *dirapi.Config
		want string
	}{
		{
			name: "module-owned entry with no base resolves against module source root",
			cfg:  &dirapi.Config{Directory: "./frontend"},
			want: filepath.Join(moduleRoot, "frontend"),
		},
		{
			name: "project base remains working-directory relative",
			cfg:  &dirapi.Config{Directory: "./frontend", Base: dirapi.BaseProject},
			want: "./frontend",
		},
		{
			name: "module base resolves against module source root",
			cfg:  &dirapi.Config{Directory: "./static/app", Base: dirapi.BaseModule},
			want: filepath.Join(moduleRoot, "static/app"),
		},
		{
			name: "absolute module path is preserved",
			cfg:  &dirapi.Config{Directory: "/srv/static", Base: dirapi.BaseModule},
			want: "/srv/static",
		},
		{
			name: "module base without module metadata falls back to raw path",
			cfg:  &dirapi.Config{Directory: "./static/app", Base: dirapi.BaseModule},
			want: "./static/app",
		},
		{
			name: "app-owned entry (no module meta) with no base stays working-directory relative",
			cfg:  &dirapi.Config{Directory: "./frontend"},
			want: "./frontend",
		},
		{
			name: "module-owned entry with explicit project base stays working-directory relative",
			cfg:  &dirapi.Config{Directory: "./frontend", Base: dirapi.BaseProject},
			want: "./frontend",
		},
		{
			name: "module-owned entry with no base falls back to raw path when source root missing",
			cfg:  &dirapi.Config{Directory: "./frontend"},
			want: "./frontend",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testEntry := entry
			testCtx := ctx

			switch tt.name {
			case "module base without module metadata falls back to raw path",
				"app-owned entry (no module meta) with no base stays working-directory relative":
				testEntry.Meta = nil
			case "module-owned entry with no base falls back to raw path when source root missing":
				testCtx = ctxapi.NewRootContext()
			}

			assert.Equal(t, tt.want, resolveDirectoryPath(testCtx, testEntry, tt.cfg))
		})
	}
}

func TestManager_AddUsesModuleBaseForDirectoryPath(t *testing.T) {
	moduleRoot := t.TempDir()
	ctx := ctxapi.NewRootContext()
	ctx = moduleapi.WithSourceRoots(ctx, moduleapi.SourceRoots{
		"acme/ui": moduleRoot,
	})

	factory := NewMockFactory(&MockFS{}, nil)
	manager := NewDirectoryManager(eventbus.NewBus(), &MockTranscoder{}, factory, zap.NewNop())

	entry := registry.Entry{
		ID:   registry.NewID("acme.ui", "static_fs"),
		Kind: dirapi.Kind,
		Meta: attrs.NewBagFrom(map[string]any{
			"module": "acme/ui",
		}),
		Data: NewMockPayload(&dirapi.Config{
			Directory: "./static/app",
			Base:      dirapi.BaseModule,
			Mode:      "0755",
		}),
	}

	err := manager.Add(ctx, entry)
	require.NoError(t, err)
	require.Len(t, factory.Configs, 1)
	assert.Equal(t, filepath.Join(moduleRoot, "static/app"), factory.Configs[0].DirPath)
}

// Regression: when a module is loaded via wippy.lock `replacements:` the entry's
// fs.directory has a relative path (./public) and no explicit `base: module`.
// The runtime must still join with the module source root, matching the path
// that vendor-unpacked modules see.
func TestManager_AddResolvesModuleRelativePathWhenBaseOmitted(t *testing.T) {
	moduleRoot := t.TempDir()
	ctx := ctxapi.NewRootContext()
	ctx = moduleapi.WithSourceRoots(ctx, moduleapi.SourceRoots{
		"wippy/facade": moduleRoot,
	})

	factory := NewMockFactory(&MockFS{}, nil)
	manager := NewDirectoryManager(eventbus.NewBus(), &MockTranscoder{}, factory, zap.NewNop())

	entry := registry.Entry{
		ID:   registry.NewID("wippy.facade", "public_files"),
		Kind: dirapi.Kind,
		Meta: attrs.NewBagFrom(map[string]any{
			"module": "wippy/facade",
		}),
		Data: NewMockPayload(&dirapi.Config{
			Directory: "./public",
			Mode:      "0755",
		}),
	}

	err := manager.Add(ctx, entry)
	require.NoError(t, err)
	require.Len(t, factory.Configs, 1)
	assert.Equal(t, filepath.Join(moduleRoot, "public"), factory.Configs[0].DirPath)
}

// Add test for factory error handling
func TestManager_FactoryError(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	logger := zap.NewNop()
	bus := eventbus.NewBus()
	transcoder := &MockTranscoder{}

	expectedErr := fmt.Errorf("factory error")
	factory := NewMockFactory(nil, expectedErr)

	manager := NewDirectoryManager(bus, transcoder, factory, logger)

	testID := registry.NewID("test", "error")
	cfg := &dirapi.Config{
		Directory: "/tmp/test",
		Mode:      "0755",
	}

	err := manager.registerFS(ctx, testID, cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create filesystem")
}
