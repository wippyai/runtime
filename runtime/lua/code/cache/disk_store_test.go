// SPDX-License-Identifier: MPL-2.0

package cache

import (
	"os"
	"path/filepath"
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

func TestDiskStorePrunesOldestGenerationByEntryLimit(t *testing.T) {
	store := NewBoundedDiskStore(t.TempDir(), 1<<20, 2, 1)
	base := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	for i, key := range []string{"old", "middle", "new"} {
		require.NoError(t, store.Put(key, &Entry{
			Meta:  Meta{EntryID: key, SourceHash: key, CreatedAt: base.Add(time.Duration(i) * time.Hour)},
			Proto: []byte(key),
		}))
	}

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
	require.NoError(t, store.Put("old", &Entry{
		Meta:  Meta{EntryID: "old", SourceHash: "old", CreatedAt: time.Unix(1, 0)},
		Proto: make([]byte, 512),
	}))
	require.NoError(t, store.Put("new", &Entry{
		Meta:  Meta{EntryID: "new", SourceHash: "new", CreatedAt: time.Unix(2, 0)},
		Proto: make([]byte, 512),
	}))

	_, oldExists, err := store.Get("old")
	require.NoError(t, err)
	assert.False(t, oldExists)
	_, newExists, err := store.Get("new")
	require.NoError(t, err)
	assert.True(t, newExists)
}

func TestDiskStore_ConcurrentTwoStoresInterleaved(t *testing.T) {
	dir := t.TempDir()
	store1 := NewBoundedDiskStore(dir, 10<<20, 20, 5)
	store2 := NewBoundedDiskStore(dir, 10<<20, 20, 5)

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
