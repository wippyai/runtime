// SPDX-License-Identifier: MPL-2.0

package component

import (
	"github.com/tetratelabs/wazero"
	"github.com/wippyai/wasm-runtime/asyncify"
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

// Caches groups the compilation cache factory and the process-wide transform cache.
type Caches struct {
	Compilation CompilationCacheFactory
	Transform   asyncify.TransformCache
}

// InMemoryCaches builds a Caches value with process-local compilation and transform caches.
func InMemoryCaches() Caches {
	return Caches{
		Compilation: InMemoryCompilationCache,
		Transform:   asyncify.NewMemoryTransformCache(),
	}
}

// RuntimeConfig constructs a wasmrt.Config configured with the compilation and transform caches.
func RuntimeConfig(compilation wazero.CompilationCache, transform asyncify.TransformCache, memoryLimitPages uint32) *wasmrt.Config {
	return &wasmrt.Config{
		CompilationCache:   compilation,
		TransformCache:     transform,
		MemoryLimitPages:   memoryLimitPages,
		CloseOnContextDone: true,
	}
}
