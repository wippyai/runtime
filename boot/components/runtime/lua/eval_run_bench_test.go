// SPDX-License-Identifier: MPL-2.0

package lua

import (
	"context"
	"fmt"
	"strings"
	"testing"

	glua "github.com/wippyai/go-lua"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/code"
	"github.com/wippyai/runtime/runtime/lua/code/cache"
	"github.com/wippyai/runtime/runtime/lua/evalhost"
	"github.com/wippyai/runtime/runtime/lua/modules/base64"
	"github.com/wippyai/runtime/runtime/lua/modules/json"
	"github.com/wippyai/runtime/runtime/lua/modules/text"
	timemod "github.com/wippyai/runtime/runtime/lua/modules/time"
	"go.uber.org/zap"
)

// benchSDKSource is a library shaped like a tool SDK: its load defines local
// state and closures, and its functions do the work when called.
const benchSDKSource = `
local json = require("json")
local sdk = { version = "1" }
local resolved = {}
local function spec(name)
    local s = resolved[name]
    if s == nil then
        s = { id = name, schema = { type = "object" } }
        resolved[name] = s
    end
    return s
end
function sdk.tools()
    local out = {}
    for k in pairs(resolved) do out[#out + 1] = k end
    table.sort(out)
    return out
end
function sdk.call(name, args)
    local t = spec(name)
    return { id = t.id, args = args, encoded = json.encode(args) }
end
sdk.meta = setmetatable({}, { __index = function(_, k) return spec(k) end })
return sdk
`

// benchEvalSource is an agent-style program calling into its imports.
func benchEvalSource(imports int, salt int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "-- %d\nreturn { run = function(input)\n", salt)
	b.WriteString("  local acc = {}\n")
	for i := range imports {
		fmt.Fprintf(&b, "  acc[#acc + 1] = sdk%d.call('tool_%d', { n = input.n, k = __cx_args.k })\n", i, i+1)
	}
	b.WriteString("  return { count = #acc, first = acc[1] and acc[1].id, tools = #sdk0.tools(), b = base64.encode('x') }\nend }\n")
	return b.String()
}

func benchArgs() *glua.LTable {
	t := glua.CreateTable(0, 1)
	t.RawSetString("n", glua.LInteger(3))
	return t
}

func newBenchEvalManager(b *testing.B) *code.Manager {
	b.Helper()
	cm, err := code.NewCodeManager(zap.NewNop(), nopBus{}, code.Config{
		Modules: []*luaapi.ModuleDef{json.Module, timemod.Module, base64.Module, text.Module},
		Cache: cache.Config{
			Enabled:           true,
			CompileEnabled:    true,
			Dir:               b.TempDir(),
			ToolchainIdentity: "eval-run-bench",
		},
	})
	if err != nil {
		b.Fatal(err)
	}
	return cm
}

func benchRunCmd(b *testing.B, cm *code.Manager, imports int) evalhost.RunCmd {
	b.Helper()
	cmd := evalhost.RunCmd{
		Method:        "run",
		Modules:       []string{"json", "time", "base64"},
		Imports:       make(map[string]registry.ID, imports),
		ImportModules: map[string][]string{"sdk0": {"json", "time"}},
		Context:       map[string]any{"tools": []any{"tool_1", "tool_2"}},
		CustomModules: map[string]any{"__cx_args": map[string]any{"k": "v"}},
		Args:          payload.Payloads{payload.NewPayload(benchArgs(), payload.Lua)},
		MaxSteps:      1000000,
		MaxStepsSet:   true,
	}
	for i := range imports {
		id := registry.NewID("app.lib", fmt.Sprintf("sdk%d", i))
		addLibrary(b, cm, id, benchSDKSource)
		cmd.Imports[fmt.Sprintf("sdk%d", i)] = id
	}
	return cmd
}

// BenchmarkEvalRunCachedProgram runs one cached program with n imports, three
// modules and a small environment, as a repeated agent tool call does.
func BenchmarkEvalRunCachedProgram(b *testing.B) {
	for _, n := range []int{1, 4} {
		b.Run(fmt.Sprintf("imports=%d", n), func(b *testing.B) {
			cm := newBenchEvalManager(b)
			cmd := benchRunCmd(b, cm, n)
			cmd.Source = benchEvalSource(n, 0)
			host := evalhost.NewHost(zap.NewNop(), cm.ModuleDefsByName,
				evalhost.WithProgramCache(evalhost.HostConfig{CacheSize: 256}),
			).WithImportLoader(evalImportLoader(cm))
			ctx := context.Background()

			b.ReportAllocs()
			for b.Loop() {
				if _, err := host.Run(ctx, cmd); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkEvalRunColdCompile runs a distinct program every iteration, so each
// run compiles its source.
func BenchmarkEvalRunColdCompile(b *testing.B) {
	cm := newBenchEvalManager(b)
	cmd := benchRunCmd(b, cm, 1)
	host := evalhost.NewHost(zap.NewNop(), cm.ModuleDefsByName,
		evalhost.WithProgramCache(evalhost.HostConfig{CacheSize: 256}),
	).WithImportLoader(evalImportLoader(cm))
	ctx := context.Background()

	b.ReportAllocs()
	i := 0
	for b.Loop() {
		i++
		cmd.Source = benchEvalSource(1, i)
		if _, err := host.Run(ctx, cmd); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkEvalRunCachedProgramParallel runs the cached program from all
// procs at once, as concurrent agent calls do.
func BenchmarkEvalRunCachedProgramParallel(b *testing.B) {
	cm := newBenchEvalManager(b)
	cmd := benchRunCmd(b, cm, 1)
	cmd.Source = benchEvalSource(1, 0)
	host := evalhost.NewHost(zap.NewNop(), cm.ModuleDefsByName,
		evalhost.WithProgramCache(evalhost.HostConfig{CacheSize: 256}),
	).WithImportLoader(evalImportLoader(cm))
	ctx := context.Background()

	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if _, err := host.Run(ctx, cmd); err != nil {
				b.Error(err)
				return
			}
		}
	})
}
