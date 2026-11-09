// SPDX-License-Identifier: MPL-2.0

package lua

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	glua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/registry"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/code"
	"github.com/wippyai/runtime/runtime/lua/code/cache"
	"github.com/wippyai/runtime/runtime/lua/evalhost"
	"github.com/wippyai/runtime/runtime/lua/modules/json"
	timemod "github.com/wippyai/runtime/runtime/lua/modules/time"
	"go.uber.org/zap"
)

type nopBus struct{}

func (nopBus) Send(context.Context, event.Event) {}
func (nopBus) Subscribe(context.Context, event.System, chan<- event.Event) (event.SubscriberID, error) {
	return "", nil
}
func (nopBus) SubscribeP(context.Context, event.System, event.Kind, chan<- event.Event) (event.SubscriberID, error) {
	return "", nil
}
func (nopBus) HasSubscribers(event.System, event.Kind) bool    { return false }
func (nopBus) Unsubscribe(context.Context, event.SubscriberID) {}

func librarySource(version string) string {
	return fmt.Sprintf(`
local sdk = {}
sdk.version = %q
function sdk.greet(name)
    return "hello " .. name
end
return sdk
`, version)
}

// newEvalCodeManager returns a code manager whose compile cache is enabled, so
// CacheStats counts every library compilation that is not served from the
// retained prototype.
func newEvalCodeManager(t testing.TB) *code.Manager {
	t.Helper()
	cm, err := code.NewCodeManager(zap.NewNop(), nopBus{}, code.Config{
		Modules: []*luaapi.ModuleDef{json.Module, timemod.Module},
		Cache: cache.Config{
			Enabled:           true,
			CompileEnabled:    true,
			Dir:               t.TempDir(),
			ToolchainIdentity: "eval-imports-test",
		},
	})
	require.NoError(t, err)
	return cm
}

func newEvalHost(cm *code.Manager) *evalhost.Host {
	return evalhost.NewHost(zap.NewNop(), cm.ModuleDefsByName).WithImportLoader(evalImportLoader(cm))
}

func addLibrary(t testing.TB, cm *code.Manager, id registry.ID, source string) {
	t.Helper()
	require.NoError(t, cm.AddNode(context.Background(), code.Node{
		ID:     id,
		Kind:   luaapi.Library,
		Source: source,
	}, nil))
}

func compilations(cm *code.Manager) uint64 {
	stats := cm.CacheStats()
	return stats.CompileHits + stats.CompileMisses
}

func runVersionErr(host *evalhost.Host, id registry.ID) (string, error) {
	result, err := host.Run(context.Background(), evalhost.RunCmd{
		Source:  `return { run = function() return sdk.version .. ":" .. sdk.greet("eval") end }`,
		Method:  "run",
		Modules: []string{"json"},
		Imports: map[string]registry.ID{"sdk": id},
	})
	if err != nil {
		return "", err
	}
	str, ok := result.(glua.LString)
	if !ok {
		return "", fmt.Errorf("result should be LString, got %T", result)
	}
	return string(str), nil
}

func runVersion(t testing.TB, host *evalhost.Host, id registry.ID) string {
	t.Helper()
	out, err := runVersionErr(host, id)
	require.NoError(t, err)
	return out
}

func TestEvalImport_UnchangedImportCompilesOnce(t *testing.T) {
	cm := newEvalCodeManager(t)
	id := registry.NewID("app.lib", "sdk")
	addLibrary(t, cm, id, librarySource("1"))
	host := newEvalHost(cm)

	for range 5 {
		require.Equal(t, "1:hello eval", runVersion(t, host, id))
	}
	require.Equal(t, uint64(1), compilations(cm))
}

func TestEvalImport_ChangedImportRecompiles(t *testing.T) {
	cm := newEvalCodeManager(t)
	id := registry.NewID("app.lib", "sdk")
	addLibrary(t, cm, id, librarySource("1"))
	host := newEvalHost(cm)

	require.Equal(t, "1:hello eval", runVersion(t, host, id))
	require.Equal(t, uint64(1), compilations(cm))

	require.NoError(t, cm.UpdateNode(context.Background(), code.Node{
		ID:     id,
		Kind:   luaapi.Library,
		Source: librarySource("2"),
	}, nil))

	require.Equal(t, "2:hello eval", runVersion(t, host, id))
	require.Equal(t, "2:hello eval", runVersion(t, host, id))
	require.Equal(t, uint64(2), compilations(cm))
}

func TestEvalImport_ChangedDependencyRecompilesImport(t *testing.T) {
	cm := newEvalCodeManager(t)
	ctx := context.Background()
	depID := registry.NewID("app.lib", "dep")
	id := registry.NewID("app.lib", "sdk")
	addLibrary(t, cm, depID, `return { n = 1 }`)
	require.NoError(t, cm.AddNode(ctx, code.Node{ID: id, Kind: luaapi.Library, Source: librarySource("1")},
		[]code.Import{{ID: depID, Alias: "dep"}}))
	host := newEvalHost(cm)

	require.Equal(t, "1:hello eval", runVersion(t, host, id))
	compiled := compilations(cm)

	require.NoError(t, cm.UpdateNode(ctx, code.Node{ID: depID, Kind: luaapi.Library, Source: `return { n = 2 }`}, nil))

	require.Equal(t, "1:hello eval", runVersion(t, host, id))
	require.Greater(t, compilations(cm), compiled)
}

func TestEvalImport_ModuleDefsFollowGraph(t *testing.T) {
	cm := newEvalCodeManager(t)
	host := newEvalHost(cm)
	run := func() error {
		_, err := host.Run(context.Background(), evalhost.RunCmd{
			Source:  `return { run = function() return extra.value end }`,
			Method:  "run",
			Modules: []string{"extra"},
		})
		return err
	}
	require.Error(t, run())

	extra := &luaapi.ModuleDef{
		Name:  "extra",
		Class: []string{luaapi.ClassDeterministic},
		Build: func() (*glua.LTable, []luaapi.YieldType) {
			t := glua.CreateTable(0, 1)
			t.RawSetString("value", glua.LString("present"))
			return t, nil
		},
	}
	require.NoError(t, AddModules(context.Background(), cm, extra))
	require.NoError(t, run())

	require.NoError(t, cm.DeleteNode(context.Background(), registry.NewID("", "extra")))
	require.Error(t, run())
}

func TestEvalImport_ConcurrentRunsAndUpdates(t *testing.T) {
	cm := newEvalCodeManager(t)
	id := registry.NewID("app.lib", "sdk")
	addLibrary(t, cm, id, librarySource("0"))
	host := newEvalHost(cm)

	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 25 {
				if _, err := runVersionErr(host, id); err != nil {
					t.Error(err)
					return
				}
			}
		}()
	}
	for v := 1; v <= 10; v++ {
		require.NoError(t, cm.UpdateNode(context.Background(), code.Node{
			ID:     id,
			Kind:   luaapi.Library,
			Source: librarySource(fmt.Sprint(v)),
		}, nil))
	}
	wg.Wait()
	require.Equal(t, "10:hello eval", runVersion(t, host, id))
}

// BenchmarkEvalRunImports runs an eval program that binds n library imports
// against a code graph holding many unrelated library nodes.
func BenchmarkEvalRunImports(b *testing.B) {
	for _, n := range []int{1, 4, 16} {
		b.Run(fmt.Sprintf("imports=%d", n), func(b *testing.B) {
			cm := newEvalCodeManager(b)
			for i := range 2000 {
				addLibrary(b, cm, registry.NewID("app.filler", fmt.Sprintf("lib%d", i)), `return {}`)
			}
			imports := make(map[string]registry.ID, n)
			src := "return { run = function() return 0"
			for i := range n {
				id := registry.NewID("app.lib", fmt.Sprintf("sdk%d", i))
				addLibrary(b, cm, id, librarySource(fmt.Sprint(i)))
				alias := fmt.Sprintf("sdk%d", i)
				imports[alias] = id
				src += fmt.Sprintf(" + #%s.greet('x')", alias)
			}
			src += " end }"
			host := evalhost.NewHost(zap.NewNop(), cm.ModuleDefsByName,
				evalhost.WithProgramCache(evalhost.HostConfig{CacheSize: 16}),
			).WithImportLoader(evalImportLoader(cm))
			cmd := evalhost.RunCmd{Source: src, Method: "run", Modules: []string{"json"}, Imports: imports}
			ctx := context.Background()

			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if _, err := host.Run(ctx, cmd); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
