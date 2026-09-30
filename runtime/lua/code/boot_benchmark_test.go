// SPDX-License-Identifier: MPL-2.0

package code

import (
	"fmt"
	"os"
	"strings"
	"testing"

	glua "github.com/wippyai/go-lua"
	"github.com/wippyai/go-lua/compiler/bytecode"
	"github.com/wippyai/go-lua/compiler/parse"
	"github.com/wippyai/runtime/api/registry"
	api "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/code/cache"
	"go.uber.org/zap"
)

func bootProto(b *testing.B) *glua.FunctionProto {
	b.Helper()
	source := "local sum = 0\n" + strings.Repeat("sum = sum + 1\n", 200) + "return function() return sum end"
	chunk, err := parse.Parse(strings.NewReader(source), "bench")
	if err != nil {
		b.Fatal(err)
	}
	proto, err := glua.Compile(chunk, "bench")
	if err != nil {
		b.Fatal(err)
	}
	return proto
}

// BenchmarkBootCompileCache1400 reads 700 units twice, as different isolates do.
// Disk artifacts are warm; each iteration starts with a new manager.
func BenchmarkBootCompileCache1400(b *testing.B) {
	benchmarkBootCompileCache(b, 700)
}

func BenchmarkBootCompileCache20000(b *testing.B) {
	benchmarkBootCompileCache(b, 10_000)
}

func benchmarkBootCompileCache(b *testing.B, count int) {
	b.Helper()
	cfg := cache.Config{Enabled: true, CompileEnabled: true, Mode: cache.ModeReadWrite, Dir: b.TempDir(), ToolchainIdentity: "bench"}.Normalize()
	store := cache.NewBoundedDiskStore(cfg.Dir, cfg.MaxBytes, cfg.MaxEntries, cfg.PruneInterval)
	data, err := bytecode.Dump(bootProto(b))
	if err != nil {
		b.Fatal(err)
	}
	ids := make([]registry.ID, count)
	fps := make([]string, count)
	for i := range ids {
		ids[i] = registry.NewID("bee", fmt.Sprintf("unit%04d", i))
		fps[i] = cache.HashStrings(ids[i].String())
		err := store.Put(cache.CompileKey(fps[i]), &cache.Entry{Meta: cache.Meta{SchemaVersion: cache.SchemaVersion, EntryID: ids[i].String(), CompileFingerprint: fps[i]}, Proto: data})
		if err != nil {
			b.Fatal(err)
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		cm, err := NewCodeManager(zap.NewNop(), nil, Config{Cache: cfg})
		if err != nil {
			b.Fatal(err)
		}
		for pass := 0; pass < 2; pass++ {
			for i, id := range ids {
				if _, ok := cm.loadCompileCache(id, fps[i]); !ok {
					b.Fatal("miss")
				}
			}
		}
	}
}

// BenchmarkBootFingerprints1400 models 700 entrypoints importing 12 shared
// libraries, with source hashes already recorded by registry revision on main.
func BenchmarkBootFingerprints1400(b *testing.B) {
	benchmarkBootFingerprints(b, 700)
}

func BenchmarkBootFingerprints20000(b *testing.B) {
	benchmarkBootFingerprints(b, 10_000)
}

func benchmarkBootFingerprints(b *testing.B, count int) {
	b.Helper()
	cm := &Manager{memGraph: NewMemoryGraph(), toolchainIdentity: "bench", typeCfgHash: "types", builtinHash: "builtins"}
	libs := make([]registry.ID, 12)
	for i := range libs {
		libs[i] = registry.NewID("bee", fmt.Sprintf("lib%02d", i))
		node := &Node{ID: libs[i], Kind: api.Library, Source: "return 1", Version: Version{Revision: uint64(i + 1)}}
		node.Version.Hash = HashNode(node)
		if err := cm.memGraph.AddNode(node); err != nil {
			b.Fatal(err)
		}
	}
	ids := make([]registry.ID, count)
	for i := range ids {
		ids[i] = registry.NewID("bee", fmt.Sprintf("unit%04d", i))
		node := &Node{ID: ids[i], Kind: api.Function, Source: "return 1", Method: "main", Version: Version{Revision: uint64(i + 13)}}
		node.Version.Hash = HashNode(node)
		if err := cm.memGraph.AddNode(node); err != nil {
			b.Fatal(err)
		}
		for j, lib := range libs {
			if err := cm.memGraph.AddDependency(ids[i], lib, fmt.Sprintf("lib%d", j)); err != nil {
				b.Fatal(err)
			}
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		// A fresh graph generation models a new boot, including first-pass
		// memo population, rather than measuring only resident cache hits.
		b.StopTimer()
		marker := registry.NewID("bench", "generation")
		if err := cm.memGraph.AddNode(&Node{ID: marker}); err != nil {
			b.Fatal(err)
		}
		if err := cm.memGraph.RemoveNode(marker); err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
		for pass := 0; pass < 2; pass++ {
			for _, id := range ids {
				memo := newBuildMemo()
				if _, err := runtimeFingerprintMemo(cm.memGraph, id, memo.runtime, cm.toolchainIdentity); err != nil {
					b.Fatal(err)
				}
				if _, err := cm.compileFingerprintMemo(cm.memGraph, id, memo.compile, memo.compileMeta); err != nil {
					b.Fatal(err)
				}
				if _, err := cm.typecheckFingerprintMemo(cm.memGraph, id, memo.typecheck, memo.typecheckMeta); err != nil {
					b.Fatal(err)
				}
			}
		}
	}
}

type bootFailStore struct{}

func (bootFailStore) Get(string) (*cache.Entry, bool, error) { return nil, false, nil }
func (bootFailStore) Put(string, *cache.Entry) error         { return os.ErrPermission }

// BenchmarkBootCacheWriteFailure1400 measures the nonfatal write-failure path.
// The startup directory warning is already present on main; this exercises a
// directory becoming unwritable after startup, during compilation.
func BenchmarkBootCacheWriteFailure1400(b *testing.B) {
	cfg := cache.Config{Enabled: true, CompileEnabled: true, Mode: cache.ModeReadWrite, ToolchainIdentity: "bench"}.Normalize()
	proto := bootProto(b)
	node := &Node{ID: registry.NewID("bee", "unit"), Kind: api.Library, Source: "return 1"}
	node.Version.Hash = HashNode(node)
	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		cm := &Manager{cacheCfg: cfg, cacheStore: bootFailStore{}, log: zap.NewNop()}
		for i := 0; i < 1400; i++ {
			cm.saveCompileCache(node, "fingerprint", nil, proto)
		}
	}
}
