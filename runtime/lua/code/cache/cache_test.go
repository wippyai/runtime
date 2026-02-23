// SPDX-License-Identifier: MPL-2.0

package cache

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Config ---

func TestConfig_Normalize_Defaults(t *testing.T) {
	cfg := Config{Enabled: true}
	norm := cfg.Normalize()
	assert.Equal(t, DefaultDir, norm.Dir)
	assert.Equal(t, ModeReadWrite, norm.Mode)
}

func TestConfig_Normalize_DisabledSetsOff(t *testing.T) {
	cfg := Config{Enabled: false, Mode: ModeReadWrite}
	norm := cfg.Normalize()
	assert.Equal(t, ModeOff, norm.Mode)
}

func TestConfig_Normalize_PreservesDir(t *testing.T) {
	cfg := Config{Enabled: true, Dir: "/custom/dir"}
	norm := cfg.Normalize()
	assert.Equal(t, "/custom/dir", norm.Dir)
}

func TestConfig_Normalize_PreservesMode(t *testing.T) {
	cfg := Config{Enabled: true, Mode: ModeReadOnly}
	norm := cfg.Normalize()
	assert.Equal(t, ModeReadOnly, norm.Mode)
}

func TestConfig_AllowsRead(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		allowed bool
	}{
		{"disabled", Config{Enabled: false}, false},
		{"off", Config{Enabled: true, Mode: ModeOff}, false},
		{"readonly", Config{Enabled: true, Mode: ModeReadOnly}, true},
		{"readwrite", Config{Enabled: true, Mode: ModeReadWrite}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.allowed, tt.cfg.AllowsRead())
		})
	}
}

func TestConfig_AllowsWrite(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		allowed bool
	}{
		{"disabled", Config{Enabled: false}, false},
		{"off", Config{Enabled: true, Mode: ModeOff}, false},
		{"readonly", Config{Enabled: true, Mode: ModeReadOnly}, false},
		{"readwrite", Config{Enabled: true, Mode: ModeReadWrite}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.allowed, tt.cfg.AllowsWrite())
		})
	}
}

// --- ParseMode ---

func TestParseMode(t *testing.T) {
	tests := []struct {
		input    string
		expected Mode
	}{
		{"off", ModeOff},
		{"readonly", ModeReadOnly},
		{"readwrite", ModeReadWrite},
		{"unknown", ModeReadWrite},
		{"", ModeReadWrite},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, ParseMode(tt.input))
		})
	}
}

// --- SourceHash ---

func TestSourceHash_Deterministic(t *testing.T) {
	h1 := SourceHash("print('hello')", "main")
	h2 := SourceHash("print('hello')", "main")
	assert.Equal(t, h1, h2)
	assert.Len(t, h1, 64) // SHA256 hex
}

func TestSourceHash_DifferentSource(t *testing.T) {
	h1 := SourceHash("print('a')", "main")
	h2 := SourceHash("print('b')", "main")
	assert.NotEqual(t, h1, h2)
}

func TestSourceHash_DifferentMethod(t *testing.T) {
	h1 := SourceHash("code", "main")
	h2 := SourceHash("code", "test")
	assert.NotEqual(t, h1, h2)
}

// --- HashStrings ---

func TestHashStrings_Deterministic(t *testing.T) {
	h1 := HashStrings("a", "b", "c")
	h2 := HashStrings("a", "b", "c")
	assert.Equal(t, h1, h2)
}

func TestHashStrings_OrderMatters(t *testing.T) {
	h1 := HashStrings("a", "b")
	h2 := HashStrings("b", "a")
	assert.NotEqual(t, h1, h2)
}

func TestHashStrings_SeparatorPreventsCollision(t *testing.T) {
	// "ab" + "c" should differ from "a" + "bc" due to null separators
	h1 := HashStrings("ab", "c")
	h2 := HashStrings("a", "bc")
	assert.NotEqual(t, h1, h2)
}

func TestHashStrings_Empty(t *testing.T) {
	h := HashStrings()
	assert.Len(t, h, 64)
}

// --- Fingerprint ---

func TestFingerprint_Deterministic(t *testing.T) {
	deps := []DepFingerprint{
		{Alias: "lib", ID: "ns:lib", Fingerprint: "fp1"},
	}
	f1 := Fingerprint("self-hash", deps)
	f2 := Fingerprint("self-hash", deps)
	assert.Equal(t, f1, f2)
}

func TestFingerprint_DifferentSelfHash(t *testing.T) {
	deps := []DepFingerprint{{Alias: "a", ID: "ns:a", Fingerprint: "fp"}}
	f1 := Fingerprint("hash1", deps)
	f2 := Fingerprint("hash2", deps)
	assert.NotEqual(t, f1, f2)
}

func TestFingerprint_DependencyOrderIndependent(t *testing.T) {
	deps1 := []DepFingerprint{
		{Alias: "a", ID: "ns:a", Fingerprint: "fp-a"},
		{Alias: "b", ID: "ns:b", Fingerprint: "fp-b"},
	}
	deps2 := []DepFingerprint{
		{Alias: "b", ID: "ns:b", Fingerprint: "fp-b"},
		{Alias: "a", ID: "ns:a", Fingerprint: "fp-a"},
	}
	f1 := Fingerprint("self", deps1)
	f2 := Fingerprint("self", deps2)
	assert.Equal(t, f1, f2)
}

func TestFingerprint_NoDeps(t *testing.T) {
	f := Fingerprint("self", nil)
	assert.Len(t, f, 64)
}

func TestFingerprint_DoesNotMutateInput(t *testing.T) {
	deps := []DepFingerprint{
		{Alias: "b", ID: "ns:b", Fingerprint: "fp-b"},
		{Alias: "a", ID: "ns:a", Fingerprint: "fp-a"},
	}
	Fingerprint("self", deps)
	assert.Equal(t, "b", deps[0].Alias)
	assert.Equal(t, "a", deps[1].Alias)
}

// --- CompileKey / TypecheckKey ---

func TestCompileKey_Deterministic(t *testing.T) {
	k1 := CompileKey("fp-abc")
	k2 := CompileKey("fp-abc")
	assert.Equal(t, k1, k2)
	assert.Len(t, k1, 64)
}

func TestCompileKey_DifferentFingerprint(t *testing.T) {
	k1 := CompileKey("fp-1")
	k2 := CompileKey("fp-2")
	assert.NotEqual(t, k1, k2)
}

func TestTypecheckKey_Deterministic(t *testing.T) {
	k1 := TypecheckKey("fp-abc")
	k2 := TypecheckKey("fp-abc")
	assert.Equal(t, k1, k2)
}

func TestTypecheckKey_DiffersFromCompileKey(t *testing.T) {
	ck := CompileKey("fp-same")
	tk := TypecheckKey("fp-same")
	assert.NotEqual(t, ck, tk)
}

// --- DecodeManifestSafe ---

func TestDecodeManifestSafe_EmptyData(t *testing.T) {
	m, ok := DecodeManifestSafe(nil)
	assert.Nil(t, m)
	assert.False(t, ok)
}

func TestDecodeManifestSafe_CorruptData(t *testing.T) {
	m, ok := DecodeManifestSafe([]byte{0xFF, 0xFE, 0xFD})
	assert.Nil(t, m)
	assert.False(t, ok)
}

// --- DiskStore extended tests ---

func TestDiskStore_Get_NonExistent(t *testing.T) {
	store := NewDiskStore(t.TempDir())
	entry, ok, err := store.Get("nonexistent")
	require.NoError(t, err)
	assert.False(t, ok)
	assert.Nil(t, entry)
}

func TestDiskStore_Put_NilEntry(t *testing.T) {
	store := NewDiskStore(t.TempDir())
	err := store.Put("key", nil)
	assert.NoError(t, err)
}

func TestDiskStore_Put_SetsSchemaVersion(t *testing.T) {
	dir := t.TempDir()
	store := NewDiskStore(dir)

	entry := &Entry{
		Meta:  Meta{EntryID: "test", SourceHash: "h"},
		Proto: []byte{0x01},
	}
	require.NoError(t, store.Put("key", entry))

	got, ok, err := store.Get("key")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, SchemaVersion, got.Meta.SchemaVersion)
}

func TestDiskStore_Put_SetsCreatedAtWhenZero(t *testing.T) {
	dir := t.TempDir()
	store := NewDiskStore(dir)

	fixed := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
	prev := nowUTC
	nowUTC = func() time.Time { return fixed }
	t.Cleanup(func() { nowUTC = prev })

	entry := &Entry{
		Meta:  Meta{EntryID: "test", SourceHash: "h"},
		Proto: []byte{0x01},
	}
	require.NoError(t, store.Put("key", entry))

	got, ok, err := store.Get("key")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, fixed, got.Meta.CreatedAt)
}

func TestDiskStore_Put_PreservesExistingCreatedAt(t *testing.T) {
	dir := t.TempDir()
	store := NewDiskStore(dir)

	custom := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	entry := &Entry{
		Meta:  Meta{EntryID: "test", SourceHash: "h", CreatedAt: custom},
		Proto: []byte{0x01},
	}
	require.NoError(t, store.Put("key", entry))

	got, ok, err := store.Get("key")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, custom, got.Meta.CreatedAt)
}

func TestDiskStore_Overwrite(t *testing.T) {
	dir := t.TempDir()
	store := NewDiskStore(dir)

	entry1 := &Entry{
		Meta:  Meta{EntryID: "first", SourceHash: "h1"},
		Proto: []byte{0x01},
	}
	require.NoError(t, store.Put("key", entry1))

	entry2 := &Entry{
		Meta:  Meta{EntryID: "second", SourceHash: "h2"},
		Proto: []byte{0x02, 0x03},
	}
	require.NoError(t, store.Put("key", entry2))

	got, ok, err := store.Get("key")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "second", got.Meta.EntryID)
	assert.Equal(t, []byte{0x02, 0x03}, got.Proto)
}

func TestDiskStore_Delete_NonExistent(t *testing.T) {
	store := NewDiskStore(t.TempDir())
	err := store.Delete("nonexistent")
	assert.NoError(t, err)
}

func TestDiskStore_Get_CorruptMeta(t *testing.T) {
	dir := t.TempDir()
	store := NewDiskStore(dir)

	entryDir := filepath.Join(dir, "v1", "entries", "bad-key")
	require.NoError(t, os.MkdirAll(entryDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(entryDir, metaFile), []byte("not json"), 0o644))

	entry, ok, err := store.Get("bad-key")
	require.NoError(t, err)
	assert.False(t, ok)
	assert.Nil(t, entry)
}

func TestDiskStore_Put_OnlyProto(t *testing.T) {
	dir := t.TempDir()
	store := NewDiskStore(dir)

	entry := &Entry{
		Meta:  Meta{EntryID: "proto-only", SourceHash: "h"},
		Proto: []byte{0xCA, 0xFE},
	}
	require.NoError(t, store.Put("key", entry))

	got, ok, err := store.Get("key")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, []byte{0xCA, 0xFE}, got.Proto)
	assert.Nil(t, got.Manifest)
	assert.Nil(t, got.Diagnostics)
}

func TestDiskStore_Put_OnlyManifest(t *testing.T) {
	dir := t.TempDir()
	store := NewDiskStore(dir)

	entry := &Entry{
		Meta:     Meta{EntryID: "manifest-only", SourceHash: "h"},
		Manifest: []byte{0x01, 0x02, 0x03},
	}
	require.NoError(t, store.Put("key", entry))

	got, ok, err := store.Get("key")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, []byte{0x01, 0x02, 0x03}, got.Manifest)
	assert.Nil(t, got.Proto)
}

func TestDiskStore_DepMeta_Serialization(t *testing.T) {
	dir := t.TempDir()
	store := NewDiskStore(dir)

	entry := &Entry{
		Meta: Meta{
			EntryID:    "with-deps",
			SourceHash: "h",
			Deps: []DepMeta{
				{Alias: "lib", ID: "ns:lib", CompileFingerprint: "cfp"},
				{Alias: "util", ID: "ns:util", TypecheckFingerprint: "tfp"},
			},
		},
		Proto: []byte{0x01},
	}
	require.NoError(t, store.Put("key", entry))

	got, ok, err := store.Get("key")
	require.NoError(t, err)
	require.True(t, ok)
	require.Len(t, got.Meta.Deps, 2)
	assert.Equal(t, "lib", got.Meta.Deps[0].Alias)
	assert.Equal(t, "cfp", got.Meta.Deps[0].CompileFingerprint)
	assert.Equal(t, "util", got.Meta.Deps[1].Alias)
	assert.Equal(t, "tfp", got.Meta.Deps[1].TypecheckFingerprint)
}

// --- Meta JSON roundtrip ---

func TestMeta_JSON_Roundtrip(t *testing.T) {
	original := Meta{
		SchemaVersion:        SchemaVersion,
		EntryID:              "app/main",
		Kind:                 "function.lua",
		Method:               "main",
		SourceHash:           "abc123",
		CompileFingerprint:   "cfp",
		TypecheckFingerprint: "tfp",
		BuiltinHash:          "builtin",
		TypecheckConfigHash:  "tch",
		CreatedAt:            time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		Deps: []DepMeta{
			{Alias: "lib", ID: "ns:lib", CompileFingerprint: "dep-cfp"},
		},
	}

	data, err := json.Marshal(original)
	require.NoError(t, err)

	var restored Meta
	require.NoError(t, json.Unmarshal(data, &restored))

	assert.Equal(t, original.SchemaVersion, restored.SchemaVersion)
	assert.Equal(t, original.EntryID, restored.EntryID)
	assert.Equal(t, original.Kind, restored.Kind)
	assert.Equal(t, original.Method, restored.Method)
	assert.Equal(t, original.SourceHash, restored.SourceHash)
	assert.Equal(t, original.CompileFingerprint, restored.CompileFingerprint)
	assert.Equal(t, original.TypecheckFingerprint, restored.TypecheckFingerprint)
	assert.Equal(t, original.BuiltinHash, restored.BuiltinHash)
	assert.Equal(t, original.TypecheckConfigHash, restored.TypecheckConfigHash)
	assert.Equal(t, original.CreatedAt, restored.CreatedAt)
	require.Len(t, restored.Deps, 1)
	assert.Equal(t, original.Deps[0], restored.Deps[0])
}
