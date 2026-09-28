// SPDX-License-Identifier: MPL-2.0

package system

import (
	"context"
	"errors"
	"os"
	"testing"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/attrs"
	ctxapi "github.com/wippyai/runtime/api/context"
	moduleapi "github.com/wippyai/runtime/api/modules"
	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/security"
	apiversion "github.com/wippyai/runtime/api/version"
	secsystem "github.com/wippyai/runtime/system/security"
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
	checkTable(t, l, "system", "cluster")
	checkTable(t, l, "system", "raft")
	checkTable(t, l, "system", "lock")
	checkTable(t, l, "system", "supervisor")
	checkTable(t, l, "system", "source")

	// Check functions exist
	checkFunction(t, l, "system", "exit")
	checkFunction(t, l, "system", "modules")
	checkFunction(t, l, "system", "version")
}

func TestVersionReturnsBuildIdentity(t *testing.T) {
	original := apiversion.Version
	t.Cleanup(func() { apiversion.Version = original })

	tbl, _ := Module.Build()
	states := []*lua.LState{lua.NewState(), lua.NewState()}
	for _, l := range states {
		defer l.Close()
		l.SetGlobal("system", tbl)
	}

	for _, test := range []struct {
		name  string
		stamp string
	}{
		{name: "v-prefixed release", stamp: "v0.3.43a"},
		{name: "bare release", stamp: "0.3.43a"},
		{name: "semver prerelease and build", stamp: "v1.2.3-rc.10+build.7"},
		{name: "development", stamp: "dev"},
		{name: "development description", stamp: "dev-v0.3.43a-5-gabcdef0-dirty"},
		{name: "nightly", stamp: "nightly-20260925-3f84d88"},
		{name: "empty", stamp: ""},
		{name: "malformed", stamp: "not a version"},
	} {
		t.Run(test.name, func(t *testing.T) {
			previous := apiversion.Version
			t.Cleanup(func() { apiversion.Version = previous })
			apiversion.Version = test.stamp

			for _, l := range states {
				l.SetGlobal("expectedVersion", lua.LString(test.stamp))
				if err := l.DoString(`
					local count = select("#", system.version())
					local value = system.version()
					assert(count == 1, "version returns exactly one value")
					assert(value ~= nil, "version is not nil")
					assert(type(value) == "string", "version is a string")
					assert(value == expectedVersion, "version matches the build identity")
					local with_extra = system.version("ignored", 42)
					assert(with_extra == expectedVersion, "extra arguments do not change the version")
					local first, second = system.version()
					assert(first == expectedVersion and second == nil, "version has no second result")
				`); err != nil {
					t.Fatalf("system.version() failed in a Lua state: %v", err)
				}
			}
		})
	}
}

func TestVersionLinkedStamp(t *testing.T) {
	expected := os.Getenv("WIPPY_TEST_EXPECT_VERSION")
	if expected == "" {
		expected = apiversion.Short()
	}

	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal("system", tbl)
	l.SetGlobal("expectedVersion", lua.LString(expected))
	if err := l.DoString(`
		local count = select("#", system.version())
		local version = system.version()
		assert(count == 1)
		assert(type(version) == "string")
		assert(version == expectedVersion)
	`); err != nil {
		t.Fatal(err)
	}
}

func TestVersionWithoutSystemRead(t *testing.T) {
	contexts := []struct {
		ctx  context.Context
		name string
	}{
		{name: "strict without actor or scope", ctx: security.SetStrictMode(ctxapi.NewRootContext(), true)},
		{name: "deny all policy", ctx: denyContext(t)},
	}

	for _, test := range contexts {
		t.Run(test.name, func(t *testing.T) {
			l := lua.NewState()
			defer l.Close()
			l.SetContext(test.ctx)

			tbl, _ := Module.Build()
			l.SetGlobal("system", tbl)
			l.SetGlobal("expectedVersion", lua.LString(apiversion.Short()))
			if err := l.DoString(`
				local value = system.version()
				assert(type(value) == "string")
				assert(value == expectedVersion)
			`); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestVersionModuleImmutable(t *testing.T) {
	tbl, _ := Module.Build()
	l1 := lua.NewState()
	defer l1.Close()
	l1.SetGlobal("system", tbl)
	if err := l1.DoString(`system.version = nil`); err == nil {
		t.Fatal("expected assigning to system.version to fail")
	}

	l2 := lua.NewState()
	defer l2.Close()
	l2.SetGlobal("system", tbl)
	if err := l2.DoString(`assert(type(system.version) == "function")`); err != nil {
		t.Fatalf("system.version was changed by another state: %v", err)
	}
}

func TestSourceLoadReturnsAtomicOwnersAndEntriesWithoutPaths(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	ctx := security.SetStrictMode(ctxapi.NewRootContext(), false)
	sources := moduleapi.NewSourceRegistry()
	sources.Set(moduleapi.Sources{
		moduleapi.ApplicationSourceID: {LoadPath: "/private/app", Owner: moduleapi.ApplicationSourceID},
		"acme/zeta":                   {LoadPath: "/private/zeta", ResourceRoot: "/private/zeta", Owner: "acme/zeta"},
		"acme/alpha":                  {LoadPath: "/private/alpha", ResourceRoot: "/private/alpha", Owner: "acme/alpha"},
		"acme/packed":                 {LoadPath: "/private/packed.wapp"},
	})
	sources.SetLoader(func(context.Context, moduleapi.Sources) ([]regapi.Entry, error) {
		return []regapi.Entry{{ID: regapi.NewID("example.source", "probe"), Kind: "registry.entry"}}, nil
	})
	ctx = moduleapi.WithSourceRegistry(ctx, sources)
	l.SetContext(ctx)

	tbl, _ := Module.Build()
	l.SetGlobal("system", tbl)
	if err := l.DoString(`
		local sources, err = system.source.load()
		assert(err == nil)
		assert(#sources.owners == 3)
		assert(sources.owners[1] == "application")
		assert(sources.owners[2] == "acme/alpha")
		assert(sources.owners[3] == "acme/zeta")
		assert(#sources.entries == 1)
		assert(sources.entries[1].id == "example.source:probe")
		for _, owner in ipairs(sources.owners) do
			assert(not string.find(owner, "/private", 1, true))
		end
	`); err != nil {
		t.Fatal(err)
	}
}

func TestSourceLoadRequiresPermission(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	ctx := security.SetStrictMode(ctxapi.NewRootContext(), true)
	l.SetContext(ctx)

	tbl, _ := Module.Build()
	l.SetGlobal("system", tbl)
	if err := l.DoString(`
		local sources, err = system.source.load()
		assert(sources == nil)
		assert(err ~= nil)
	`); err != nil {
		t.Fatal(err)
	}
}

func TestSourceLoadDoesNotExposePrivatePathInError(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	ctx := security.SetStrictMode(ctxapi.NewRootContext(), false)
	sources := moduleapi.NewSourceRegistry()
	sources.Set(moduleapi.Sources{
		moduleapi.ApplicationSourceID: {
			LoadPath: "/private/application/source",
			Owner:    moduleapi.ApplicationSourceID,
		},
	})
	sources.SetLoader(func(context.Context, moduleapi.Sources) ([]regapi.Entry, error) {
		return nil, errors.New("open /private/application/source/_index.yaml: permission denied")
	})
	ctx = moduleapi.WithSourceRegistry(ctx, sources)
	l.SetContext(ctx)

	tbl, _ := Module.Build()
	l.SetGlobal("system", tbl)
	if err := l.DoString(`
		local loaded, err = system.source.load()
		assert(loaded == nil)
		assert(err ~= nil)
		assert(err:message() == "failed to load deployment sources")
		assert(not string.find(tostring(err), "/private", 1, true))
	`); err != nil {
		t.Fatal(err)
	}
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
	lua.OpenErrors(l)

	tbl, _ := Module.Build()
	l.SetGlobal("system", tbl)

	t.Run("state_no_context", func(t *testing.T) {
		err := l.DoString(`
			local state, err = system.supervisor.state("test:service")
			assert(state == nil, "expected nil state")
			assert(err ~= nil, "expected error")
			assert(err:kind() == errors.PERMISSION_DENIED, "expected PERMISSION_DENIED kind, got: " .. tostring(err:kind()))
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
			assert(err:kind() == errors.PERMISSION_DENIED, "expected PERMISSION_DENIED kind, got: " .. tostring(err:kind()))
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

	ctx := security.SetStrictMode(ctxapi.NewRootContext(), false)
	l.SetContext(ctx)

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

func TestSystemPermissionDenied(t *testing.T) {
	calls := []string{
		`system.memory.stats()`,
		`system.memory.allocated()`,
		`system.memory.heap_objects()`,
		`system.memory.set_limit(1 << 30)`,
		`system.memory.get_limit()`,
		`system.gc.collect()`,
		`system.gc.set_percent(100)`,
		`system.gc.get_percent()`,
		`system.runtime.goroutines()`,
		`system.runtime.max_procs(2)`,
		`system.runtime.max_procs()`,
		`system.runtime.cpu_count()`,
		`system.process.pid()`,
		`system.process.cwd()`,
		`system.process.hostname()`,
		`system.exit()`,
		`system.modules()`,
		`system.source.load()`,
		`system.supervisor.state("test:service")`,
		`system.supervisor.states()`,
		`system.hosts.list()`,
		`system.hosts.processes("test:host")`,
	}

	for _, call := range calls {
		t.Run(call, func(t *testing.T) {
			l := lua.NewState()
			defer l.Close()
			lua.OpenErrors(l)

			l.SetContext(denyContext(t))

			tbl, _ := Module.Build()
			l.SetGlobal("system", tbl)

			if err := l.DoString(`
				local v, err = ` + call + `
				assert(v == nil, "expected nil result under a deny policy")
				assert(err ~= nil, "expected error under a deny policy")
				assert(err:kind() == errors.PERMISSION_DENIED, "expected PERMISSION_DENIED kind, got: " .. tostring(err:kind()))
				assert(err:retryable() == false, "expected not retryable")
			`); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// denyContext builds a security context whose scope denies every action.
func denyContext(t *testing.T) context.Context {
	t.Helper()

	ctx := security.SetStrictMode(ctxapi.NewRootContext(), false)
	ctx, fc := ctxapi.OpenFrameContext(ctx)
	t.Cleanup(func() { ctxapi.ReleaseFrameContext(fc) })

	if err := security.SetActor(ctx, security.Actor{ID: "tester"}); err != nil {
		t.Fatal(err)
	}
	if err := security.SetScope(ctx, secsystem.NewScope([]security.Policy{denyAllPolicy{}})); err != nil {
		t.Fatal(err)
	}
	return ctx
}

type denyAllPolicy struct{}

func (denyAllPolicy) ID() regapi.ID { return regapi.NewID("test", "deny-all") }

func (denyAllPolicy) Evaluate(_ security.Actor, _, _ string, _ attrs.Bag) security.Result {
	return security.Deny
}
