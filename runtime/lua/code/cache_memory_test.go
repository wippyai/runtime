// SPDX-License-Identifier: MPL-2.0

package code

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/go-lua/compiler/bytecode"
	"github.com/wippyai/runtime/api/registry"
	api "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/code/cache"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

type countingCacheStore struct {
	entry    *cache.Entry
	reads    atomic.Int32
	writeErr error
}

func (s *countingCacheStore) Get(string) (*cache.Entry, bool, error) {
	s.reads.Add(1)
	return s.entry, s.entry != nil, nil
}
func (s *countingCacheStore) Put(string, *cache.Entry) error { return s.writeErr }

func TestCompileByteCacheIsolation(t *testing.T) {
	cm, err := NewCodeManager(zap.NewNop(), nil, Config{Cache: cache.Config{Enabled: true, CompileEnabled: true, TypecheckEnabled: true, Mode: cache.ModeReadWrite, Dir: t.TempDir(), ToolchainIdentity: "test"}})
	require.NoError(t, err)
	id := registry.NewID("bee", "module")
	require.NoError(t, cm.AddNode(nil, Node{ID: id, Kind: api.Library, Source: `return "original"`}, nil))
	compiled, err := cm.Compile(id, nil)
	require.NoError(t, err)
	data, err := bytecode.Dump(compiled.Main)
	require.NoError(t, err)
	store := &countingCacheStore{entry: &cache.Entry{Meta: cache.Meta{SchemaVersion: cache.SchemaVersion, EntryID: id.String(), CompileFingerprint: "fp"}, Proto: data}}
	cm.cacheStore = store
	first, ok := cm.loadCompileCache(id, "fp")
	require.True(t, ok)
	first.SetTypeInfo([]byte("first-isolate"))
	first.Constants[0] = nil
	// Corrupt the store's backing bytes. The memory cache owns a separate copy.
	store.entry.Proto[0] ^= 0xff
	second, ok := cm.loadCompileCache(id, "fp")
	require.True(t, ok)
	require.NotSame(t, first, second)
	require.Empty(t, second.TypeInfo)
	require.NotNil(t, second.Constants[0])
	require.Equal(t, "original", executeCompiledString(t, second))
	require.EqualValues(t, 1, store.reads.Load())
	require.EqualValues(t, 2, cm.CacheStats().CompileHits)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Go(func() {
			proto, ok := cm.loadCompileCache(id, "fp")
			if !ok {
				t.Error("memory cache miss")
				return
			}
			require.Equal(t, "original", executeCompiledString(t, proto))
		})
	}
	wg.Wait()
	require.EqualValues(t, 1, store.reads.Load())
	// The entry ID is validated independently of the fingerprint.
	_, ok = cm.loadCompileCache(registry.NewID("bee", "other"), "fp")
	require.False(t, ok)
}

func TestCompileBytesCacheBoundsAndLRU(t *testing.T) {
	c := newCompileBytesCache(6, 2)
	key := func(fp string) compileBytesKey { return compileBytesKey{fingerprint: fp} }
	c.put(key("a"), []byte("aa"))
	c.put(key("b"), []byte("bb"))
	_, ok := c.get(key("a"))
	require.True(t, ok)
	c.put(key("c"), []byte("cc"))
	_, ok = c.get(key("b"))
	require.False(t, ok)
	c.put(key("d"), []byte("dddddd"))
	require.Equal(t, 6, c.bytes)
	require.Len(t, c.entries, 1)
	c.put(key("oversize"), []byte("oversize"))
	require.Equal(t, 6, c.bytes)
	require.Len(t, c.entries, 1)
}

func TestCompileBytesCacheConcurrent(t *testing.T) {
	c := newCompileBytesCache(64, 4)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Go(func() {
			for n := 0; n < 100; n++ {
				key := compileBytesKey{fingerprint: string(rune('a' + n%8))}
				c.put(key, []byte("bytecode"))
				if data, ok := c.get(key); ok && string(data) != "bytecode" {
					t.Errorf("data: %q", data)
				}
			}
		})
	}
	wg.Wait()
	require.LessOrEqual(t, c.bytes, 64)
	require.LessOrEqual(t, len(c.entries), 4)
}

func TestPersistentCacheWriteFailureWarnsOnce(t *testing.T) {
	core, logs := observer.New(zap.WarnLevel)
	cfg := cache.Config{Enabled: true, CompileEnabled: true, TypecheckEnabled: true, Mode: cache.ModeReadWrite, Dir: t.TempDir(), ToolchainIdentity: "test"}
	cm, err := NewCodeManager(zap.New(core), nil, Config{Cache: cfg})
	require.NoError(t, err)
	cm.cacheStore = &countingCacheStore{writeErr: errors.New("permission denied after startup")}
	id := registry.NewID("bee", "module")
	require.NoError(t, cm.AddNode(nil, Node{ID: id, Kind: api.Library, Source: "return 1"}, nil))
	compiled, err := cm.Compile(id, nil)
	require.NoError(t, err)
	require.NotNil(t, compiled.Main)
	node, err := cm.GetNode(id)
	require.NoError(t, err)
	cm.saveTypecheckCache(node, "typecheck", nil, nil, nil)
	cm.saveCompileCache(node, "compile", nil, compiled.Main)
	warnings := logs.All()
	require.Len(t, warnings, 1)
	require.Contains(t, warnings[0].Message, "persistent cache write failed")
	require.Equal(t, cfg.Dir, warnings[0].ContextMap()["dir"])
	require.Contains(t, warnings[0].ContextMap()["error"], "permission denied")
}

func TestCompileBytesRejectInvalidArtifacts(t *testing.T) {
	id := registry.NewID("bee", "module")
	for _, tc := range []struct {
		name string
		meta cache.Meta
		data []byte
	}{
		{"schema", cache.Meta{SchemaVersion: cache.SchemaVersion + 1, EntryID: id.String(), CompileFingerprint: "fp"}, []byte("invalid")},
		{"identity", cache.Meta{SchemaVersion: cache.SchemaVersion, EntryID: "other:module", CompileFingerprint: "fp"}, []byte("invalid")},
		{"fingerprint", cache.Meta{SchemaVersion: cache.SchemaVersion, EntryID: id.String(), CompileFingerprint: "other"}, []byte("invalid")},
		{"empty", cache.Meta{SchemaVersion: cache.SchemaVersion, EntryID: id.String(), CompileFingerprint: "fp"}, nil},
		{"corrupt", cache.Meta{SchemaVersion: cache.SchemaVersion, EntryID: id.String(), CompileFingerprint: "fp"}, []byte("invalid")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cm, err := NewCodeManager(zap.NewNop(), nil, Config{Cache: cache.Config{Enabled: true, CompileEnabled: true, Mode: cache.ModeReadOnly, Dir: t.TempDir(), ToolchainIdentity: "test"}})
			require.NoError(t, err)
			cm.cacheStore = &countingCacheStore{entry: &cache.Entry{Meta: tc.meta, Proto: tc.data}}
			proto, ok := cm.loadCompileCache(id, "fp")
			require.False(t, ok)
			require.Nil(t, proto)
			require.Empty(t, cm.compileBytes.entries)
			require.EqualValues(t, 1, cm.CacheStats().CompileMisses)
		})
	}
}
