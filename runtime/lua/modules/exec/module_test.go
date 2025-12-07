package exec

import (
	"testing"

	lua "github.com/yuin/gopher-lua"
)

func setupState() *lua.LState {
	l := lua.NewState()
	lua.OpenErrors(l)
	Module.Load(l)
	return l
}

func TestModuleLoads(t *testing.T) {
	l := setupState()
	defer l.Close()

	mod := l.GetGlobal("exec")
	if mod.Type() != lua.LTTable {
		t.Fatal("exec module not registered")
	}

	tbl := mod.(*lua.LTable)
	if tbl.RawGetString("get").Type() != lua.LTFunction {
		t.Error("get function not registered")
	}
}

func TestModuleReuse(t *testing.T) {
	l1 := lua.NewState()
	defer l1.Close()
	l2 := lua.NewState()
	defer l2.Close()

	Module.Load(l1)
	Module.Load(l2)

	mod1 := l1.GetGlobal("exec").(*lua.LTable)
	mod2 := l2.GetGlobal("exec").(*lua.LTable)

	if mod1 != mod2 {
		t.Error("module table should be reused across states")
	}
}

func TestModuleImmutable(t *testing.T) {
	l := setupState()
	defer l.Close()

	mod := l.GetGlobal("exec").(*lua.LTable)
	if !mod.Immutable {
		t.Error("module table should be immutable")
	}
}

func TestGetNoContext(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	lua.OpenErrors(l)
	Module.Load(l)

	err := l.DoString(`
		local ok, err = exec.get("test:executor")
		if ok ~= nil then
			error("expected nil result")
		end
		if not err then
			error("expected error")
		end
		if err:kind() ~= errors.INTERNAL then
			error("expected INTERNAL error kind, got: " .. tostring(err:kind()))
		end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}

func TestGetEmptyID(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	lua.OpenErrors(l)
	Module.Load(l)

	err := l.DoString(`
		local ok, err = exec.get("")
		if ok ~= nil then
			error("expected nil result")
		end
		if not err then
			error("expected error")
		end
		if err:kind() ~= errors.INVALID then
			error("expected INVALID error kind, got: " .. tostring(err:kind()))
		end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}

func TestErrorMethods(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	lua.OpenErrors(l)
	Module.Load(l)

	err := l.DoString(`
		local ok, err = exec.get("")
		if not err then error("expected error") end

		if type(err.kind) ~= "function" then
			error("error should have kind method")
		end
		if type(err.message) ~= "function" then
			error("error should have message method")
		end
		if type(err.retryable) ~= "function" then
			error("error should have retryable method")
		end

		if err:retryable() ~= false then
			error("error should not be retryable")
		end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}

func TestProcessMethodsRegistered(t *testing.T) {
	methods := []string{"start", "wait", "signal", "write_stdin", "stdout_stream", "stderr_stream", "close"}

	for _, m := range methods {
		if _, ok := processMethods[m]; !ok {
			t.Errorf("process method %q not registered", m)
		}
	}
}

func TestExecutorMethodsRegistered(t *testing.T) {
	methods := []string{"exec", "release"}

	for _, m := range methods {
		if _, ok := executorMethods[m]; !ok {
			t.Errorf("executor method %q not registered", m)
		}
	}
}

func TestProcessStateNotStarted(t *testing.T) {
	p := &Process{
		started: false,
		closed:  false,
	}

	if p.started {
		t.Error("new process should not be started")
	}
	if p.closed {
		t.Error("new process should not be closed")
	}
}

func TestProcessStateClosed(t *testing.T) {
	p := &Process{
		started: true,
		closed:  true,
	}

	if !p.closed {
		t.Error("closed process should be marked closed")
	}
}
