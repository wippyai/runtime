// SPDX-License-Identifier: MPL-2.0

package cache

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/go-lua/types/diag"
	"github.com/wippyai/go-lua/types/io"
)

func TestDiskStorePutGet(t *testing.T) {
	dir := t.TempDir()
	store := NewDiskStore(dir)

	fixed := time.Date(2024, time.January, 2, 3, 4, 5, 0, time.UTC)
	previousNow := nowUTC
	nowUTC = func() time.Time { return fixed }
	t.Cleanup(func() { nowUTC = previousNow })

	manifest := &io.Manifest{Path: "lib.math", Version: 1}
	manifestBytes, err := manifest.Encode()
	require.NoError(t, err)

	diags := []diag.Diagnostic{{
		Code:     42,
		Severity: diag.SeverityError,
		Message:  "broken",
		Position: diag.Position{Line: 1, Column: 2},
	}}

	entry := &Entry{
		Meta: Meta{
			EntryID:              "app/main",
			Kind:                 "function.lua",
			Method:               "main",
			SourceHash:           "abc",
			CompileFingerprint:   "compile-fp",
			TypecheckFingerprint: "type-fp",
		},
		Manifest:    manifestBytes,
		Diagnostics: diags,
		Proto:       []byte{0x01, 0x02, 0x03},
	}

	require.NoError(t, store.Put("key", entry))

	got, ok, err := store.Get("key")
	require.NoError(t, err)
	require.True(t, ok)
	require.NotNil(t, got)

	assert.Equal(t, SchemaVersion, got.Meta.SchemaVersion)
	assert.Equal(t, entry.Meta.EntryID, got.Meta.EntryID)
	assert.Equal(t, entry.Meta.Kind, got.Meta.Kind)
	assert.Equal(t, entry.Meta.Method, got.Meta.Method)
	assert.Equal(t, entry.Meta.SourceHash, got.Meta.SourceHash)
	assert.Equal(t, entry.Meta.CompileFingerprint, got.Meta.CompileFingerprint)
	assert.Equal(t, entry.Meta.TypecheckFingerprint, got.Meta.TypecheckFingerprint)
	assert.Equal(t, fixed, got.Meta.CreatedAt)
	assert.Equal(t, entry.Proto, got.Proto)

	gotManifest, err := io.DecodeManifest(got.Manifest)
	require.NoError(t, err)
	assert.Equal(t, manifest.Path, gotManifest.Path)
	assert.Equal(t, manifest.Version, gotManifest.Version)

	require.Len(t, got.Diagnostics, 1)
	assert.Equal(t, diags[0].Code, got.Diagnostics[0].Code)
	assert.Equal(t, diags[0].Message, got.Diagnostics[0].Message)
}

func TestDiskStoreDelete(t *testing.T) {
	store := NewDiskStore(t.TempDir())
	entry := &Entry{
		Meta: Meta{
			EntryID:            "app/main",
			CompileFingerprint: "compile-fp",
			SourceHash:         "abc",
		},
		Proto: []byte{0x01},
	}
	require.NoError(t, store.Put("key", entry))

	require.NoError(t, store.Delete("key"))
	_, ok, err := store.Get("key")
	require.NoError(t, err)
	assert.False(t, ok)
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

// replaceSeam swaps a package-level test seam and restores it during cleanup.
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
	replaceSeam(t, &pruneScanHook, func(string) {
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
	replaceSeam(t, &pruneScanHook, func(path string) {
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
	// A concurrent Put has created this entry and written only a temp file.
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
	replaceSeam(t, &pruneScanHook, func(path string) {
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

func TestDiskStorePruneSkipsEntryItCannotScan(t *testing.T) {
	store := NewBoundedDiskStore(t.TempDir(), 1<<20, 1, 1000)
	putTestEntry(t, store, "a", time.Unix(1, 0))
	putTestEntry(t, store, "b", time.Unix(2, 0))
	putTestEntry(t, store, "c", time.Unix(3, 0))
	unreadable := store.entryDir("a")
	replaceSeam(t, &walkDir, func(root string, fn fs.WalkDirFunc) error {
		if root == unreadable {
			return fs.ErrPermission
		}
		return filepath.WalkDir(root, fn)
	})

	require.NoError(t, store.Prune(), "the limits are met without the unreadable entry")

	assert.True(t, entryExists(t, store, "a"))
	assert.False(t, entryExists(t, store, "b"))
	assert.True(t, entryExists(t, store, "c"))
}

func TestDiskStorePruneSkipsEntryItCannotRemove(t *testing.T) {
	store := NewBoundedDiskStore(t.TempDir(), 1<<20, 2, 1000)
	putTestEntry(t, store, "a", time.Unix(1, 0))
	putTestEntry(t, store, "b", time.Unix(2, 0))
	putTestEntry(t, store, "c", time.Unix(3, 0))
	stuck := store.entryDir("a")
	replaceSeam(t, &removeAll, func(path string) error {
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
	replaceSeam(t, &removeAll, func(string) error { return injected })

	require.ErrorIs(t, store.Prune(), injected)

	assert.True(t, entryExists(t, store, "a"))
	assert.True(t, entryExists(t, store, "b"))
}

func TestDiskStoreBackgroundPruneErrorReachesHandler(t *testing.T) {
	store := NewBoundedDiskStore(t.TempDir(), 1<<20, 1, 2)
	injected := errors.New("injected removal failure")
	replaceSeam(t, &removeAll, func(string) error { return injected })
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

func TestDiskStorePutWaitsForReaderOfReplacedFile(t *testing.T) {
	store := NewDiskStore(t.TempDir())
	putTestEntry(t, store, "k", time.Unix(1, 0))
	reader, err := os.Open(filepath.Join(store.entryDir("k"), metaFile))
	require.NoError(t, err)
	closed := make(chan struct{})
	go func() {
		defer close(closed)
		time.Sleep(20 * time.Millisecond)
		_ = reader.Close()
	}()
	t.Cleanup(func() { <-closed })

	require.NoError(t, store.Put("k", &Entry{Meta: Meta{EntryID: "k", SourceHash: "rewritten"}, Proto: []byte("rewritten")}))

	got, ok, err := store.Get("k")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, []byte("rewritten"), got.Proto)
	leftovers, err := filepath.Glob(filepath.Join(store.entryDir("k"), "*.tmp-*"))
	require.NoError(t, err)
	assert.Empty(t, leftovers)
}

func TestDiskStorePutRemovesTempFileWhenReplaceFails(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("only Windows refuses to replace a file that another handle has open")
	}
	store := NewDiskStore(t.TempDir())
	putTestEntry(t, store, "k", time.Unix(1, 0))
	reader, err := os.Open(filepath.Join(store.entryDir("k"), metaFile))
	require.NoError(t, err)

	putErr := store.Put("k", &Entry{Meta: Meta{EntryID: "k", SourceHash: "rewritten"}, Proto: []byte("rewritten")})
	require.NoError(t, reader.Close())

	require.ErrorIs(t, putErr, fs.ErrPermission)
	leftovers, err := filepath.Glob(filepath.Join(store.entryDir("k"), "*.tmp-*"))
	require.NoError(t, err)
	assert.Empty(t, leftovers)
}

func TestDiskStore_ConcurrentTwoStoresInterleaved(t *testing.T) {
	dir := t.TempDir()
	store1 := NewBoundedDiskStore(dir, 10<<20, 20, 5)
	store2 := NewBoundedDiskStore(dir, 10<<20, 20, 5)
	t.Cleanup(func() {
		store1.WaitPrune()
		store2.WaitPrune()
	})

	manifestA := []byte("manifest-A")
	manifestB := []byte("manifest-B")
	protoA := []byte("proto-bytes-A")
	protoB := []byte("proto-bytes-B")
	diagsA := []diag.Diagnostic{{Code: 1, Message: "diag-A"}}
	diagsB := []diag.Diagnostic{{Code: 2, Message: "diag-B"}}

	keys := []string{"key-1", "key-2", "key-3"}

	var wg sync.WaitGroup
	iterations := 200

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			key := keys[i%len(keys)]
			entryA := &Entry{
				Meta: Meta{
					EntryID:    "entryA",
					SourceHash: "srcA",
				},
				Manifest:    manifestA,
				Proto:       protoA,
				Diagnostics: diagsA,
			}
			_ = store1.Put(key, entryA)
			got, ok, err := store1.Get(key)
			if err == nil && ok && got != nil {
				validateEntryCompleteness(t, got, "entryA", manifestA, protoA, diagsA, "entryB", manifestB, protoB, diagsB)
			}
			_ = store1.Prune()
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			key := keys[(i+1)%len(keys)]
			entryB := &Entry{
				Meta: Meta{
					EntryID:    "entryB",
					SourceHash: "srcB",
				},
				Manifest:    manifestB,
				Proto:       protoB,
				Diagnostics: diagsB,
			}
			_ = store2.Put(key, entryB)
			got, ok, err := store2.Get(key)
			if err == nil && ok && got != nil {
				validateEntryCompleteness(t, got, "entryA", manifestA, protoA, diagsA, "entryB", manifestB, protoB, diagsB)
			}
			_ = store2.Prune()
		}
	}()

	for w := 0; w < 2; w++ {
		wg.Add(1)
		st := store1
		if w%2 == 1 {
			st = store2
		}
		go func(s *DiskStore) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				key := keys[i%len(keys)]
				got, ok, err := s.Get(key)
				if err == nil && ok && got != nil {
					validateEntryCompleteness(t, got, "entryA", manifestA, protoA, diagsA, "entryB", manifestB, protoB, diagsB)
				}
			}
		}(st)
	}

	wg.Wait()
}

func validateEntryCompleteness(t *testing.T, entry *Entry, idA string, manA, protoA []byte, diagsA []diag.Diagnostic, idB string, manB, protoB []byte, diagsB []diag.Diagnostic) {
	t.Helper()
	switch entry.Meta.EntryID {
	case idA:
		assert.Equal(t, manA, entry.Manifest, "entryA must have manifestA")
		assert.Equal(t, protoA, entry.Proto, "entryA must have protoA")
		assert.Equal(t, diagsA, entry.Diagnostics, "entryA must have diagsA")
	case idB:
		assert.Equal(t, manB, entry.Manifest, "entryB must have manifestB")
		assert.Equal(t, protoB, entry.Proto, "entryB must have protoB")
		assert.Equal(t, diagsB, entry.Diagnostics, "entryB must have diagsB")
	default:
		t.Fatalf("unexpected entry id: %s", entry.Meta.EntryID)
	}
}

func TestDiskStore_NewPartWithOldMetaReturnsMiss(t *testing.T) {
	dir := t.TempDir()
	store := NewBoundedDiskStore(dir, 10<<20, 10, 10)

	entry := &Entry{
		Meta: Meta{
			EntryID:            "app.main",
			CompileFingerprint: "compile-fp-1",
			SourceHash:         "source-1",
		},
		Manifest:    []byte("manifest-1"),
		Proto:       []byte("proto-1"),
		Diagnostics: []diag.Diagnostic{{Code: 1, Message: "diag-1"}},
	}
	require.NoError(t, store.Put("key-1", entry))

	got, ok, err := store.Get("key-1")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, []byte("proto-1"), got.Proto)

	// Simulate a new part being written to disk while meta still has the old part hash
	entryDir := store.entryDir("key-1")
	newProto := []byte("proto-2-new-bytes")
	require.NoError(t, os.WriteFile(filepath.Join(entryDir, protoFile), newProto, 0o644))

	// A reader observing the new part with old meta must get a miss
	gotMiss, okMiss, errMiss := store.Get("key-1")
	require.NoError(t, errMiss)
	assert.False(t, okMiss, "must return miss when part hash does not match meta")
	assert.Nil(t, gotMiss)
}
