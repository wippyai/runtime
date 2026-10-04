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
	writeErr error
	reads    atomic.Int32
}

func (s *countingCacheStore) Get(string) (*cache.Entry, bool, error) {
	s.reads.Add(1)
	return s.entry, s.entry != nil, nil
}
func (s *countingCacheStore) Put(string, *cache.Entry) error { return s.writeErr }

func TestCompileCacheLoadsReadTheStoreAndShareNothing(t *testing.T) {
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
	second, ok := cm.loadCompileCache(id, "fp")
	require.True(t, ok)
	require.NotSame(t, first, second)
	require.Empty(t, second.TypeInfo)
	require.NotNil(t, second.Constants[0])
	require.Equal(t, "original", executeCompiledString(t, second))
	// The warmed store is the only copy of compiled bytes: every load reads it.
	require.EqualValues(t, 2, store.reads.Load())
	require.EqualValues(t, 2, cm.CacheStats().CompileHits)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Go(func() {
			proto, ok := cm.loadCompileCache(id, "fp")
			if !ok {
				t.Error("compile cache miss")
				return
			}
			require.Equal(t, "original", executeCompiledString(t, proto))
		})
	}
	wg.Wait()
	require.EqualValues(t, 18, store.reads.Load())
	// The entry ID is validated independently of the fingerprint.
	_, ok = cm.loadCompileCache(registry.NewID("bee", "other"), "fp")
	require.False(t, ok)
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
		data []byte
		name string
		meta cache.Meta
	}{
		{name: "schema", meta: cache.Meta{SchemaVersion: cache.SchemaVersion + 1, EntryID: id.String(), CompileFingerprint: "fp"}, data: []byte("invalid")},
		{name: "identity", meta: cache.Meta{SchemaVersion: cache.SchemaVersion, EntryID: "other:module", CompileFingerprint: "fp"}, data: []byte("invalid")},
		{name: "fingerprint", meta: cache.Meta{SchemaVersion: cache.SchemaVersion, EntryID: id.String(), CompileFingerprint: "other"}, data: []byte("invalid")},
		{name: "empty", meta: cache.Meta{SchemaVersion: cache.SchemaVersion, EntryID: id.String(), CompileFingerprint: "fp"}},
		{name: "corrupt", meta: cache.Meta{SchemaVersion: cache.SchemaVersion, EntryID: id.String(), CompileFingerprint: "fp"}, data: []byte("invalid")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cm, err := NewCodeManager(zap.NewNop(), nil, Config{Cache: cache.Config{Enabled: true, CompileEnabled: true, Mode: cache.ModeReadOnly, Dir: t.TempDir(), ToolchainIdentity: "test"}})
			require.NoError(t, err)
			cm.cacheStore = &countingCacheStore{entry: &cache.Entry{Meta: tc.meta, Proto: tc.data}}
			proto, ok := cm.loadCompileCache(id, "fp")
			require.False(t, ok)
			require.Nil(t, proto)
			require.EqualValues(t, 1, cm.CacheStats().CompileMisses)
		})
	}
}

func TestEmbeddedCompileMismatchIsRecompiledAndReplaced(t *testing.T) {
	cfg := Config{Cache: cache.Config{Enabled: true, CompileEnabled: true, Dir: t.TempDir(), ToolchainIdentity: "test"}}
	original, err := NewCodeManager(zap.NewNop(), nil, cfg)
	require.NoError(t, err)
	id := registry.NewID("test", "entry")
	node := Node{ID: id, Kind: api.Library, Source: `return "valid"`}
	require.NoError(t, original.AddNode(nil, node, nil))
	_, err = original.Compile(id, nil)
	require.NoError(t, err)
	fingerprint, _, err := original.compileFingerprint(id)
	require.NoError(t, err)
	cfg.Cache.Dir = t.TempDir()
	cfg.EmbeddedCache = &countingCacheStore{entry: &cache.Entry{
		Meta:  cache.Meta{SchemaVersion: cache.SchemaVersion, EntryID: id.String(), CompileFingerprint: fingerprint},
		Proto: []byte("invalid bytecode"),
	}}
	restored, err := NewCodeManager(zap.NewNop(), nil, cfg)
	require.NoError(t, err)
	require.NoError(t, restored.AddNode(nil, node, nil))
	compiled, err := restored.Compile(id, nil)
	require.NoError(t, err)
	require.Equal(t, "valid", executeCompiledString(t, compiled.Main))
	require.Positive(t, restored.CacheStats().CompileMisses)
	proto, ok := restored.loadCompileCache(id, fingerprint)
	require.True(t, ok, "the rejected embedded artifact must be replaced")
	require.Equal(t, "valid", executeCompiledString(t, proto))
}

func TestEmbeddedCacheHonorsCacheModes(t *testing.T) {
	cfg := Config{Cache: cache.Config{Enabled: true, CompileEnabled: true, Dir: t.TempDir(), ToolchainIdentity: "test"}}
	producer, err := NewCodeManager(zap.NewNop(), nil, cfg)
	require.NoError(t, err)
	id := registry.NewID("test", "modes")
	node := Node{ID: id, Kind: api.Library, Source: `return "embedded"`}
	require.NoError(t, producer.AddNode(nil, node, nil))
	compiled, err := producer.Compile(id, nil)
	require.NoError(t, err)
	fp, _, err := producer.compileFingerprint(id)
	require.NoError(t, err)
	data, err := bytecode.Dump(compiled.Main)
	require.NoError(t, err)
	for _, mode := range []cache.Mode{cache.ModeReadOnly, cache.ModeOff} {
		t.Run(string(mode), func(t *testing.T) {
			seed := &countingCacheStore{entry: &cache.Entry{Meta: cache.Meta{SchemaVersion: cache.SchemaVersion, EntryID: id.String(), CompileFingerprint: fp}, Proto: data}}
			cfg.Cache.Dir = t.TempDir()
			cfg.Cache.Mode = mode
			cfg.EmbeddedCache = seed
			cm, err := NewCodeManager(zap.NewNop(), nil, cfg)
			require.NoError(t, err)
			require.NoError(t, cm.AddNode(nil, node, nil))
			_, err = cm.Compile(id, nil)
			require.NoError(t, err)
			if mode == cache.ModeReadOnly {
				require.Positive(t, seed.reads.Load())
				require.Positive(t, cm.CacheStats().CompileHits)
			} else {
				require.Zero(t, seed.reads.Load())
				require.Zero(t, cm.CacheStats().CompileHits)
			}
			require.NoDirExists(t, cfg.Cache.Dir+"/v1/entries")
		})
	}
}
