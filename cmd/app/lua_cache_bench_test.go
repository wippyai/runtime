// SPDX-License-Identifier: MPL-2.0

package app

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/wippyai/runtime/runtime/lua/code/cache"
)

// BenchmarkLuaSeed906 measures first-binary adoption of 906 synthetic compile
// entries, excluding archive construction and state cleanup.
func BenchmarkLuaSeed906(b *testing.B) {
	root := b.TempDir()
	store := cache.NewDiskStore(root)
	for i := range 906 {
		fp := fmt.Sprintf("entry-%d", i)
		if err := store.Put(cache.CompileKey(fp), &cache.Entry{Meta: cache.Meta{CompileFingerprint: fp}, Proto: make([]byte, 1024)}); err != nil {
			b.Fatal(err)
		}
	}
	benchmarkLuaSeedDirectory(b, root)
}

// BenchmarkLuaSeedDirectory measures an existing verified cache directory.
// Set WIPPY_LUA_SEED_BENCH_DIR to a cache root; no source files are changed.
func BenchmarkLuaSeedDirectory(b *testing.B) {
	root := os.Getenv("WIPPY_LUA_SEED_BENCH_DIR")
	if root == "" {
		b.Skip("set WIPPY_LUA_SEED_BENCH_DIR to a verified Lua cache")
	}
	benchmarkLuaSeedDirectory(b, root)
}

func benchmarkLuaSeedDirectory(b *testing.B, root string) {
	b.Helper()
	entries, err := os.ReadDir(filepath.Join(root, "v1", "entries"))
	if err != nil {
		b.Fatal(err)
	}
	archive := makeLuaCacheArchive(b, root)
	b.Logf("%d entries, %d compressed archive bytes", len(entries), len(archive))
	schema, toolchain, err := LuaCacheIdentity()
	if err != nil {
		b.Fatal(err)
	}
	digest := sha256.Sum256(archive)
	seed := LuaCacheSeed{Archive: archive, Digest: "sha256:" + hex.EncodeToString(digest[:]), SchemaVersion: schema, ToolchainIdentity: toolchain}
	states := b.TempDir()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		state := filepath.Join(states, fmt.Sprint(i))
		if _, err := seedLuaCache(&seed); err != nil {
			b.Fatal(err)
		}
		b.StopTimer()
		if err := os.RemoveAll(state); err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
	}
}
