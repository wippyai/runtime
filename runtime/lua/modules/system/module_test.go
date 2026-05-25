// SPDX-License-Identifier: MPL-2.0

package system

import (
	"os"
	"testing"

	lua "github.com/wippyai/go-lua"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/security"
)

func TestLoad(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	tbl, yields := Module.Build()
	if tbl == nil {
		t.Fatal("module table is nil")
	}
	if yields != nil {
		t.Error("expected nil yields")
	}

	l.SetGlobal("system", tbl)

	mod := l.GetGlobal("system")
	if mod.Type() != lua.LTTable {
		t.Fatal("module not registered as table")
	}

	// Check child tables exist
	checkTable(t, l, "system", "memory")
	checkTable(t, l, "system", "gc")
	checkTable(t, l, "system", "runtime")
	checkTable(t, l, "system", "process")
	checkTable(t, l, "system", "node")
	checkTable(t, l, "system", "nodes")
	checkTable(t, l, "system", "supervisor")

	// Check functions exist
	checkFunction(t, l, "system", "exit")
	checkFunction(t, l, "system", "modules")
}

func TestLoadReuse(t *testing.T) {
	l1 := lua.NewState()
	defer l1.Close()
	l2 := lua.NewState()
	defer l2.Close()

	tbl1, _ := Module.Build()
	tbl2, _ := Module.Build()

	if tbl1 != tbl2 {
		t.Error("module table should be reused across states")
	}
}

func TestMemoryFunctions(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	ctx := security.SetStrictMode(ctxapi.NewRootContext(), false)
	l.SetContext(ctx)

	tbl, _ := Module.Build()
	l.SetGlobal("system", tbl)

	t.Run("stats", func(t *testing.T) {
		err := l.DoString(`
			local stats, err = system.memory.stats()
			assert(err == nil, "expected nil error")
			assert(type(stats) == "table", "expected table")
			assert(stats.alloc > 0, "alloc should be > 0")
			assert(stats.heap_objects > 0, "heap_objects should be > 0")
			assert(stats.num_gc ~= nil, "num_gc should exist")
		`)
		if err != nil {
			t.Errorf("stats test failed: %v", err)
		}
	})

	t.Run("allocated", func(t *testing.T) {
		err := l.DoString(`
			local alloc, err = system.memory.allocated()
			assert(err == nil, "expected nil error")
			assert(type(alloc) == "number", "expected number")
			assert(alloc > 0, "alloc should be > 0")
		`)
		if err != nil {
			t.Errorf("allocated test failed: %v", err)
		}
	})

	t.Run("heap_objects", func(t *testing.T) {
		err := l.DoString(`
			local objs, err = system.memory.heap_objects()
			assert(err == nil, "expected nil error")
			assert(type(objs) == "number", "expected number")
			assert(objs > 0, "heap_objects should be > 0")
		`)
		if err != nil {
			t.Errorf("heap_objects test failed: %v", err)
		}
	})

	t.Run("memory_limit", func(t *testing.T) {
		err := l.DoString(`
			local limit, err = system.memory.get_limit()
			assert(err == nil, "expected nil error")
			assert(type(limit) == "number", "expected number")
		`)
		if err != nil {
			t.Errorf("get_limit test failed: %v", err)
		}
	})
}

func TestGCFunctions(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	ctx := security.SetStrictMode(ctxapi.NewRootContext(), false)
	l.SetContext(ctx)

	tbl, _ := Module.Build()
	l.SetGlobal("system", tbl)

	t.Run("collect", func(t *testing.T) {
		err := l.DoString(`
			local ok, err = system.gc.collect()
			assert(err == nil, "expected nil error")
			assert(ok == true, "expected true")
		`)
		if err != nil {
			t.Errorf("collect test failed: %v", err)
		}
	})

	t.Run("gc_percent", func(t *testing.T) {
		err := l.DoString(`
			local orig, err = system.gc.get_percent()
			assert(err == nil, "expected nil error")
			assert(type(orig) == "number", "expected number")

			local old, err = system.gc.set_percent(200)
			assert(err == nil, "expected nil error")

			local new, err = system.gc.get_percent()
			assert(err == nil, "expected nil error")
			assert(new == 200, "expected 200, got " .. tostring(new))

			-- restore
			system.gc.set_percent(orig)
		`)
		if err != nil {
			t.Errorf("gc_percent test failed: %v", err)
		}
	})
}

func TestRuntimeFunctions(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	ctx := security.SetStrictMode(ctxapi.NewRootContext(), false)
	l.SetContext(ctx)

	tbl, _ := Module.Build()
	l.SetGlobal("system", tbl)

	t.Run("goroutines", func(t *testing.T) {
		err := l.DoString(`
			local count, err = system.runtime.goroutines()
			assert(err == nil, "expected nil error")
			assert(type(count) == "number", "expected number")
			assert(count > 0, "goroutines should be > 0")
		`)
		if err != nil {
			t.Errorf("goroutines test failed: %v", err)
		}
	})

	t.Run("cpu_count", func(t *testing.T) {
		err := l.DoString(`
			local count, err = system.runtime.cpu_count()
			assert(err == nil, "expected nil error")
			assert(type(count) == "number", "expected number")
			assert(count > 0, "cpu_count should be > 0")
		`)
		if err != nil {
			t.Errorf("cpu_count test failed: %v", err)
		}
	})

	t.Run("max_procs_get", func(t *testing.T) {
		err := l.DoString(`
			local procs, err = system.runtime.max_procs()
			assert(err == nil, "expected nil error")
			assert(type(procs) == "number", "expected number")
			assert(procs > 0, "max_procs should be > 0")
		`)
		if err != nil {
			t.Errorf("max_procs get test failed: %v", err)
		}
	})

	t.Run("max_procs_set", func(t *testing.T) {
		err := l.DoString(`
			local orig, err = system.runtime.max_procs()
			assert(err == nil, "expected nil error")

			local target = orig == 2 and 3 or 2
			local old, err = system.runtime.max_procs(target)
			assert(err == nil, "expected nil error")
			assert(old == orig, "expected old value")

			local new, err = system.runtime.max_procs()
			assert(err == nil, "expected nil error")
			assert(new == target, "expected target value")

			-- restore
			system.runtime.max_procs(orig)
		`)
		if err != nil {
			t.Errorf("max_procs set test failed: %v", err)
		}
	})
}

func TestProcessFunctions(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	ctx := security.SetStrictMode(ctxapi.NewRootContext(), false)
	l.SetContext(ctx)

	tbl, _ := Module.Build()
	l.SetGlobal("system", tbl)

	t.Run("pid", func(t *testing.T) {
		expectedPID := os.Getpid()
		err := l.DoString(`
			local pid, err = system.process.pid()
			assert(err == nil, "expected nil error")
			assert(type(pid) == "number", "expected number")
			assert(pid > 0, "pid should be > 0")
			return pid
		`)
		if err != nil {
			t.Errorf("pid test failed: %v", err)
		}

		gotPID := int(l.Get(-1).(lua.LNumber))
		if gotPID != expectedPID {
			t.Errorf("expected pid %d, got %d", expectedPID, gotPID)
		}
	})

	t.Run("hostname", func(t *testing.T) {
		err := l.DoString(`
			local name, err = system.process.hostname()
			assert(err == nil, "expected nil error")
			assert(type(name) == "string", "expected string")
			assert(#name > 0, "hostname should not be empty")
		`)
		if err != nil {
			t.Errorf("hostname test failed: %v", err)
		}
	})

	t.Run("cwd", func(t *testing.T) {
		expectedCwd, _ := os.Getwd()
		err := l.DoString(`
			local dir, err = system.process.cwd()
			assert(err == nil, "expected nil error")
			assert(type(dir) == "string", "expected string")
			assert(#dir > 0, "cwd should not be empty")
			return dir
		`)
		if err != nil {
			t.Errorf("cwd test failed: %v", err)
		}

		gotCwd := l.Get(-1).String()
		if gotCwd != expectedCwd {
			t.Errorf("expected cwd %q, got %q", expectedCwd, gotCwd)
		}
	})
}

func TestSupervisorFunctions(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	tbl, _ := Module.Build()
	l.SetGlobal("system", tbl)

	t.Run("state_no_context", func(t *testing.T) {
		err := l.DoString(`
			local state, err = system.supervisor.state("test:service")
			assert(state == nil, "expected nil state")
			assert(err ~= nil, "expected error")
		`)
		if err != nil {
			t.Errorf("supervisor.state test failed: %v", err)
		}
	})

	t.Run("states_no_context", func(t *testing.T) {
		err := l.DoString(`
			local states, err = system.supervisor.states()
			assert(states == nil, "expected nil states")
			assert(err ~= nil, "expected error")
		`)
		if err != nil {
			t.Errorf("supervisor.states test failed: %v", err)
		}
	})
}

func TestExitFunction(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	tbl, _ := Module.Build()
	l.SetGlobal("system", tbl)

	// Without proper context, exit will fail
	err := l.DoString(`
		local ok, err = system.exit()
		-- May succeed or fail depending on context
		return ok, err
	`)
	if err != nil {
		t.Errorf("exit test failed: %v", err)
	}
}

func TestModulesFunction(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	tbl, _ := Module.Build()
	l.SetGlobal("system", tbl)

	// Without code manager context, returns error
	err := l.DoString(`
		local mods, err = system.modules()
		assert(mods == nil, "expected nil without code manager")
		assert(err ~= nil, "expected error")
	`)
	if err != nil {
		t.Errorf("modules test failed: %v", err)
	}
}

func TestErrorKinds(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	lua.OpenErrors(l)

	tbl, _ := Module.Build()
	l.SetGlobal("system", tbl)

	// Test structured errors for validation failures
	t.Run("set_gc_percent_no_arg", func(t *testing.T) {
		err := l.DoString(`
			local result, err = system.gc.set_percent()
			assert(result == nil, "expected nil result")
			assert(err ~= nil, "expected error")
			assert(err:kind() == errors.INVALID, "expected INVALID kind, got: " .. tostring(err:kind()))
			assert(err:retryable() == false, "expected not retryable")
		`)
		if err != nil {
			t.Errorf("set_gc_percent error test failed: %v", err)
		}
	})

	t.Run("set_memory_limit_no_arg", func(t *testing.T) {
		err := l.DoString(`
			local result, err = system.memory.set_limit()
			assert(result == nil, "expected nil result")
			assert(err ~= nil, "expected error")
			assert(err:kind() == errors.INVALID, "expected INVALID kind")
		`)
		if err != nil {
			t.Errorf("set_memory_limit error test failed: %v", err)
		}
	})

	t.Run("max_procs_invalid", func(t *testing.T) {
		err := l.DoString(`
			local result, err = system.runtime.max_procs(0)
			assert(result == nil, "expected nil result")
			assert(err ~= nil, "expected error")
			assert(err:kind() == errors.INVALID, "expected INVALID kind")
		`)
		if err != nil {
			t.Errorf("max_procs invalid test failed: %v", err)
		}
	})
}

func checkTable(t *testing.T, l *lua.LState, _, name string) {
	t.Helper()
	err := l.DoString(`return type(system.` + name + `) == "table"`)
	if err != nil {
		t.Errorf("error checking system.%s: %v", name, err)
		return
	}
	if l.Get(-1) != lua.LTrue {
		t.Errorf("system.%s is not a table", name)
	}
	l.Pop(1)
}

func checkFunction(t *testing.T, l *lua.LState, parent, name string) {
	t.Helper()
	err := l.DoString(`return type(` + parent + `.` + name + `) == "function"`)
	if err != nil {
		t.Errorf("error checking %s.%s: %v", parent, name, err)
		return
	}
	if l.Get(-1) != lua.LTrue {
		t.Errorf("%s.%s is not a function", parent, name)
	}
	l.Pop(1)
}
