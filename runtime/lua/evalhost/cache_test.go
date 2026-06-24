// SPDX-License-Identifier: MPL-2.0

package evalhost

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/registry"
	"go.uber.org/zap"
)

func TestHost_CompileCache_ReusesProgram(t *testing.T) {
	host := NewHost(zap.NewNop(), safeModulesProvider(), WithProgramCache(HostConfig{CacheSize: 8}))

	cmd := CompileCmd{
		Source:  `return { f = function() return 1 end }`,
		Method:  "f",
		Modules: []string{"json"},
	}

	p1, err := host.Compile(context.Background(), cmd)
	require.NoError(t, err)
	p2, err := host.Compile(context.Background(), cmd)
	require.NoError(t, err)
	assert.Same(t, p1, p2, "identical source must be served from the compile cache")

	// Different source recompiles to a distinct program.
	p3, err := host.Compile(context.Background(), CompileCmd{
		Source:  `return { f = function() return 2 end }`,
		Method:  "f",
		Modules: []string{"json"},
	})
	require.NoError(t, err)
	assert.NotSame(t, p1, p3, "different source must not reuse a cached program")

	// A different module set is a different compile key.
	p4, err := host.Compile(context.Background(), CompileCmd{
		Source:  cmd.Source,
		Method:  cmd.Method,
		Modules: []string{"json", "time"},
	})
	require.NoError(t, err)
	assert.NotSame(t, p1, p4, "different modules must not reuse a cached program")
}

func TestHost_CompileCache_DisabledRecompiles(t *testing.T) {
	host := NewHost(zap.NewNop(), safeModulesProvider())

	cmd := CompileCmd{
		Source:  `return { f = function() return 1 end }`,
		Method:  "f",
		Modules: []string{"json"},
	}

	p1, err := host.Compile(context.Background(), cmd)
	require.NoError(t, err)
	p2, err := host.Compile(context.Background(), cmd)
	require.NoError(t, err)
	assert.NotSame(t, p1, p2, "with caching disabled every compile must produce a fresh program")
}

func TestHost_CompileCache_ZeroSizeDisabled(t *testing.T) {
	host := NewHost(zap.NewNop(), safeModulesProvider(), WithProgramCache(HostConfig{CacheSize: 0}))
	assert.Nil(t, host.programCache, "a zero cache size must leave caching disabled")
}

func TestHost_CompileCache_KeyDistinguishesMethod(t *testing.T) {
	host := NewHost(zap.NewNop(), safeModulesProvider(), WithProgramCache(HostConfig{CacheSize: 8}))
	src := `return { a = function() return 1 end, b = function() return 2 end }`

	pa, err := host.Compile(context.Background(), CompileCmd{Source: src, Method: "a", Modules: []string{"json"}})
	require.NoError(t, err)
	pb, err := host.Compile(context.Background(), CompileCmd{Source: src, Method: "b", Modules: []string{"json"}})
	require.NoError(t, err)
	assert.NotSame(t, pa, pb, "different method must be a different compile key")
}

func TestHost_CompileCache_KeyDistinguishesAllowClasses(t *testing.T) {
	host := NewHost(zap.NewNop(), safeModulesProvider(), WithProgramCache(HostConfig{CacheSize: 8}))
	src := `return { f = function() return 1 end }`

	p1, err := host.Compile(context.Background(), CompileCmd{Source: src, Method: "f", Modules: []string{"json"}})
	require.NoError(t, err)
	p2, err := host.Compile(context.Background(), CompileCmd{Source: src, Method: "f", Modules: []string{"json"}, AllowClasses: []string{"time"}})
	require.NoError(t, err)
	assert.NotSame(t, p1, p2, "different allow_classes must be a different compile key")
}

func TestHost_CompileCache_OrderIndependentKeys(t *testing.T) {
	host := NewHost(zap.NewNop(), safeModulesProvider(), WithProgramCache(HostConfig{CacheSize: 8}))
	src := `return { f = function() return 1 end }`

	p1, err := host.Compile(context.Background(), CompileCmd{Source: src, Method: "f", Modules: []string{"json", "time"}, AllowClasses: []string{"time", "encoding"}})
	require.NoError(t, err)
	p2, err := host.Compile(context.Background(), CompileCmd{Source: src, Method: "f", Modules: []string{"time", "json"}, AllowClasses: []string{"encoding", "time"}})
	require.NoError(t, err)
	assert.Same(t, p1, p2, "module/class ordering must not change the cache key")
}

func TestHost_CompileCache_KeySeparatorsAvoidCollision(t *testing.T) {
	host := NewHost(zap.NewNop(), safeModulesProvider(), WithProgramCache(HostConfig{CacheSize: 8}))
	src := `return { f = function() return 1 end }`

	// ["json","time"] must not key-collide with a single "jsontime" token.
	p1, err := host.Compile(context.Background(), CompileCmd{Source: src, Method: "f", Modules: []string{"json", "time"}})
	require.NoError(t, err)
	k1 := programCacheKey(CompileCmd{Source: src, Method: "f", Modules: []string{"json", "time"}})
	k2 := programCacheKey(CompileCmd{Source: src, Method: "f", Modules: []string{"jsontime"}})
	assert.NotEqual(t, k1, k2, "concatenated tokens must not collide")
	require.NotNil(t, p1)
}

func TestHost_CompileCache_CompileErrorNotCached(t *testing.T) {
	host := NewHost(zap.NewNop(), safeModulesProvider(), WithProgramCache(HostConfig{CacheSize: 8}))

	bad := CompileCmd{Source: `return function( syntax error`, Method: "f", Modules: []string{"json"}}
	_, err := host.Compile(context.Background(), bad)
	require.Error(t, err)
	assert.Equal(t, 0, host.programCache.Len(), "a failed compile must not be cached")

	// The same bad source still errors (no poisoned nil entry), and a valid one caches.
	_, err = host.Compile(context.Background(), bad)
	require.Error(t, err)
	good := CompileCmd{Source: `return { f = function() return 1 end }`, Method: "f", Modules: []string{"json"}}
	_, err = host.Compile(context.Background(), good)
	require.NoError(t, err)
	assert.Equal(t, 1, host.programCache.Len())
}

func TestHost_CompileCache_AllFitNoEviction(t *testing.T) {
	host := NewHost(zap.NewNop(), safeModulesProvider(), WithProgramCache(HostConfig{CacheSize: 64}))
	cmds := makeEvalSources(60)

	for _, rc := range cmds {
		_, err := host.Compile(context.Background(), CompileCmd{Source: rc.Source, Method: rc.Method, Modules: rc.Modules})
		require.NoError(t, err)
	}
	assert.Equal(t, 60, host.programCache.Len(), "all distinct programs fit and are cached")

	// Second pass: every program is a hit (same pointer).
	first, err := host.Compile(context.Background(), CompileCmd{Source: cmds[0].Source, Method: cmds[0].Method, Modules: cmds[0].Modules})
	require.NoError(t, err)
	again, err := host.Compile(context.Background(), CompileCmd{Source: cmds[0].Source, Method: cmds[0].Method, Modules: cmds[0].Modules})
	require.NoError(t, err)
	assert.Same(t, first, again)
}

func TestHost_CompileCache_EvictionRecompiles(t *testing.T) {
	host := NewHost(zap.NewNop(), safeModulesProvider(), WithProgramCache(HostConfig{CacheSize: 1}))
	a := CompileCmd{Source: `return { f = function() return 1 end }`, Method: "f", Modules: []string{"json"}}
	b := CompileCmd{Source: `return { f = function() return 2 end }`, Method: "f", Modules: []string{"json"}}

	pa1, err := host.Compile(context.Background(), a)
	require.NoError(t, err)
	_, err = host.Compile(context.Background(), b) // evicts a
	require.NoError(t, err)
	pa2, err := host.Compile(context.Background(), a) // a was evicted, recompiles
	require.NoError(t, err)

	assert.NotSame(t, pa1, pa2, "an evicted entry must be recompiled")
	assert.Equal(t, 1, host.programCache.Len(), "size bound is enforced")
}

func TestHost_CompileCache_TTLExpiry(t *testing.T) {
	host := NewHost(zap.NewNop(), safeModulesProvider(), WithProgramCache(HostConfig{CacheSize: 8, CacheTTL: 30 * time.Millisecond}))
	cmd := CompileCmd{Source: `return { f = function() return 1 end }`, Method: "f", Modules: []string{"json"}}

	p1, err := host.Compile(context.Background(), cmd)
	require.NoError(t, err)
	p2, err := host.Compile(context.Background(), cmd)
	require.NoError(t, err)
	assert.Same(t, p1, p2, "within TTL the program is reused")

	time.Sleep(80 * time.Millisecond)
	p3, err := host.Compile(context.Background(), cmd)
	require.NoError(t, err)
	assert.NotSame(t, p1, p3, "after TTL the program is recompiled")
}

func TestHost_CompileCache_ConcurrentSafe(t *testing.T) {
	host := NewHost(zap.NewNop(), safeModulesProvider(), WithProgramCache(HostConfig{CacheSize: 16}))
	cmds := makeEvalSources(8)

	var wg sync.WaitGroup
	for g := range 32 {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := range 200 {
				rc := cmds[(g+i)%len(cmds)]
				if _, err := host.Compile(context.Background(), CompileCmd{Source: rc.Source, Method: rc.Method, Modules: rc.Modules}); err != nil {
					t.Errorf("concurrent compile failed: %v", err)
					return
				}
			}
		}(g)
	}
	wg.Wait()
	assert.LessOrEqual(t, host.programCache.Len(), 16)
}

// TestHost_Run_CachedProgramReusedCorrectly proves the shared cached program
// (one immutable proto) yields correct results across repeated runs.
func TestHost_Run_CachedProgramReusedCorrectly(t *testing.T) {
	host := NewHost(zap.NewNop(), safeModulesProvider(), WithProgramCache(HostConfig{CacheSize: 8}))
	cmd := RunCmd{Source: `return { run = function() return 20 + 3 end }`, Method: "run", Modules: []string{"json"}}

	for range 3 {
		res, err := host.Run(context.Background(), cmd)
		require.NoError(t, err)
		num, ok := res.(lua.LNumber)
		require.True(t, ok, "result should be LNumber, got %T", res)
		assert.Equal(t, lua.LNumber(23), num)
	}
}

// TestHost_Run_ImportsNotInCacheKey proves that imports are bound per run: the
// same source served from the compile cache still binds whichever import the
// run specifies.
func TestHost_Run_ImportsNotInCacheKey(t *testing.T) {
	host := NewHost(zap.NewNop(), safeModulesProvider(), WithProgramCache(HostConfig{CacheSize: 8}))

	libA := registry.ParseID("test.lib:a")
	libB := registry.ParseID("test.lib:b")
	host.WithImportLoader(mockImportLoader(map[registry.ID]string{
		libA: `return { version = "A" }`,
		libB: `return { version = "B" }`,
	}))

	src := `return { run = function() return dep.version end }`

	resA, err := host.Run(context.Background(), RunCmd{
		Source: src, Method: "run", Modules: []string{"json"},
		Imports: map[string]registry.ID{"dep": libA},
	})
	require.NoError(t, err)
	assert.Equal(t, "A", resA.(lua.LString).String())

	// Same source (compile cache hit) but a different import must bind libB.
	resB, err := host.Run(context.Background(), RunCmd{
		Source: src, Method: "run", Modules: []string{"json"},
		Imports: map[string]registry.ID{"dep": libB},
	})
	require.NoError(t, err)
	assert.Equal(t, "B", resB.(lua.LString).String())

	// Only one compiled program exists despite the differing imports.
	assert.Equal(t, 1, host.programCache.Len(), "imports must not be part of the compile key")
}
