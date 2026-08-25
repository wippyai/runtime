// SPDX-License-Identifier: MPL-2.0

package embed

import (
	"bytes"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

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

	t.Run("overwrites existing reader and closes old file", func(t *testing.T) {
		r := NewRegistry()
		reader1, file1 := createTestReaderWithFile(t)
		reader2 := createTestReader(t)

		require.NoError(t, r.Register("test/pack", reader1, file1))
		require.NoError(t, r.Register("test/pack", reader2, nil))

		r.mu.RLock()
		assert.Len(t, r.packs, 1)
		assert.Equal(t, reader2, r.packs["test/pack"].reader)
		r.mu.RUnlock()

		// The replaced file handle must be closed.
		_, err := file1.Read(make([]byte, 1))
		require.Error(t, err)
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

func TestRegistry_GetFSForEntry(t *testing.T) {
	t.Run("selects pack matching operation module and version", func(t *testing.T) {
		r := NewRegistry()
		// Two pack versions expose the SAME resource ID with different content.
		oldReader := createReaderWithResource(t, "ui", "app", map[string]string{"v.txt": "old"})
		newReader := createReaderWithResource(t, "ui", "app", map[string]string{"v.txt": "new"})
		require.NoError(t, r.RegisterPack("org/mod-v1.0.0.wapp", "org/mod", "1.0.0", oldReader, nil))
		require.NoError(t, r.RegisterPack("org/mod-v2.0.0.wapp", "org/mod", "2.0.0", newReader, nil))

		fsysNew, err := r.GetFSForEntry(packEntry("ui", "app"), moduleProvenance("org/mod", "2.0.0"))
		require.NoError(t, err)
		data, err := fs.ReadFile(fsysNew, "v.txt")
		require.NoError(t, err)
		assert.Equal(t, "new", string(data))

		fsysOld, err := r.GetFSForEntry(packEntry("ui", "app"), moduleProvenance("org/mod", "1.0.0"))
		require.NoError(t, err)
		data, err = fs.ReadFile(fsysOld, "v.txt")
		require.NoError(t, err)
		assert.Equal(t, "old", string(data))
	})

	t.Run("matches by module when version omitted", func(t *testing.T) {
		r := NewRegistry()
		reader := createReaderWithResource(t, "ui", "app", map[string]string{"v.txt": "only"})
		require.NoError(t, r.RegisterPack("org/mod-v1.0.0.wapp", "org/mod", "1.0.0", reader, nil))

		fsys, err := r.GetFSForEntry(packEntry("ui", "app"), moduleProvenance("org/mod", ""))
		require.NoError(t, err)
		data, err := fs.ReadFile(fsys, "v.txt")
		require.NoError(t, err)
		assert.Equal(t, "only", string(data))
	})

	t.Run("falls back to GetFS without provenance", func(t *testing.T) {
		r := NewRegistry()
		reader := createReaderWithResource(t, "ui", "app", map[string]string{"v.txt": "legacy"})
		require.NoError(t, r.RegisterPack("legacy.wapp", "", "", reader, nil))

		fsys, err := r.GetFSForEntry(packEntry("ui", "app"), nil)
		require.NoError(t, err)
		data, err := fs.ReadFile(fsys, "v.txt")
		require.NoError(t, err)
		assert.Equal(t, "legacy", string(data))
	})

	t.Run("falls back to GetFS for a host-authored entry", func(t *testing.T) {
		r := NewRegistry()
		reader := createReaderWithResource(t, "ui", "app", map[string]string{"v.txt": "host"})
		require.NoError(t, r.RegisterPack("app.wapp", "", "", reader, nil))

		fsys, err := r.GetFSForEntry(packEntry("ui", "app"), &registry.EntryProvenance{})
		require.NoError(t, err)
		data, err := fs.ReadFile(fsys, "v.txt")
		require.NoError(t, err)
		assert.Equal(t, "host", string(data))
	})

	t.Run("does not fall back to another pack when versioned owner is missing", func(t *testing.T) {
		r := NewRegistry()
		oldReader := createReaderWithResource(t, "ui", "app", map[string]string{"v.txt": "old"})
		otherReader := createReaderWithResource(t, "ui", "app", map[string]string{"v.txt": "other"})
		require.NoError(t, r.RegisterPack("org/mod-v1.0.0.wapp", "org/mod", "1.0.0", oldReader, nil))
		require.NoError(t, r.RegisterPack("org/other-v9.9.9.wapp", "org/other", "9.9.9", otherReader, nil))

		_, err := r.GetFSForEntry(packEntry("ui", "app"), moduleProvenance("org/mod", "2.0.0"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("falls back to GetFS when module version is omitted", func(t *testing.T) {
		r := NewRegistry()
		reader := createReaderWithResource(t, "ui", "app", map[string]string{"v.txt": "legacy"})
		require.NoError(t, r.RegisterPack("legacy.wapp", "", "", reader, nil))

		fsys, err := r.GetFSForEntry(packEntry("ui", "app"), moduleProvenance("org/mod", ""))
		require.NoError(t, err)
		data, err := fs.ReadFile(fsys, "v.txt")
		require.NoError(t, err)
		assert.Equal(t, "legacy", string(data))
	})
}

func TestRegistry_RetargetModule(t *testing.T) {
	t.Run("serves the new pack after the old one closes", func(t *testing.T) {
		r := NewRegistry()
		oldReader, oldFile := createReaderWithResourceFile(t, "ui", "app", map[string]string{"v.txt": "old"})
		newReader := createReaderWithResource(t, "ui", "app", map[string]string{"v.txt": "new"})
		require.NoError(t, r.RegisterPack("org/mod-v1.0.0.wapp", "org/mod", "1.0.0", oldReader, oldFile))
		require.NoError(t, r.RegisterPack("org/mod-v2.0.0.wapp", "org/mod", "2.0.0", newReader, nil))

		// The entry received no event on the version bump: the consumer still
		// holds the filesystem resolved from the 1.0.0 pack.
		cached, err := r.GetFSForEntry(packEntry("ui", "app"), moduleProvenance("org/mod", "1.0.0"))
		require.NoError(t, err)
		assert.Equal(t, "old", readFile(t, cached, "v.txt"))

		require.NoError(t, r.RetargetModule("org/mod", "1.0.0", "2.0.0"))
		assert.Equal(t, "new", readFile(t, cached, "v.txt"))

		// Finalize retires the old pack; the cached filesystem keeps reading.
		require.NoError(t, r.UnregisterModule("org/mod", "1.0.0"))
		assert.Equal(t, "new", readFile(t, cached, "v.txt"))
	})

	t.Run("concurrent reads survive the old pack closing", func(t *testing.T) {
		r := NewRegistry()
		oldReader, oldFile := createReaderWithResourceFile(t, "ui", "app", map[string]string{"v.txt": "old"})
		newReader := createReaderWithResource(t, "ui", "app", map[string]string{"v.txt": "new"})
		require.NoError(t, r.RegisterPack("org/mod-v1.0.0.wapp", "org/mod", "1.0.0", oldReader, oldFile))
		require.NoError(t, r.RegisterPack("org/mod-v2.0.0.wapp", "org/mod", "2.0.0", newReader, nil))

		cached, err := r.GetFSForEntry(packEntry("ui", "app"), moduleProvenance("org/mod", "1.0.0"))
		require.NoError(t, err)

		var (
			wg     sync.WaitGroup
			stop   = make(chan struct{})
			failed = make(chan error, 64)
		)
		reportFailure := func(err error) {
			select {
			case failed <- err:
			default:
			}
		}
		// Half the readers hold an open handle across the retarget and the
		// close, which is the window a per-call reference would miss.
		for i := 0; i < 8; i++ {
			holdOpen := i%2 == 0
			wg.Add(1)
			go func() {
				defer wg.Done()
				for {
					select {
					case <-stop:
						return
					default:
					}
					if !holdOpen {
						if _, err := fs.ReadFile(cached, "v.txt"); err != nil {
							reportFailure(err)
							return
						}
						continue
					}
					file, err := cached.Open("v.txt")
					if err != nil {
						reportFailure(err)
						return
					}
					runtime.Gosched()
					if _, err := io.ReadAll(file); err != nil {
						_ = file.Close()
						reportFailure(err)
						return
					}
					if err := file.Close(); err != nil {
						reportFailure(err)
						return
					}
				}
			}()
		}

		require.NoError(t, r.RetargetModule("org/mod", "1.0.0", "2.0.0"))
		require.NoError(t, r.UnregisterModule("org/mod", "1.0.0"))
		// Reads after the close must come from the new pack.
		for i := 0; i < 32; i++ {
			assert.Equal(t, "new", readFile(t, cached, "v.txt"))
		}
		close(stop)

		// Readers must drain on their own: nothing in the retarget or the close
		// waits on them, so nothing can hold them.
		done := make(chan struct{})
		go func() {
			wg.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Fatal("readers did not finish: a read is blocked on the pack lifecycle")
		}

		close(failed)
		for err := range failed {
			t.Fatalf("read failed while the pack was retargeted: %v", err)
		}
	})

	t.Run("missing target pack fails and keeps the current pack", func(t *testing.T) {
		r := NewRegistry()
		reader := createReaderWithResource(t, "ui", "app", map[string]string{"v.txt": "old"})
		require.NoError(t, r.RegisterPack("org/mod-v1.0.0.wapp", "org/mod", "1.0.0", reader, nil))

		cached, err := r.GetFSForEntry(packEntry("ui", "app"), moduleProvenance("org/mod", "1.0.0"))
		require.NoError(t, err)

		err = r.RetargetModule("org/mod", "1.0.0", "2.0.0")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
		assert.Equal(t, "old", readFile(t, cached, "v.txt"))
	})

	t.Run("entry missing from the new pack retargets nothing", func(t *testing.T) {
		r := NewRegistry()
		oldReader := createReaderWithResources(t, "org/mod", map[string]map[string]string{
			"ui:app":  {"v.txt": "old"},
			"ui:docs": {"v.txt": "old"},
		})
		newReader := createReaderWithResource(t, "ui", "app", map[string]string{"v.txt": "new"})
		require.NoError(t, r.RegisterPack("org/mod-v1.0.0.wapp", "org/mod", "1.0.0", oldReader, nil))
		require.NoError(t, r.RegisterPack("org/mod-v2.0.0.wapp", "org/mod", "2.0.0", newReader, nil))

		app, err := r.GetFSForEntry(packEntry("ui", "app"), moduleProvenance("org/mod", "1.0.0"))
		require.NoError(t, err)
		docs, err := r.GetFSForEntry(packEntry("ui", "docs"), moduleProvenance("org/mod", "1.0.0"))
		require.NoError(t, err)

		err = r.RetargetModule("org/mod", "1.0.0", "2.0.0")
		require.Error(t, err)
		assert.Equal(t, "old", readFile(t, app, "v.txt"))
		assert.Equal(t, "old", readFile(t, docs, "v.txt"))
	})

	t.Run("nothing served from the version is a no-op", func(t *testing.T) {
		r := NewRegistry()
		require.NoError(t, r.RetargetModule("org/mod", "1.0.0", "2.0.0"))
	})

	t.Run("same version is a no-op", func(t *testing.T) {
		r := NewRegistry()
		reader := createReaderWithResource(t, "ui", "app", map[string]string{"v.txt": "old"})
		require.NoError(t, r.RegisterPack("org/mod-v1.0.0.wapp", "org/mod", "1.0.0", reader, nil))
		_, err := r.GetFSForEntry(packEntry("ui", "app"), moduleProvenance("org/mod", "1.0.0"))
		require.NoError(t, err)

		require.NoError(t, r.RetargetModule("org/mod", "1.0.0", "1.0.0"))
	})

	t.Run("empty module is rejected", func(t *testing.T) {
		r := NewRegistry()
		err := r.RetargetModule("", "1.0.0", "2.0.0")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "module cannot be empty")
	})

	t.Run("one store repoints every entry of the module version", func(t *testing.T) {
		r := NewRegistry()
		oldReader := createReaderWithResources(t, "org/mod", map[string]map[string]string{
			"ui:app":  {"v.txt": "old"},
			"ui:docs": {"v.txt": "old"},
		})
		newReader := createReaderWithResources(t, "org/mod", map[string]map[string]string{
			"ui:app":  {"v.txt": "new"},
			"ui:docs": {"v.txt": "new"},
		})
		require.NoError(t, r.RegisterPack("org/mod-v1.0.0.wapp", "org/mod", "1.0.0", oldReader, nil))
		require.NoError(t, r.RegisterPack("org/mod-v2.0.0.wapp", "org/mod", "2.0.0", newReader, nil))

		app, err := r.GetFSForEntry(packEntry("ui", "app"), moduleProvenance("org/mod", "1.0.0"))
		require.NoError(t, err)
		docs, err := r.GetFSForEntry(packEntry("ui", "docs"), moduleProvenance("org/mod", "1.0.0"))
		require.NoError(t, err)

		// Both entries read through the same generation, so the retarget is one
		// store rather than one store per filesystem.
		assert.Same(t, app.(*entryFS).lineage, docs.(*entryFS).lineage)

		require.NoError(t, r.RetargetModule("org/mod", "1.0.0", "2.0.0"))
		assert.Equal(t, "new", readFile(t, app, "v.txt"))
		assert.Equal(t, "new", readFile(t, docs, "v.txt"))
	})

	t.Run("a read in flight keeps its pack open past the close", func(t *testing.T) {
		r := NewRegistry()
		oldReader, oldFile := createReaderWithResourceFile(t, "ui", "app", map[string]string{"v.txt": "old"})
		newReader := createReaderWithResource(t, "ui", "app", map[string]string{"v.txt": "new"})
		require.NoError(t, r.RegisterPack("org/mod-v1.0.0.wapp", "org/mod", "1.0.0", oldReader, oldFile))
		require.NoError(t, r.RegisterPack("org/mod-v2.0.0.wapp", "org/mod", "2.0.0", newReader, nil))

		cached, err := r.GetFSForEntry(packEntry("ui", "app"), moduleProvenance("org/mod", "1.0.0"))
		require.NoError(t, err)

		inFlight, err := cached.Open("v.txt")
		require.NoError(t, err)

		require.NoError(t, r.RetargetModule("org/mod", "1.0.0", "2.0.0"))
		require.NoError(t, r.UnregisterModule("org/mod", "1.0.0"))

		// The handle opened before the retarget still reads its own pack.
		data, err := io.ReadAll(inFlight)
		require.NoError(t, err)
		assert.Equal(t, "old", string(data))
		_, err = oldFile.ReadAt(make([]byte, 1), 0)
		require.NoError(t, err, "the pack file must stay open while a read holds it")

		// Closing the last reader closes the pack file.
		require.NoError(t, inFlight.Close())
		_, err = oldFile.ReadAt(make([]byte, 1), 0)
		require.Error(t, err)

		// New reads come from the pack the lineage was retargeted to.
		assert.Equal(t, "new", readFile(t, cached, "v.txt"))
	})

	t.Run("retired generations are not referenced by the registry", func(t *testing.T) {
		r := NewRegistry()
		oldReader, oldFile := createReaderWithResourceFile(t, "ui", "app", map[string]string{"v.txt": "old"})
		newReader := createReaderWithResource(t, "ui", "app", map[string]string{"v.txt": "new"})
		require.NoError(t, r.RegisterPack("org/mod-v1.0.0.wapp", "org/mod", "1.0.0", oldReader, oldFile))
		require.NoError(t, r.RegisterPack("org/mod-v2.0.0.wapp", "org/mod", "2.0.0", newReader, nil))

		cached, err := r.GetFSForEntry(packEntry("ui", "app"), moduleProvenance("org/mod", "1.0.0"))
		require.NoError(t, err)
		assert.Equal(t, "old", readFile(t, cached, "v.txt"))

		require.NoError(t, r.RetargetModule("org/mod", "1.0.0", "2.0.0"))
		require.NoError(t, r.UnregisterModule("org/mod", "1.0.0"))

		r.mu.RLock()
		defer r.mu.RUnlock()
		for p, lineages := range r.lineages {
			assert.Equal(t, "2.0.0", p.version, "no lineage may stay keyed by the retired pack")
			for _, l := range lineages {
				require.NotNil(t, l.current.Load())
				assert.Equal(t, "2.0.0", l.current.Load().version())
			}
		}
		// Nothing holds the retired pack: its handle is closed.
		_, err = oldFile.ReadAt(make([]byte, 1), 0)
		require.Error(t, err)
	})

	t.Run("unregistering the served pack stops resolution", func(t *testing.T) {
		r := NewRegistry()
		reader := createReaderWithResource(t, "ui", "app", map[string]string{"v.txt": "old"})
		require.NoError(t, r.RegisterPack("org/mod-v1.0.0.wapp", "org/mod", "1.0.0", reader, nil))
		cached, err := r.GetFSForEntry(packEntry("ui", "app"), moduleProvenance("org/mod", "1.0.0"))
		require.NoError(t, err)

		require.NoError(t, r.UnregisterModule("org/mod", "1.0.0"))

		_, err = cached.Open("v.txt")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")

		r.mu.RLock()
		lineages := len(r.lineages)
		r.mu.RUnlock()
		assert.Equal(t, 0, lineages)
	})
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

// packEntry builds the registry entry a pack resource belongs to. Ownership
// rides the operation provenance, never the entry.
func packEntry(ns, name string) registry.Entry {
	return registry.Entry{ID: registry.NewID(ns, name)}
}

func moduleProvenance(module, version string) *registry.EntryProvenance {
	return &registry.EntryProvenance{Module: module, Version: version}
}

func readFile(t *testing.T, fsys fs.ReadDirFS, name string) string {
	t.Helper()

	data, err := fs.ReadFile(fsys, name)
	require.NoError(t, err)
	return string(data)
}

// createReaderWithResource builds a wapp containing a single filesystem resource.
func createReaderWithResource(t *testing.T, ns, name string, files map[string]string) *wapp.Reader {
	t.Helper()
	return readerFromBytes(t, packResources(t, []wapp.ResourceSpec{{ID: wapp.NewID(ns, name), FS: resourceFS(files)}}))
}

// createReaderWithResourceFile builds a single-resource wapp on disk and returns
// the open file handle backing it, so closing the pack breaks its reads.
func createReaderWithResourceFile(t *testing.T, ns, name string, files map[string]string) (*wapp.Reader, *os.File) {
	t.Helper()

	data := packResources(t, []wapp.ResourceSpec{{ID: wapp.NewID(ns, name), FS: resourceFS(files)}})
	wappPath := filepath.Join(t.TempDir(), "pack.wapp")
	require.NoError(t, os.WriteFile(wappPath, data, 0644))

	file, err := os.Open(wappPath)
	require.NoError(t, err)

	reader, err := wapp.NewReader(file)
	require.NoError(t, err)
	return reader, file
}

// createReaderWithResources builds a wapp exposing several resources, keyed
// "namespace:name".
func createReaderWithResources(t *testing.T, _ string, resources map[string]map[string]string) *wapp.Reader {
	t.Helper()

	specs := make([]wapp.ResourceSpec, 0, len(resources))
	for id, files := range resources {
		ns, name, ok := strings.Cut(id, ":")
		require.True(t, ok, "resource id %q must be namespace:name", id)
		specs = append(specs, wapp.ResourceSpec{ID: wapp.NewID(ns, name), FS: resourceFS(files)})
	}
	return readerFromBytes(t, packResources(t, specs))
}

func resourceFS(files map[string]string) fstest.MapFS {
	mapFS := fstest.MapFS{}
	for path, content := range files {
		mapFS[path] = &fstest.MapFile{Data: []byte(content), Mode: 0644}
	}
	return mapFS
}

func packResources(t *testing.T, specs []wapp.ResourceSpec) []byte {
	t.Helper()

	var buf bytes.Buffer
	writer := wapp.NewWriter()
	require.NoError(t, writer.PackWithResources(wapp.Metadata{}, nil, specs, &buf))
	return buf.Bytes()
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
