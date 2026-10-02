// SPDX-License-Identifier: MPL-2.0

package cache

import (
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

type testSeedReader struct {
	entry *Entry
	err   error
}

func (s testSeedReader) Get(string) (*Entry, bool, error) { return s.entry, s.entry != nil, s.err }

func TestLayeredStorePrefersEmbeddedAndWritesOnlyMutableCache(t *testing.T) {
	disk := NewDiskStore(t.TempDir())
	embedded := &Entry{Meta: Meta{SchemaVersion: SchemaVersion}, Proto: []byte("embedded")}
	store := NewLayeredStore(testSeedReader{entry: embedded}, disk)
	key := CompileKey("test")
	entry, found, err := store.Get(key)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, embedded, entry)
	_, found, err = disk.Get(key)
	require.NoError(t, err)
	require.False(t, found)
	require.NoError(t, store.Delete(key))
	_, found, err = store.Get(key)
	require.NoError(t, err)
	require.False(t, found)
	replacement := &Entry{Proto: []byte("recompiled")}
	require.NoError(t, store.Put(key, replacement))
	entry, found, err = store.Get(key)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, replacement.Proto, entry.Proto)
	require.NoError(t, store.Prune())
	require.Equal(t, []byte("embedded"), embedded.Proto)
}

func TestLayeredStoreFallsBackOnEmbeddedMissAndError(t *testing.T) {
	disk := NewDiskStore(t.TempDir())
	key := CompileKey("test")
	require.NoError(t, disk.Put(key, &Entry{Proto: []byte("disk")}))
	for _, seed := range []testSeedReader{{}, {err: errors.New("invalid")}} {
		store := NewLayeredStore(seed, disk)
		entry, found, err := store.Get(key)
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, []byte("disk"), entry.Proto)
	}
}

func TestLayeredStoreConcurrentAccess(t *testing.T) {
	disk := NewDiskStore(t.TempDir())
	store := NewLayeredStore(testSeedReader{entry: &Entry{Proto: []byte("embedded")}}, disk)
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Go(func() {
			key := CompileKey(string(rune('a' + i)))
			for range 5 {
				if _, _, err := store.Get(key); err != nil {
					t.Error(err)
				}
				if err := store.Put(key, &Entry{Proto: []byte("replacement")}); err != nil {
					t.Error(err)
				}
				if err := store.Delete(key); err != nil {
					t.Error(err)
				}
			}
		})
	}
	wg.Wait()
}
