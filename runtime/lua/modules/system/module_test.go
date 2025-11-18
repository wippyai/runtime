package system

import (
	"context"
	"os"
	"syscall"
	"testing"
	"time"

	ctxapi "github.com/wippyai/runtime/api/context"
	systemapi "github.com/wippyai/runtime/api/system"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/runtime/lua/engine"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

func newTestContext() context.Context {
	ctx := ctxapi.NewRootContext()
	ctx, _ = ctxapi.OpenFrameContext(ctx)
	return ctx
}

func TestSystemModuleWithVM(t *testing.T) {
	logger := zap.NewNop()

	t.Run("module creation and loading", func(t *testing.T) {
		mod := NewSystemModule()
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(newTestContext(), `
			local system = require("system")
			assert(type(system) == "table")
			assert(type(system.mem_stats) == "function")
			assert(type(system.allocated) == "function")
			assert(type(system.heap_objects) == "function")
			assert(type(system.gc) == "function")
			assert(type(system.set_gc_percent) == "function")
			assert(type(system.get_gc_percent) == "function")
			assert(type(system.num_goroutines) == "function")
			assert(type(system.go_max_procs) == "function")
			assert(type(system.num_cpu) == "function")
			assert(type(system.hostname) == "function")
			assert(type(system.pid) == "function")
		`, "test")
		assert.NoError(t, err)
	})

	t.Run("mem_stats function", func(t *testing.T) {
		mod := NewSystemModule()
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		script := `
			local system = require("system")
			function test()
				local stats, err = system.mem_stats()
				if err then
					return nil, err
				end
				
				-- Check some basic expectations
				if type(stats) ~= "table" then
					return nil, "expected table result"
				end
				
				if stats.alloc == nil or stats.heap_objects == nil or stats.num_gc == nil then
					return nil, "missing expected fields"
				end
				
				return {
					has_alloc = stats.alloc > 0,
					has_heap_objects = stats.heap_objects > 0
				}
			end
			return test
		`
		err = vm.Import(script, "test", "test")
		require.NoError(t, err)

		result, err := vm.Execute(newTestContext(), "test")
		require.NoError(t, err)
		require.IsType(t, &lua.LTable{}, result)

		tbl := result.(*lua.LTable)
		hasAlloc := tbl.RawGetString("has_alloc").(lua.LBool)
		hasHeapObjects := tbl.RawGetString("has_heap_objects").(lua.LBool)

		assert.True(t, bool(hasAlloc), "mem_stats should return alloc > 0")
		assert.True(t, bool(hasHeapObjects), "mem_stats should return heap_objects > 0")
	})

	t.Run("gc and allocated functions", func(t *testing.T) {
		mod := NewSystemModule()
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		script := `
			local system = require("system")
			function test()
				-- Get current allocation
				local alloc_before, err = system.allocated()
				if err then
					return nil, err
				end
				
				-- Force garbage collection
				local gc_success, err = system.gc()
				if err then
					return nil, err
				end
				
				-- Get allocation after GC
				local alloc_after, err = system.allocated()
				if err then
					return nil, err
				end
				
				return {
					gc_success = gc_success,
					alloc_before = alloc_before,
					alloc_after = alloc_after
				}
			end
			return test
		`
		err = vm.Import(script, "test", "test")
		require.NoError(t, err)

		result, err := vm.Execute(newTestContext(), "test")
		require.NoError(t, err)
		require.IsType(t, &lua.LTable{}, result)

		tbl := result.(*lua.LTable)
		gcSuccess := tbl.RawGetString("gc_success").(lua.LBool)
		allocBefore := float64(tbl.RawGetString("alloc_before").(lua.LNumber))
		allocAfter := float64(tbl.RawGetString("alloc_after").(lua.LNumber))

		assert.True(t, bool(gcSuccess), "gc() should return true")
		assert.True(t, allocBefore > 0, "allocated() should return positive value")
		assert.True(t, allocAfter > 0, "allocated() should return positive value after GC")
	})

	t.Run("gc_percent functions", func(t *testing.T) {
		mod := NewSystemModule()
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		script := `
			local system = require("system")
			function test()
				-- Get current GC percent
				local original, err = system.get_gc_percent()
				if err then
					return nil, err
				end
				
				-- Set new value
				local old, err = system.set_gc_percent(200)
				if err then
					return nil, err
				end
				
				-- Get updated value
				local new, err = system.get_gc_percent()
				if err then
					return nil, err
				end
				
				-- Restore original value
				system.set_gc_percent(original)
				
				return {
					original = original,
					old_from_set = old,
					new_value = new
				}
			end
			return test
		`
		err = vm.Import(script, "test", "test")
		require.NoError(t, err)

		result, err := vm.Execute(newTestContext(), "test")
		require.NoError(t, err)
		require.IsType(t, &lua.LTable{}, result)

		tbl := result.(*lua.LTable)
		original := int(tbl.RawGetString("original").(lua.LNumber))
		oldFromSet := int(tbl.RawGetString("old_from_set").(lua.LNumber))
		newValue := int(tbl.RawGetString("new_value").(lua.LNumber))

		// Both should be normalized to 100
		assert.Equal(t, 100, original, "get_gc_percent should normalize initial value")
		assert.Equal(t, 100, oldFromSet, "set_gc_percent should return normalized old value")
		assert.Equal(t, 200, newValue, "get_gc_percent should return new value")
	})

	t.Run("system info functions", func(t *testing.T) {
		mod := NewSystemModule()
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		script := `
			local system = require("system")
			function test()
				-- Get number of goroutines
				local goroutines, err = system.num_goroutines()
				if err then
					return nil, err
				end
				
				-- Get number of CPUs
				local cpus, err = system.num_cpu()
				if err then
					return nil, err
				end
				
				-- Get GOMAXPROCS
				local procs, err = system.go_max_procs()
				if err then
					return nil, err
				end
				
				-- Get hostname
				local hostname, err = system.hostname()
				if err then
					return nil, err
				end
				
				-- Get PID
				local pid, err = system.pid()
				if err then
					return nil, err
				end
				
				return {
					goroutines = goroutines,
					cpus = cpus,
					procs = procs,
					hostname_type = type(hostname),
					hostname_exists = hostname ~= nil and hostname ~= "",
					pid = pid
				}
			end
			return test
		`
		err = vm.Import(script, "test", "test")
		require.NoError(t, err)

		result, err := vm.Execute(newTestContext(), "test")
		require.NoError(t, err)
		require.IsType(t, &lua.LTable{}, result)

		tbl := result.(*lua.LTable)
		goroutines := int(tbl.RawGetString("goroutines").(lua.LNumber))
		cpus := int(tbl.RawGetString("cpus").(lua.LNumber))
		procs := int(tbl.RawGetString("procs").(lua.LNumber))
		hostnameType := tbl.RawGetString("hostname_type").String()
		hostnameExists := tbl.RawGetString("hostname_exists").(lua.LBool)
		pid := int(tbl.RawGetString("pid").(lua.LNumber))

		assert.True(t, goroutines > 0, "num_goroutines should return positive count")
		assert.True(t, cpus > 0, "num_cpu should return positive count")
		assert.True(t, procs > 0, "go_max_procs should return positive count")
		assert.Equal(t, "string", hostnameType, "hostname should return string")
		assert.True(t, bool(hostnameExists), "hostname should return non-empty string")
		assert.True(t, pid > 0, "pid should return positive number")
	})

	t.Run("go_max_procs set function", func(t *testing.T) {
		mod := NewSystemModule()
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		script := `
			local system = require("system")
			function test()
				-- Get current GOMAXPROCS
				local original, err = system.go_max_procs()
				if err then
					return nil, err
				end
				
				-- Set to 2 (or current if already 2)
				local target = original == 2 and 3 or 2
				local old, err = system.go_max_procs(target)
				if err then
					return nil, err
				end
				
				-- Get new value
				local new, err = system.go_max_procs()
				if err then
					return nil, err
				end
				
				-- Restore original
				system.go_max_procs(original)
				
				return {
					original = original,
					old_returned = old,
					new_value = new,
					target_value = target
				}
			end
			return test
		`
		err = vm.Import(script, "test", "test")
		require.NoError(t, err)

		result, err := vm.Execute(newTestContext(), "test")
		require.NoError(t, err)
		require.IsType(t, &lua.LTable{}, result)

		tbl := result.(*lua.LTable)
		original := int(tbl.RawGetString("original").(lua.LNumber))
		oldReturned := int(tbl.RawGetString("old_returned").(lua.LNumber))
		newValue := int(tbl.RawGetString("new_value").(lua.LNumber))
		targetValue := int(tbl.RawGetString("target_value").(lua.LNumber))

		assert.Equal(t, original, oldReturned, "go_max_procs should return old value")
		assert.Equal(t, targetValue, newValue, "go_max_procs should have changed to target value")
	})

	t.Run("exit function", func(t *testing.T) {
		mod := NewSystemModule()
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		ctx := newTestContext()

		sigChan := make(chan os.Signal, 1)
		systemapi.SetSignalChannel(ctx, sigChan)

		go func() {
			err = vm.DoString(ctx, `
				local system = require("system")
				local result, err = system.exit(42)
				assert(result == true)
				assert(err == nil)
			`, "test")
			require.NoError(t, err)
		}()

		select {
		case sig := <-sigChan:
			assert.Equal(t, syscall.SIGTERM, sig)
		case <-time.After(100 * time.Millisecond):
			t.Fatal("Expected signal not received")
		}

		exitCode := systemapi.GetExitCode(ctx)
		assert.Equal(t, 42, exitCode)
	})

	t.Run("exit function default code", func(t *testing.T) {
		mod := NewSystemModule()
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		ctx := newTestContext()

		sigChan := make(chan os.Signal, 1)
		systemapi.SetSignalChannel(ctx, sigChan)

		go func() {
			err = vm.DoString(ctx, `
				local system = require("system")
				local result, err = system.exit()
				assert(result == true)
				assert(err == nil)
			`, "test")
			require.NoError(t, err)
		}()

		select {
		case sig := <-sigChan:
			assert.Equal(t, syscall.SIGTERM, sig)
		case <-time.After(100 * time.Millisecond):
			t.Fatal("Expected signal not received")
		}

		exitCode := systemapi.GetExitCode(ctx)
		assert.Equal(t, 0, exitCode)
	})
}
