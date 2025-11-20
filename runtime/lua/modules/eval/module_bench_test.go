package eval

import (
	"context"
	"testing"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/registry"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/code"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

func setupBenchCodeManager(b *testing.B) context.Context {
	logger := zap.NewNop()
	bus := &mockEventBus{}
	jsonMod := &mockModule{name: "json"}

	cm, err := code.NewCodeManager(logger, bus, code.Config{
		Modules:        []luaapi.Module{jsonMod},
		ProtoCacheSize: 100,
		MainCacheSize:  50,
	})
	if err != nil {
		b.Fatal(err)
	}

	libNode := code.Node{
		ID:     registry.ID{NS: "app", Name: "mylib"},
		Kind:   luaapi.KindLibrary,
		Source: `return {hello = function() return "world" end}`,
	}
	err = cm.AddNode(context.Background(), libNode, nil)
	if err != nil {
		b.Fatal(err)
	}

	ctx := ctxapi.NewRootContext()
	ac := ctxapi.NewAppContext()
	ctx = ctxapi.WithAppContext(ctx, ac)
	ac.With(luaapi.CodeManagerKey, cm)

	return ctx
}

func BenchmarkBuildIsolatedRunner_Simple(b *testing.B) {
	ctx := setupBenchCodeManager(b)

	config := &evalConfig{
		Source:  "function add(a, b) return a + b end",
		Method:  "add",
		Modules: []string{},
		Imports: map[string]string{},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		runner, err := buildIsolatedRunner(ctx, config)
		if err != nil {
			b.Fatal(err)
		}
		runner.Close()
	}
}

func BenchmarkBuildIsolatedRunner_WithModules(b *testing.B) {
	ctx := setupBenchCodeManager(b)

	config := &evalConfig{
		Source:  "function test_json() local json = require('json'); return json.test end",
		Method:  "test_json",
		Modules: []string{"json"},
		Imports: map[string]string{},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		runner, err := buildIsolatedRunner(ctx, config)
		if err != nil {
			b.Fatal(err)
		}
		runner.Close()
	}
}

func BenchmarkBuildIsolatedRunner_WithImports(b *testing.B) {
	ctx := setupBenchCodeManager(b)

	config := &evalConfig{
		Source:  "function test_lib() local lib = require('helper'); return lib.hello() end",
		Method:  "test_lib",
		Modules: []string{},
		Imports: map[string]string{"helper": "app:mylib"},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		runner, err := buildIsolatedRunner(ctx, config)
		if err != nil {
			b.Fatal(err)
		}
		runner.Close()
	}
}

func BenchmarkBuildIsolatedRunner_Complex(b *testing.B) {
	ctx := setupBenchCodeManager(b)

	config := &evalConfig{
		Source: `
			function process(data)
				local json = require('json')
				local lib = require('helper')
				return lib.hello() .. ':' .. json.test .. ':' .. tostring(data)
			end
		`,
		Method:  "process",
		Modules: []string{"json"},
		Imports: map[string]string{"helper": "app:mylib"},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		runner, err := buildIsolatedRunner(ctx, config)
		if err != nil {
			b.Fatal(err)
		}
		runner.Close()
	}
}

func BenchmarkCompileAndExecute_Simple(b *testing.B) {
	ctx := setupBenchCodeManager(b)

	source := "function add(a, b) return a + b end"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		config := &evalConfig{
			Source:  source,
			Method:  "add",
			Modules: []string{},
			Imports: map[string]string{},
		}

		runner, err := buildIsolatedRunner(ctx, config)
		if err != nil {
			b.Fatal(err)
		}
		runner.Close()
	}
}

func BenchmarkCompileAndExecute_WithModules(b *testing.B) {
	ctx := setupBenchCodeManager(b)

	source := "function test_json() local json = require('json'); return json.test end"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		config := &evalConfig{
			Source:  source,
			Method:  "test_json",
			Modules: []string{"json"},
			Imports: map[string]string{},
		}

		runner, err := buildIsolatedRunner(ctx, config)
		if err != nil {
			b.Fatal(err)
		}
		runner.Close()
	}
}

func BenchmarkCompileAndExecute_Complex(b *testing.B) {
	ctx := setupBenchCodeManager(b)

	source := `
		function process(data)
			local json = require('json')
			local lib = require('helper')
			return lib.hello() .. ':' .. json.test .. ':' .. tostring(data)
		end
	`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		config := &evalConfig{
			Source:  source,
			Method:  "process",
			Modules: []string{"json"},
			Imports: map[string]string{"helper": "app:mylib"},
		}

		runner, err := buildIsolatedRunner(ctx, config)
		if err != nil {
			b.Fatal(err)
		}
		runner.Close()
	}
}

func BenchmarkExecuteOnly_Simple(b *testing.B) {
	ctx := setupBenchCodeManager(b)

	config := &evalConfig{
		Source:  "function add(a, b) return a + b end",
		Method:  "add",
		Modules: []string{},
		Imports: map[string]string{},
	}

	runner, err := buildIsolatedRunner(ctx, config)
	if err != nil {
		b.Fatal(err)
	}
	defer runner.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		execCtx := ctxapi.NewRootContext()
		execCtx, _ = ctxapi.OpenFrameContext(execCtx)

		_, err := runner.Execute(execCtx, "add", lua.LNumber(10), lua.LNumber(20))
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkExecuteOnly_WithModules(b *testing.B) {
	ctx := setupBenchCodeManager(b)

	config := &evalConfig{
		Source:  "function test_json() local json = require('json'); return json.test end",
		Method:  "test_json",
		Modules: []string{"json"},
		Imports: map[string]string{},
	}

	runner, err := buildIsolatedRunner(ctx, config)
	if err != nil {
		b.Fatal(err)
	}
	defer runner.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		execCtx := ctxapi.NewRootContext()
		execCtx, _ = ctxapi.OpenFrameContext(execCtx)

		_, err := runner.Execute(execCtx, "test_json")
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkExecuteOnly_Complex(b *testing.B) {
	ctx := setupBenchCodeManager(b)

	config := &evalConfig{
		Source: `
			function process(data)
				local json = require('json')
				local lib = require('helper')
				return lib.hello() .. ':' .. json.test .. ':' .. tostring(data)
			end
		`,
		Method:  "process",
		Modules: []string{"json"},
		Imports: map[string]string{"helper": "app:mylib"},
	}

	runner, err := buildIsolatedRunner(ctx, config)
	if err != nil {
		b.Fatal(err)
	}
	defer runner.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		execCtx := ctxapi.NewRootContext()
		execCtx, _ = ctxapi.OpenFrameContext(execCtx)

		_, err := runner.Execute(execCtx, "process", lua.LString("test"))
		if err != nil {
			b.Fatal(err)
		}
	}
}
