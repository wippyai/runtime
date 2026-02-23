// SPDX-License-Identifier: MPL-2.0

package cache

import (
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
