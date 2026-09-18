// SPDX-License-Identifier: MPL-2.0

package component

import (
	"github.com/tetratelabs/wazero"
	wasmrt "github.com/wippyai/wasm-runtime/runtime"
)

// CompilationCacheFactory creates a compilation cache. The caller that invokes
// the factory owns the returned cache and closes it.
type CompilationCacheFactory func() (wazero.CompilationCache, error)

// InMemoryCompilationCache builds a process-local compilation cache.
func InMemoryCompilationCache() (wazero.CompilationCache, error) {
	return wazero.NewCompilationCache(), nil
}

// DirCompilationCache builds a file-backed compilation cache rooted at dir.
func DirCompilationCache(dir string) CompilationCacheFactory {
	return func() (wazero.CompilationCache, error) {
		return wazero.NewCompilationCacheWithDir(dir)
	}
}

// RuntimeConfig constructs a wasmrt.Config configured with the compilation cache.
func RuntimeConfig(cache wazero.CompilationCache, memoryLimitPages uint32) *wasmrt.Config {
	return &wasmrt.Config{
		CompilationCache:   cache,
		MemoryLimitPages:   memoryLimitPages,
		CloseOnContextDone: true,
	}
}
