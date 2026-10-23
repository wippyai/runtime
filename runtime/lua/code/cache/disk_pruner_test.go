// SPDX-License-Identifier: MPL-2.0

package cache

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func observePruneScan(t *testing.T, store *DiskStore, after func(string)) {
	t.Helper()
	scan := store.pruner.scan
	replaceSeam(t, &store.pruner.scan, func(path string) (diskEntryInfo, error) {
		info, err := scan(path)
		after(path)
		return info, err
	})
}

func TestDiskStorePruneReportsUnknownByteUsage(t *testing.T) {
	store := NewBoundedDiskStore(t.TempDir(), 1, 100, 1000)
	putTestEntry(t, store, "unreadable", time.Unix(1, 0))
	replaceSeam(t, &store.pruner.scan, func(string) (diskEntryInfo, error) {
		return diskEntryInfo{}, fs.ErrPermission
	})

	err := store.Prune()
	require.ErrorIs(t, err, errIncompleteScan)
	require.ErrorIs(t, err, fs.ErrPermission, "unknown bytes must not be reported as a successful cleanup")
	assert.DirExists(t, store.entryDir("unreadable"))
}

func TestDiskStorePruneInterval(t *testing.T) {
	for _, tc := range []struct {
		name     string
		interval int
		want     int
	}{
		{name: "default", want: 512},
		{name: "custom", interval: 1000, want: 1000},
		{name: "every write", interval: 1, want: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := NewBoundedDiskStore(t.TempDir(), 1<<20, 100, tc.interval)
			var scans atomic.Int32
			observePruneScan(t, store, func(string) { scans.Add(1) })
			t.Cleanup(store.WaitPrune)
			require.NoError(t, store.Put("entry", nil))
			require.Error(t, store.Put("\x00", &Entry{}))
			for range tc.want - 1 {
				putTestEntry(t, store, "entry", time.Unix(1, 0))
			}
			store.WaitPrune()
			require.Zero(t, scans.Load(), "cleanup started before the configured number of successful writes")
			putTestEntry(t, store, "entry", time.Unix(1, 0))
			store.WaitPrune()
			require.Equal(t, int32(1), scans.Load(), "cleanup did not run at the configured interval")
		})
	}
}

func TestDiskStoreUnknownByteUsageReachesBackgroundHandler(t *testing.T) {
	store := NewBoundedDiskStore(t.TempDir(), 1, 100, 2)
	putTestEntry(t, store, "unreadable", time.Unix(1, 0))
	unreadable := store.entryDir("unreadable")
	replaceSeam(t, &store.pruner.scan, func(path string) (diskEntryInfo, error) {
		if path == unreadable {
			return diskEntryInfo{}, fs.ErrPermission
		}
		return scanEntry(path)
	})
	reported := make(chan error, 1)
	store.SetPruneErrorHandler(func(err error) { reported <- err })
	t.Cleanup(store.WaitPrune)
	putTestEntry(t, store, "readable", time.Unix(2, 0))
	store.WaitPrune()

	select {
	case err := <-reported:
		require.ErrorIs(t, err, errIncompleteScan)
		require.ErrorIs(t, err, fs.ErrPermission)
	default:
		t.Fatal("unknown byte usage was not reported to the background error handler")
	}
	assert.DirExists(t, unreadable)
	assert.NoDirExists(t, store.entryDir("readable"), "readable entries should still be evicted")
}

func putTestEntry(t *testing.T, store *DiskStore, key string, created time.Time) {
	t.Helper()
	require.NoError(t, store.Put(key, &Entry{
		Meta:  Meta{EntryID: key, SourceHash: key, CreatedAt: created},
		Proto: []byte(key),
	}))
}

func entryExists(t *testing.T, store *DiskStore, key string) bool {
	t.Helper()
	_, ok, err := store.Get(key)
	require.NoError(t, err)
	return ok
}

// replaceSeam swaps a store operation and restores it during cleanup.
// Register it before any cleanup that waits for background prunes, so the
// prunes finish before the seam is restored.
func replaceSeam[T any](t *testing.T, seam *T, value T) {
	t.Helper()
	previous := *seam
	*seam = value
	t.Cleanup(func() { *seam = previous })
}

func TestDiskStorePrunesOldestGenerationByEntryLimit(t *testing.T) {
	store := NewBoundedDiskStore(t.TempDir(), 1<<20, 2, 1)
	t.Cleanup(store.WaitPrune)
	base := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	for i, key := range []string{"old", "middle", "new"} {
		require.NoError(t, store.Put(key, &Entry{
			Meta:  Meta{EntryID: key, SourceHash: key, CreatedAt: base.Add(time.Duration(i) * time.Hour)},
			Proto: []byte(key),
		}))
	}
	store.WaitPrune()

	_, oldExists, err := store.Get("old")
	require.NoError(t, err)
	assert.False(t, oldExists)
	for _, key := range []string{"middle", "new"} {
		_, exists, getErr := store.Get(key)
		require.NoError(t, getErr)
		assert.True(t, exists)
	}
}

func TestDiskStorePrunesToByteLimit(t *testing.T) {
	store := NewBoundedDiskStore(t.TempDir(), 1200, 10, 1)
	t.Cleanup(store.WaitPrune)
	require.NoError(t, store.Put("old", &Entry{
		Meta:  Meta{EntryID: "old", SourceHash: "old", CreatedAt: time.Unix(1, 0)},
		Proto: make([]byte, 512),
	}))
	require.NoError(t, store.Put("new", &Entry{
		Meta:  Meta{EntryID: "new", SourceHash: "new", CreatedAt: time.Unix(2, 0)},
		Proto: make([]byte, 512),
	}))
	store.WaitPrune()

	_, oldExists, err := store.Get("old")
	require.NoError(t, err)
	assert.False(t, oldExists)
	_, newExists, err := store.Get("new")
	require.NoError(t, err)
	assert.True(t, newExists)
}

func TestDiskStorePutGetAndDeleteDoNotWaitForPruneScan(t *testing.T) {
	store := NewBoundedDiskStore(t.TempDir(), 1<<20, 1, 2)
	putTestEntry(t, store, "old", time.Unix(1, 0))

	scanning := make(chan struct{})
	release := make(chan struct{})
	var scanningOnce, releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	// Hold the scan after it has read its first entry.
	observePruneScan(t, store, func(string) {
		scanningOnce.Do(func() { close(scanning) })
		<-release
	})
	var workers sync.WaitGroup
	t.Cleanup(func() {
		unblock()
		workers.Wait()
		store.WaitPrune()
	})

	// finishes runs op and fails the test if op is still running after the
	// timeout, which means it waited for the held scan.
	finishes := func(name string, op func() error) {
		t.Helper()
		done := make(chan error, 1)
		workers.Add(1)
		go func() {
			defer workers.Done()
			done <- op()
		}()
		select {
		case err := <-done:
			require.NoError(t, err, name)
		case <-time.After(5 * time.Second):
			t.Fatalf("%s waited for the prune scan", name)
		}
	}

	// The second write reaches the prune interval and starts the held prune.
	finishes("Put that starts the prune", func() error {
		return store.Put("new", &Entry{
			Meta:  Meta{EntryID: "new", SourceHash: "new", CreatedAt: time.Unix(2, 0)},
			Proto: []byte("new"),
		})
	})
	select {
	case <-scanning:
	case <-time.After(5 * time.Second):
		t.Fatal("the prune interval did not start a prune")
	}
	finishes("Get", func() error {
		_, ok, err := store.Get("new")
		if err == nil && !ok {
			return errors.New("entry new is missing")
		}
		return err
	})
	finishes("Put", func() error {
		return store.Put("extra", &Entry{Meta: Meta{EntryID: "extra", SourceHash: "extra"}, Proto: []byte("extra")})
	})
	finishes("Delete", func() error { return store.Delete("extra") })

	unblock()
	store.WaitPrune()

	assert.False(t, entryExists(t, store, "old"), "the background prune still evicts the oldest generation")
	assert.True(t, entryExists(t, store, "new"))
}

func TestDiskStoreBackgroundPruneRunsAgainForWritesDuringScan(t *testing.T) {
	store := NewBoundedDiskStore(t.TempDir(), 1<<20, 100, 1)
	scanning := make(chan struct{})
	release := make(chan struct{})
	var scanningOnce, releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	var mu sync.Mutex
	var scanned []string
	observePruneScan(t, store, func(path string) {
		mu.Lock()
		scanned = append(scanned, filepath.Base(path))
		mu.Unlock()
		scanningOnce.Do(func() { close(scanning) })
		<-release
	})
	t.Cleanup(func() {
		unblock()
		store.WaitPrune()
	})

	putTestEntry(t, store, "a", time.Unix(1, 0))
	select {
	case <-scanning:
	case <-time.After(5 * time.Second):
		t.Fatal("the prune interval did not start a prune")
	}
	// The held scan listed its entries before this write.
	putTestEntry(t, store, "b", time.Unix(2, 0))
	unblock()
	store.WaitPrune()

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []string{"a", "a", "b"}, scanned, "a second pass counts the write made during the first")
}

func TestDiskStorePruneKeepsEntryBeingWritten(t *testing.T) {
	store := NewBoundedDiskStore(t.TempDir(), 1<<20, 2, 1000)
	putTestEntry(t, store, "old", time.Unix(1, 0))
	putTestEntry(t, store, "middle", time.Unix(2, 0))
	// A Put in another process sharing the directory has created this entry
	// and written only a temp file.
	writing := store.entryDir("new")
	require.NoError(t, os.MkdirAll(writing, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(writing, protoFile+".tmp-1"), []byte("new"), 0o644))

	require.NoError(t, store.Prune())

	assert.DirExists(t, writing, "an entry that is still being written is not the oldest")
	assert.False(t, entryExists(t, store, "old"))
	assert.True(t, entryExists(t, store, "middle"))
}

func TestDiskStorePruneKeepsEntryRewrittenAfterScan(t *testing.T) {
	store := NewBoundedDiskStore(t.TempDir(), 1<<20, 1, 1000)
	putTestEntry(t, store, "middle", time.Unix(2, 0))
	putTestEntry(t, store, "old", time.Unix(1, 0))
	oldDir := store.entryDir("old")
	observePruneScan(t, store, func(path string) {
		if path != oldDir {
			return
		}
		// Rewritten after the scan read it, so it is no longer the oldest.
		require.NoError(t, store.Put("old", &Entry{
			Meta:  Meta{EntryID: "old", SourceHash: "rewritten"},
			Proto: []byte("rewritten"),
		}))
	})

	require.NoError(t, store.Prune())

	got, ok, err := store.Get("old")
	require.NoError(t, err)
	require.True(t, ok, "the rewritten entry is kept")
	assert.Equal(t, []byte("rewritten"), got.Proto)
	assert.False(t, entryExists(t, store, "middle"))
}

func TestDiskStorePruneKeepsEntryRewrittenWithSameCreatedAt(t *testing.T) {
	store := NewBoundedDiskStore(t.TempDir(), 1<<20, 1, 1000)
	putTestEntry(t, store, "middle", time.Unix(2, 0))
	putTestEntry(t, store, "old", time.Unix(1, 0))
	oldDir := store.entryDir("old")
	observePruneScan(t, store, func(path string) {
		if path != oldDir {
			return
		}
		// New content, but the caller keeps the original creation time.
		require.NoError(t, store.Put("old", &Entry{
			Meta:  Meta{EntryID: "old", SourceHash: "rewritten", CreatedAt: time.Unix(1, 0)},
			Proto: []byte("rewritten"),
		}))
	})

	require.NoError(t, store.Prune())

	got, ok, err := store.Get("old")
	require.NoError(t, err)
	require.True(t, ok, "the rewritten entry is kept")
	assert.Equal(t, []byte("rewritten"), got.Proto)
	assert.False(t, entryExists(t, store, "middle"))
}

func TestDiskStorePruneReportsOverLimitsWhenEveryVictimChanged(t *testing.T) {
	store := NewBoundedDiskStore(t.TempDir(), 1<<20, 1, 1000)
	putTestEntry(t, store, "a", time.Unix(1, 0))
	putTestEntry(t, store, "b", time.Unix(2, 0))
	observePruneScan(t, store, func(path string) {
		key := filepath.Base(path)
		require.NoError(t, store.Put(key, &Entry{Meta: Meta{EntryID: key, SourceHash: "rewritten"}, Proto: []byte("rewritten")}))
	})

	require.ErrorIs(t, store.Prune(), errOverLimits)

	assert.True(t, entryExists(t, store, "a"))
	assert.True(t, entryExists(t, store, "b"))
}

func TestDiskStorePruneSkipsEntryItCannotScan(t *testing.T) {
	store := NewBoundedDiskStore(t.TempDir(), 1<<20, 2, 1000)
	putTestEntry(t, store, "a", time.Unix(1, 0))
	putTestEntry(t, store, "b", time.Unix(2, 0))
	putTestEntry(t, store, "c", time.Unix(3, 0))
	unreadable := store.entryDir("a")
	replaceSeam(t, &store.pruner.scan, func(root string) (diskEntryInfo, error) {
		if root == unreadable {
			return diskEntryInfo{}, fs.ErrPermission
		}
		return scanEntry(root)
	})

	err := store.Prune()
	require.ErrorIs(t, err, errIncompleteScan)
	require.ErrorIs(t, err, fs.ErrPermission)

	assert.True(t, entryExists(t, store, "a"), "the unreadable entry stays and still counts")
	assert.False(t, entryExists(t, store, "b"))
	assert.True(t, entryExists(t, store, "c"))
}

func TestDiskStorePruneEvictsEntryWithUnreadableMetadata(t *testing.T) {
	store := NewBoundedDiskStore(t.TempDir(), 512, 10, 1000)
	require.NoError(t, store.Put("big", &Entry{
		Meta:  Meta{EntryID: "big", SourceHash: "big", CreatedAt: time.Unix(1, 0)},
		Proto: make([]byte, 1024),
	}))
	// A directory in place of meta.json makes reading it fail with an error
	// other than "not found" on every platform.
	metaPath := filepath.Join(store.entryDir("big"), metaFile)
	require.NoError(t, os.Remove(metaPath))
	require.NoError(t, os.Mkdir(metaPath, 0o755))

	require.NoError(t, store.Prune(), "the entry's bytes still count, so it is evicted")

	assert.NoDirExists(t, store.entryDir("big"))
}

func TestDiskStorePruneReportsUnreadableEntriesThatKeepItOverLimits(t *testing.T) {
	store := NewBoundedDiskStore(t.TempDir(), 1<<20, 1, 1000)
	putTestEntry(t, store, "a", time.Unix(1, 0))
	putTestEntry(t, store, "b", time.Unix(2, 0))
	putTestEntry(t, store, "c", time.Unix(3, 0))
	unreadable := map[string]bool{store.entryDir("a"): true, store.entryDir("b"): true}
	replaceSeam(t, &store.pruner.scan, func(root string) (diskEntryInfo, error) {
		if unreadable[root] {
			return diskEntryInfo{}, fs.ErrPermission
		}
		return scanEntry(root)
	})

	err := store.Prune()

	require.ErrorIs(t, err, errOverLimits)
	require.ErrorIs(t, err, fs.ErrPermission)
	assert.False(t, entryExists(t, store, "c"), "the readable entry is still removed")
}

func TestDiskStorePruneSkipsEntryItCannotRemove(t *testing.T) {
	store := NewBoundedDiskStore(t.TempDir(), 1<<20, 2, 1000)
	putTestEntry(t, store, "a", time.Unix(1, 0))
	putTestEntry(t, store, "b", time.Unix(2, 0))
	putTestEntry(t, store, "c", time.Unix(3, 0))
	stuck := store.entryDir("a")
	replaceSeam(t, &store.pruner.removeAll, func(path string) error {
		if path == stuck {
			return fs.ErrPermission
		}
		return os.RemoveAll(path)
	})

	require.NoError(t, store.Prune(), "the limits are met by removing the next oldest entry")

	assert.True(t, entryExists(t, store, "a"))
	assert.False(t, entryExists(t, store, "b"))
	assert.True(t, entryExists(t, store, "c"))
}

func TestDiskStorePruneReportsErrorWhenLimitsAreStillExceeded(t *testing.T) {
	store := NewBoundedDiskStore(t.TempDir(), 1<<20, 1, 1000)
	putTestEntry(t, store, "a", time.Unix(1, 0))
	putTestEntry(t, store, "b", time.Unix(2, 0))
	injected := errors.New("injected removal failure")
	replaceSeam(t, &store.pruner.removeAll, func(string) error { return injected })

	err := store.Prune()

	require.ErrorIs(t, err, errOverLimits)
	require.ErrorIs(t, err, injected)

	assert.True(t, entryExists(t, store, "a"))
	assert.True(t, entryExists(t, store, "b"))
}

func TestDiskStoreBackgroundPruneErrorReachesHandler(t *testing.T) {
	store := NewBoundedDiskStore(t.TempDir(), 1<<20, 1, 2)
	injected := errors.New("injected removal failure")
	replaceSeam(t, &store.pruner.removeAll, func(string) error { return injected })
	reported := make(chan error, 4)
	store.SetPruneErrorHandler(func(err error) { reported <- err })
	t.Cleanup(store.WaitPrune)

	putTestEntry(t, store, "a", time.Unix(1, 0))
	putTestEntry(t, store, "b", time.Unix(2, 0))
	store.WaitPrune()

	select {
	case err := <-reported:
		require.ErrorIs(t, err, injected)
	default:
		t.Fatal("the background prune error did not reach the handler")
	}
}
