// SPDX-License-Identifier: MPL-2.0

package cache

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiskStorePutWaitsForReaderOfReplacedFile(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("only Windows refuses to replace a file that another handle has open")
	}
	store := NewDiskStore(t.TempDir())
	putTestEntry(t, store, "k", time.Unix(1, 0))
	reader, err := os.Open(filepath.Join(store.entryDir("k"), metaFile))
	require.NoError(t, err)
	var closeOnce sync.Once
	closeReader := func() { closeOnce.Do(func() { _ = reader.Close() }) }
	t.Cleanup(closeReader)
	refused := 0
	// Release the reader as soon as a rename has been refused because of it.
	replaceSeam(t, &store.writer.rename, func(from, to string) error {
		err := os.Rename(from, to)
		if err != nil {
			refused++
			closeReader()
		}
		return err
	})

	require.NoError(t, store.Put("k", &Entry{Meta: Meta{EntryID: "k", SourceHash: "rewritten"}, Proto: []byte("rewritten")}))

	assert.Equal(t, 1, refused, "the open reader refused the first rename of meta.json")
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
