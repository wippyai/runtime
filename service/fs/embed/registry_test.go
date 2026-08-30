// SPDX-License-Identifier: MPL-2.0

package embed

import (
	"bytes"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/wapp"
)

func TestRegistry_NewRegistry(t *testing.T) {
	r := NewRegistry()
	require.NotNil(t, r)
	assert.NotNil(t, r.packs)
	assert.Len(t, r.packs, 0)
}

func TestRegistry_Register(t *testing.T) {
	t.Run("empty pack path", func(t *testing.T) {
		r := NewRegistry()

		err := r.Register("", nil, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "packPath cannot be empty")
	})

	t.Run("nil reader", func(t *testing.T) {
		r := NewRegistry()

		err := r.Register("test/pack", nil, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "reader cannot be nil")
	})

	t.Run("nil file is allowed", func(t *testing.T) {
		r := NewRegistry()
		reader := createTestReader(t)

		err := r.Register("test/pack", reader, nil)
		require.NoError(t, err)

		r.mu.RLock()
		assert.Len(t, r.packs, 1)
		assert.Nil(t, r.packs["test/pack"].file)
		r.mu.RUnlock()
	})

	t.Run("tracks file handle", func(t *testing.T) {
		r := NewRegistry()
		reader, file := createTestReaderWithFile(t)
		defer file.Close()

		err := r.Register("test/pack", reader, file)
		require.NoError(t, err)

		r.mu.RLock()
		assert.Len(t, r.packs, 1)
		assert.NotNil(t, r.packs["test/pack"].file)
		r.mu.RUnlock()
	})

	t.Run("rejects an existing pack path without disturbing it", func(t *testing.T) {
		r := NewRegistry()
		reader1, file1 := createTestReaderWithFile(t)
		reader2 := createTestReader(t)

		require.NoError(t, r.Register("test/pack", reader1, file1))
		require.Error(t, r.Register("test/pack", reader2, nil))

		r.mu.RLock()
		assert.Len(t, r.packs, 1)
		assert.Equal(t, reader1, r.packs["test/pack"].reader)
		r.mu.RUnlock()

		// The active pack remains owned by the registry.
		_, err := file1.Stat()
		require.NoError(t, err)
		require.NoError(t, r.UnregisterPack("test/pack"))
	})
}

func TestRegistry_RegisterPack(t *testing.T) {
	t.Run("records module and version", func(t *testing.T) {
		r := NewRegistry()
		reader := createTestReader(t)

		err := r.RegisterPack("vendor/org/mod-v1.0.0.wapp", "org/mod", "1.0.0", reader, nil)
		require.NoError(t, err)

		r.mu.RLock()
		p := r.packs["vendor/org/mod-v1.0.0.wapp"]
		r.mu.RUnlock()
		require.NotNil(t, p)
		assert.Equal(t, "org/mod", p.module)
		assert.Equal(t, "1.0.0", p.version)
	})

	t.Run("indexes pack resources", func(t *testing.T) {
		r := NewRegistry()
		reader := createReaderWithResource(t, "ui", "assets", map[string]string{"index.html": "<html>"})

		require.NoError(t, r.RegisterPack("p.wapp", "org/mod", "1.0.0", reader, nil))

		r.mu.RLock()
		p := r.packs["p.wapp"]
		r.mu.RUnlock()
		assert.True(t, p.owns(registry.NewID("ui", "assets")))
		assert.False(t, p.owns(registry.NewID("ui", "missing")))
	})
}

func TestRegistry_GetFS(t *testing.T) {
	t.Run("not found with no packs", func(t *testing.T) {
		r := NewRegistry()

		_, err := r.GetFS(registry.NewID("test", "notfound"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("not found with packs but no matching resource", func(t *testing.T) {
		r := NewRegistry()
		reader := createReaderWithResource(t, "ui", "assets", map[string]string{"a.txt": "a"})
		require.NoError(t, r.RegisterPack("p.wapp", "org/mod", "1.0.0", reader, nil))

		_, err := r.GetFS(registry.NewID("nonexistent", "resource"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("returns matching filesystem", func(t *testing.T) {
		r := NewRegistry()
		reader := createReaderWithResource(t, "ui", "assets", map[string]string{"index.html": "<html>hi</html>"})
		require.NoError(t, r.RegisterPack("p.wapp", "org/mod", "1.0.0", reader, nil))

		fsys, err := r.GetFS(registry.NewID("ui", "assets"))
		require.NoError(t, err)
		data, err := fs.ReadFile(fsys, "index.html")
		require.NoError(t, err)
		assert.Equal(t, "<html>hi</html>", string(data))
	})
}

func TestRegistry_ActiveResourceMapping(t *testing.T) {
	r := NewRegistry()
	oldReader := createReaderWithResource(t, "ui", "app", map[string]string{"v.txt": "old"})
	newReader := createReaderWithResource(t, "ui", "app", map[string]string{"v.txt": "new"})
	require.NoError(t, r.RegisterPack("org/mod-v1.0.0.wapp", "org/mod", "1.0.0", oldReader, nil))

	fsys, err := r.GetFS(registry.NewID("ui", "app"))
	require.NoError(t, err)
	data, err := fs.ReadFile(fsys, "v.txt")
	require.NoError(t, err)
	assert.Equal(t, "old", string(data))

	require.NoError(t, r.RegisterPack("org/mod-v2.0.0.wapp", "org/mod", "2.0.0", newReader, nil))
	data, err = fs.ReadFile(fsys, "v.txt")
	require.NoError(t, err)
	assert.Equal(t, "new", string(data))

	require.NoError(t, r.UnregisterPack("org/mod-v2.0.0.wapp"))
	data, err = fs.ReadFile(fsys, "v.txt")
	require.NoError(t, err)
	assert.Equal(t, "old", string(data))
}

func TestRegistry_StagedResourceBindsNewHandleUntilActivation(t *testing.T) {
	r := NewRegistry()
	reader := createReaderWithResource(t, "ui", "next", map[string]string{"v.txt": "next"})
	require.NoError(t, r.StagePack("org/mod-v2.0.0.wapp", "org/mod", "2.0.0", reader, nil))

	fsys, err := r.GetFS(registry.NewID("ui", "next"))
	require.NoError(t, err)
	data, err := fs.ReadFile(fsys, "v.txt")
	require.NoError(t, err)
	assert.Equal(t, "next", string(data))

	require.NoError(t, r.ActivatePack("org/mod-v2.0.0.wapp"))
	data, err = fs.ReadFile(fsys, "v.txt")
	require.NoError(t, err)
	assert.Equal(t, "next", string(data))
}

func TestRegistry_ActivatePacksDoesNotPartiallySwitch(t *testing.T) {
	r := NewRegistry()
	reader := createReaderWithResource(t, "ui", "one", map[string]string{"v.txt": "one"})
	require.NoError(t, r.StagePack("org/one.wapp", "org/one", "1.0.0", reader, nil))

	require.Error(t, r.ActivatePacks([]string{"org/one.wapp", "missing.wapp"}))
	r.mu.RLock()
	active := r.packs["org/one.wapp"].active.Load()
	r.mu.RUnlock()
	assert.False(t, active)
}

func TestRegistry_RejectsResourceCollisionAcrossModules(t *testing.T) {
	r := NewRegistry()
	first := createReaderWithResource(t, "ui", "app", map[string]string{"v.txt": "one"})
	second := createReaderWithResource(t, "ui", "app", map[string]string{"v.txt": "two"})
	require.NoError(t, r.RegisterPack("org/one.wapp", "org/one", "1.0.0", first, nil))
	require.Error(t, r.RegisterPack("org/two.wapp", "org/two", "1.0.0", second, nil))
}

func TestRegistry_RetiresPackAfterOpenFileCloses(t *testing.T) {
	r := NewRegistry()
	reader, file := createReaderWithResourceAndFile(t, "ui", "app", map[string]string{"v.txt": "open"})
	require.NoError(t, r.RegisterPack("org/mod.wapp", "org/mod", "1.0.0", reader, file))

	fsys, err := r.GetFS(registry.NewID("ui", "app"))
	require.NoError(t, err)
	open, err := fsys.Open("v.txt")
	require.NoError(t, err)

	// The retired pack remains readable through an open file, then releases the
	// underlying handle once that read completes.
	require.NoError(t, r.UnregisterPack("org/mod.wapp"))
	data, err := io.ReadAll(open)
	require.NoError(t, err)
	assert.Equal(t, "open", string(data))
	require.NoError(t, open.Close())
	_, err = file.Read(make([]byte, 1))
	require.Error(t, err)
}

func TestRegistry_UnregisterPack(t *testing.T) {
	t.Run("removes pack and closes file", func(t *testing.T) {
		r := NewRegistry()
		reader, file := createTestReaderWithFile(t)
		require.NoError(t, r.RegisterPack("p.wapp", "org/mod", "1.0.0", reader, file))

		require.NoError(t, r.UnregisterPack("p.wapp"))

		r.mu.RLock()
		assert.Len(t, r.packs, 0)
		r.mu.RUnlock()

		_, err := file.Read(make([]byte, 1))
		require.Error(t, err)
	})

	t.Run("unknown pack is a no-op", func(t *testing.T) {
		r := NewRegistry()
		require.NoError(t, r.UnregisterPack("missing.wapp"))
	})
}

func TestRegistry_UnregisterModule(t *testing.T) {
	t.Run("removes only matching module and version", func(t *testing.T) {
		r := NewRegistry()
		readerOld, fileOld := createTestReaderWithFile(t)
		readerNew := createTestReader(t)
		require.NoError(t, r.RegisterPack("mod-v1.wapp", "org/mod", "1.0.0", readerOld, fileOld))
		require.NoError(t, r.RegisterPack("mod-v2.wapp", "org/mod", "2.0.0", readerNew, nil))

		require.NoError(t, r.UnregisterModule("org/mod", "1.0.0"))

		r.mu.RLock()
		_, hasOld := r.packs["mod-v1.wapp"]
		_, hasNew := r.packs["mod-v2.wapp"]
		r.mu.RUnlock()
		assert.False(t, hasOld)
		assert.True(t, hasNew)

		_, err := fileOld.Read(make([]byte, 1))
		require.Error(t, err)
	})

	t.Run("absent module is a no-op", func(t *testing.T) {
		r := NewRegistry()
		require.NoError(t, r.UnregisterModule("org/absent", "1.0.0"))
	})
}

func TestRegistry_HasModulePack(t *testing.T) {
	r := NewRegistry()
	require.NoError(t, r.RegisterPack("mod-v1.wapp", "org/mod", "1.0.0", createTestReader(t), nil))

	assert.True(t, r.HasModulePack("org/mod", "1.0.0"))
	assert.False(t, r.HasModulePack("org/mod", "2.0.0"))
	assert.False(t, r.HasModulePack("org/other", "1.0.0"))
}

func TestRegistry_Close(t *testing.T) {
	t.Run("clears all packs", func(t *testing.T) {
		r := NewRegistry()
		require.NoError(t, r.RegisterPack("p.wapp", "org/mod", "1.0.0", createTestReader(t), nil))

		require.NoError(t, r.Close())

		r.mu.RLock()
		assert.Len(t, r.packs, 0)
		r.mu.RUnlock()
	})

	t.Run("closes tracked files", func(t *testing.T) {
		r := NewRegistry()
		reader, file := createTestReaderWithFile(t)
		require.NoError(t, r.RegisterPack("p.wapp", "org/mod", "1.0.0", reader, file))

		require.NoError(t, r.Close())

		_, err := file.Read(make([]byte, 1))
		require.Error(t, err)
	})

	t.Run("idempotent", func(t *testing.T) {
		r := NewRegistry()
		require.NoError(t, r.Close())
		require.NoError(t, r.Close())

		r.mu.RLock()
		assert.Len(t, r.packs, 0)
		r.mu.RUnlock()
	})
}

func TestRegistry_NoDoubleClose(t *testing.T) {
	// Unregister then Close must not double-close the file handle.
	r := NewRegistry()
	reader, file := createTestReaderWithFile(t)
	require.NoError(t, r.RegisterPack("p.wapp", "org/mod", "1.0.0", reader, file))

	require.NoError(t, r.UnregisterPack("p.wapp"))
	// Close has no remaining handles; must not error from a second close.
	require.NoError(t, r.Close())
}

func TestRegistry_ConcurrentAccess(t *testing.T) {
	t.Run("concurrent register and getfs", func(t *testing.T) {
		r := NewRegistry()
		var wg sync.WaitGroup

		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				reader := createTestReader(t)
				packPath := filepath.Join("test", "pack", string(rune('a'+idx)))
				_ = r.RegisterPack(packPath, "org/mod", "1.0.0", reader, nil)
			}(i)
		}

		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, _ = r.GetFS(registry.NewID("test", "resource"))
			}()
		}

		wg.Wait()
	})

	t.Run("concurrent register same path", func(t *testing.T) {
		r := NewRegistry()
		var wg sync.WaitGroup

		for i := 0; i < 100; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				reader := createTestReader(t)
				_ = r.RegisterPack("same/path", "org/mod", "1.0.0", reader, nil)
			}()
		}

		wg.Wait()

		r.mu.RLock()
		assert.Len(t, r.packs, 1)
		r.mu.RUnlock()
	})

	t.Run("concurrent unregister and close", func(t *testing.T) {
		r := NewRegistry()
		for i := 0; i < 20; i++ {
			packPath := filepath.Join("p", string(rune('a'+i)))
			require.NoError(t, r.RegisterPack(packPath, "org/mod", "1.0.0", createTestReader(t), nil))
		}

		var wg sync.WaitGroup
		for i := 0; i < 20; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				_ = r.UnregisterPack(filepath.Join("p", string(rune('a'+idx))))
			}(i)
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = r.Close()
		}()

		wg.Wait()
	})
}

// createTestReader creates a minimal wapp reader with no resources.
func createTestReader(t *testing.T) *wapp.Reader {
	t.Helper()
	return readerFromBytes(t, packEntriesOnly(t))
}

// createTestReaderWithFile creates a minimal wapp reader and returns the open file handle.
func createTestReaderWithFile(t *testing.T) (*wapp.Reader, *os.File) {
	t.Helper()

	wappPath := filepath.Join(t.TempDir(), "test.wapp")
	require.NoError(t, os.WriteFile(wappPath, packEntriesOnly(t), 0644))

	file, err := os.Open(wappPath)
	require.NoError(t, err)

	reader, err := wapp.NewReader(file)
	require.NoError(t, err)

	return reader, file
}

// createReaderWithResource builds a wapp containing a single filesystem resource.
func createReaderWithResource(t *testing.T, ns, name string, files map[string]string) *wapp.Reader {
	t.Helper()

	mapFS := fstest.MapFS{}
	for path, content := range files {
		mapFS[path] = &fstest.MapFile{Data: []byte(content), Mode: 0644}
	}

	var buf bytes.Buffer
	writer := wapp.NewWriter()
	err := writer.PackWithResources(
		wapp.Metadata{},
		nil,
		[]wapp.ResourceSpec{{ID: wapp.NewID(ns, name), FS: mapFS}},
		&buf,
	)
	require.NoError(t, err)

	return readerFromBytes(t, buf.Bytes())
}

func createReaderWithResourceAndFile(t *testing.T, ns, name string, files map[string]string) (*wapp.Reader, *os.File) {
	t.Helper()
	mapFS := fstest.MapFS{}
	for path, content := range files {
		mapFS[path] = &fstest.MapFile{Data: []byte(content), Mode: 0644}
	}

	var buf bytes.Buffer
	writer := wapp.NewWriter()
	require.NoError(t, writer.PackWithResources(
		wapp.Metadata{}, nil,
		[]wapp.ResourceSpec{{ID: wapp.NewID(ns, name), FS: mapFS}},
		&buf,
	))

	path := filepath.Join(t.TempDir(), "pack.wapp")
	require.NoError(t, os.WriteFile(path, buf.Bytes(), 0644))
	file, err := os.Open(path)
	require.NoError(t, err)
	reader, err := wapp.NewReader(file)
	require.NoError(t, err)
	return reader, file
}

func packEntriesOnly(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	writer := wapp.NewWriter()
	require.NoError(t, writer.PackEntries(wapp.Metadata{}, nil, &buf))
	return buf.Bytes()
}

func readerFromBytes(t *testing.T, data []byte) *wapp.Reader {
	t.Helper()

	wappPath := filepath.Join(t.TempDir(), "pack.wapp")
	require.NoError(t, os.WriteFile(wappPath, data, 0644))

	file, err := os.Open(wappPath)
	require.NoError(t, err)
	t.Cleanup(func() { file.Close() })

	reader, err := wapp.NewReader(file)
	require.NoError(t, err)
	return reader
}
