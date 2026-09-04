// SPDX-License-Identifier: MPL-2.0

package exec

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	lua "github.com/wippyai/go-lua"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/registry"
	securityapi "github.com/wippyai/runtime/api/security"
	execapi "github.com/wippyai/runtime/api/service/exec"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
)

func setupState() *lua.LState {
	l := lua.NewState()
	lua.OpenErrors(l)
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)
	return l
}

func TestModuleLoads(t *testing.T) {
	l := setupState()
	defer l.Close()

	mod := l.GetGlobal("exec")
	if mod.Type() != lua.LTTable {
		t.Fatal("exec module not registered")
	}

	modTbl := mod.(*lua.LTable)
	if modTbl.RawGetString("get").Type() != lua.LTFunction {
		t.Error("get function not registered")
	}
}

func TestModuleReuse(t *testing.T) {
	l1 := lua.NewState()
	defer l1.Close()
	l2 := lua.NewState()
	defer l2.Close()

	tbl, _ := Module.Build()
	l1.SetGlobal(Module.Name, tbl)
	l2.SetGlobal(Module.Name, tbl)

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
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	// Without context, security strict mode blocks access with INVALID (permission denied)
	err := l.DoString(`
		local ok, err = exec.get("test:executor")
		if ok ~= nil then
			error("expected nil result")
		end
		if not err then
			error("expected error")
		end
		if err:kind() ~= errors.INVALID then
			error("expected INVALID error kind (security denial), got: " .. tostring(err:kind()))
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
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

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
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

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

func TestNewExecutor(t *testing.T) {
	ctx := context.Background()
	mockRes := &mockResource{}
	mockFactory := &mockProcessExecutor{}

	e := NewExecutor(ctx, mockRes, mockFactory)

	if e == nil {
		t.Fatal("expected executor to be created")
		return
	}
	if e.resource != mockRes {
		t.Error("executor resource mismatch")
	}
	if e.factory != mockFactory {
		t.Error("executor factory mismatch")
	}
	if e.released {
		t.Error("new executor should not be released")
	}
}

func TestExecutorReleaseWithoutResource(t *testing.T) {
	e := &Executor{
		resource: nil,
		released: true,
	}

	l := setupState()
	defer l.Close()

	value.PushTypedUserData(l, e, executorTypeName)

	executorRelease(l)

	result := l.ToBool(-2)
	if !result {
		t.Error("release should return true even without resource")
	}
}

func TestExecutorReleaseTwice(t *testing.T) {
	mockRes := &mockResource{}
	e := &Executor{
		resource: mockRes,
		released: false,
	}

	l := setupState()
	defer l.Close()

	value.PushTypedUserData(l, e, executorTypeName)
	l.Push(lua.LNil)

	executorRelease(l)
	result1 := l.ToBool(-2)
	l.Pop(2)

	if !result1 {
		t.Error("first release should return true")
	}
	if !mockRes.released {
		t.Error("resource should be released")
	}

	value.PushTypedUserData(l, e, executorTypeName)
	l.Push(lua.LNil)

	executorRelease(l)
	result2 := l.ToBool(-2)

	if !result2 {
		t.Error("second release should return true")
	}
}

func TestCheckExecutor(t *testing.T) {
	l := setupState()
	defer l.Close()

	e := &Executor{}
	value.PushTypedUserData(l, e, executorTypeName)

	result := checkExecutor(l, 1)
	if result != e {
		t.Error("checkExecutor should return the executor")
	}
}

func TestCheckProcess(t *testing.T) {
	l := setupState()
	defer l.Close()

	p := &Process{}
	value.PushTypedUserData(l, p, processTypeName)

	result := checkProcess(l, 1)
	if result != p {
		t.Error("checkProcess should return the process")
	}
}

func TestNewProcess(t *testing.T) {
	ctx := context.Background()
	mockHandle := &mockProcess{}

	p := NewProcess(ctx, mockHandle)

	if p == nil {
		t.Fatal("expected process to be created")
		return
	}
	if p.handle == nil {
		t.Error("process handle should be set")
	}
	if p.started {
		t.Error("new process should not be started")
	}
	if p.closed {
		t.Error("new process should not be closed")
	}
}

func TestProcessDoubleClose(t *testing.T) {
	mockHandle := &mockProcess{}
	p := &Process{
		handle: mockHandle,
		closed: false,
	}

	l := setupState()
	defer l.Close()

	value.PushTypedUserData(l, p, processTypeName)
	procClose(l)
	l.Pop(2)

	value.PushTypedUserData(l, p, processTypeName)
	procClose(l)

	result := l.ToBool(-2)
	if !result {
		t.Error("second close should still return true")
	}
}

func TestProcessCloseWithForce(t *testing.T) {
	mockHandle := &mockProcess{}
	p := &Process{
		handle: mockHandle,
		closed: false,
	}

	l := setupState()
	defer l.Close()

	value.PushTypedUserData(l, p, processTypeName)
	l.Push(lua.LTrue)

	procClose(l)

	if !p.closed {
		t.Error("process should be closed")
	}
	if mockHandle.signalCalled != 1 {
		t.Errorf("signal should be called once, got %d", mockHandle.signalCalled)
	}
}

func TestProcessCloseAlreadyClosed(t *testing.T) {
	p := &Process{
		closed: true,
	}

	l := setupState()
	defer l.Close()

	value.PushTypedUserData(l, p, processTypeName)

	procClose(l)

	result := l.ToBool(-2)
	if !result {
		t.Error("close should return true even when already closed")
	}
}

func TestProcessWaitYield(t *testing.T) {
	y := AcquireProcessWaitYield()
	defer ReleaseProcessWaitYield(y)

	if y == nil {
		t.Fatal("expected yield to be created")
		return
	}
	if y.ProcessWaitCmd == nil {
		t.Error("yield should have ProcessWaitCmd")
	}

	if y.String() != "<process_wait_yield>" {
		t.Error("unexpected string representation")
	}
	if y.Type() != lua.LTUserData {
		t.Error("unexpected type")
	}
	if y.CmdID() != execapi.ProcessWait {
		t.Error("unexpected command ID")
	}
}

func TestProcessWaitYieldHandleResult(t *testing.T) {
	tests := []struct {
		data     any
		err      error
		name     string
		exitCode int
		wantErr  bool
	}{
		{
			name:     "success",
			data:     execapi.ProcessWaitResponse{ExitCode: 0, Error: nil},
			err:      nil,
			wantErr:  false,
			exitCode: 0,
		},
		{
			name:    "error from wait",
			data:    nil,
			err:     errors.New("wait failed"),
			wantErr: true,
		},
		{
			name:    "invalid response type",
			data:    "invalid",
			err:     nil,
			wantErr: true,
		},
		{
			name:    "process exit error",
			data:    execapi.ProcessWaitResponse{Error: errors.New("process error")},
			err:     nil,
			wantErr: true,
		},
		{
			name:     "non-zero exit code",
			data:     execapi.ProcessWaitResponse{ExitCode: 1, Error: nil},
			err:      nil,
			wantErr:  false,
			exitCode: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := setupState()
			defer l.Close()

			y := AcquireProcessWaitYield()
			defer ReleaseProcessWaitYield(y)

			result := y.HandleResult(l, tt.data, tt.err)

			if len(result) != 2 {
				t.Fatalf("expected 2 return values, got %d", len(result))
			}

			if tt.wantErr {
				if result[1] == lua.LNil {
					t.Error("expected error, got nil")
				}
			} else {
				if result[1] != lua.LNil {
					t.Errorf("expected no error, got %v", result[1])
				}
				if int(result[0].(lua.LNumber)) != tt.exitCode {
					t.Errorf("expected exit code %d, got %v", tt.exitCode, result[0])
				}
			}
		})
	}
}

func TestProcessWaitYieldToCommand(t *testing.T) {
	y := AcquireProcessWaitYield()
	defer ReleaseProcessWaitYield(y)

	cmd := y.ToCommand()
	if cmd == nil {
		t.Error("ToCommand should return a command")
	}
	if cmd != y.ProcessWaitCmd {
		t.Error("ToCommand should return the ProcessWaitCmd")
	}
}

func TestProcessWaitYieldPool(t *testing.T) {
	// Test that pool acquire/release works without panics
	// Note: sync.Pool doesn't guarantee reuse (GC can clear it)
	y1 := AcquireProcessWaitYield()
	if y1 == nil {
		t.Fatal("acquired yield should not be nil")
	}
	ReleaseProcessWaitYield(y1)

	y2 := AcquireProcessWaitYield()
	if y2 == nil {
		t.Fatal("acquired yield should not be nil")
	}
	ReleaseProcessWaitYield(y2)
}

func TestModuleBuild(t *testing.T) {
	table, yields := Module.Build()

	if table == nil {
		t.Fatal("module table should not be nil")
		return
	}
	if !table.Immutable {
		t.Error("module table should be immutable")
	}
	if len(yields) == 0 {
		t.Error("module should have yield types")
	}

	found := false
	for _, y := range yields {
		if y.CmdID == execapi.ProcessWait {
			found = true
			break
		}
	}
	if !found {
		t.Error("module should register ProcessWait yield type")
	}
}

func TestModuleMetadata(t *testing.T) {
	if Module.Name != "exec" {
		t.Errorf("expected module name 'exec', got %q", Module.Name)
	}
	if Module.Description == "" {
		t.Error("module should have description")
	}
	if len(Module.Class) == 0 {
		t.Error("module should have classes")
	}
}

func TestProcessCloseWithoutForce(t *testing.T) {
	mockHandle := &mockProcess{}
	p := &Process{
		handle: mockHandle,
		closed: false,
	}

	l := setupState()
	defer l.Close()

	value.PushTypedUserData(l, p, processTypeName)

	procClose(l)

	if !p.closed {
		t.Error("process should be closed")
	}
	if mockHandle.signalCalled != 1 {
		t.Errorf("signal should be called once with SIGTERM, got %d calls", mockHandle.signalCalled)
	}
}

func TestProcessCloseWithNilHandle(t *testing.T) {
	p := &Process{
		handle: nil,
		closed: false,
	}

	l := setupState()
	defer l.Close()

	value.PushTypedUserData(l, p, processTypeName)

	procClose(l)

	result := l.ToBool(-2)
	if !result {
		t.Error("close should return true even with nil handle")
	}
}

type mockResource struct {
	released bool
}

func (m *mockResource) ID() registry.ID { return registry.ID{} }
func (m *mockResource) Get() (any, error) {
	return &mockProcessExecutor{}, nil
}
func (m *mockResource) Release() {
	m.released = true
}

type mockProcessExecutor struct {
	newProcessErr error
	lastCommand   string
	lastOptions   execapi.ProcessOptions
	newProcessN   int
}

func (m *mockProcessExecutor) NewProcess(command string, options execapi.ProcessOptions) (execapi.Process, error) {
	m.lastCommand = command
	m.lastOptions = options
	m.newProcessN++
	if m.newProcessErr != nil {
		return nil, m.newProcessErr
	}
	return &mockProcess{}, nil
}

type mockProcess struct {
	startErr      error
	signalErr     error
	writeStdinErr error
	waitErr       error
	signalCalled  int
}

type mockPTYProcess struct{ mockProcess }

func (*mockPTYProcess) Resize(int, int) error { return nil }

func TestTakePTYProcessTransfersExclusiveOwnership(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	handle := &mockPTYProcess{}
	p := NewProcess(context.Background(), handle)
	ud := value.PushTypedUserData(l, p, processTypeName)

	got, err := takePTYProcess(ud)
	if err != nil {
		t.Fatalf("take PTY process: %v", err)
	}
	if got != handle {
		t.Fatal("PTY ownership transferred to wrong handle")
	}
	if _, err := takePTYProcess(ud); !errors.Is(err, errPTYOwnership) {
		t.Fatalf("second transfer error = %v, want %v", err, errPTYOwnership)
	}
}

func TestExecutorExecParsesPTYOptions(t *testing.T) {
	l := setupState()
	defer l.Close()
	ctx := securityapi.SetStrictMode(ctxapi.NewRootContext(), false)
	l.SetContext(ctx)
	factory := &mockProcessExecutor{}

	value.PushTypedUserData(l, NewExecutor(ctx, nil, factory), executorTypeName)
	l.Push(lua.LString("test-command"))
	options := l.NewTable()
	options.RawSetString("work_dir", lua.LString("/tmp"))
	env := l.NewTable()
	env.RawSetString("FOO", lua.LString("bar"))
	options.RawSetString("env", env)
	pty := l.NewTable()
	pty.RawSetString("width", lua.LInteger(100))
	pty.RawSetString("height", lua.LInteger(30))
	pty.RawSetString("term", lua.LString("xterm-256color"))
	options.RawSetString("pty", pty)
	l.Push(options)

	if returns := executorExec(l); returns != 2 {
		t.Fatalf("executorExec returned %d values, want 2", returns)
	}
	if factory.newProcessN != 1 {
		t.Fatalf("NewProcess called %d times, want 1", factory.newProcessN)
	}
	if factory.lastCommand != "test-command" {
		t.Fatalf("command = %q, want test-command", factory.lastCommand)
	}
	if factory.lastOptions.WorkDir != "/tmp" || factory.lastOptions.Env["FOO"] != "bar" {
		t.Fatalf("ordinary options were not preserved: %+v", factory.lastOptions)
	}
	if factory.lastOptions.PTY == nil || *factory.lastOptions.PTY != (execapi.PTYOptions{
		Width: 100, Height: 30, Term: "xterm-256color",
	}) {
		t.Fatalf("PTY options = %+v", factory.lastOptions.PTY)
	}
}

func TestExecutorExecRejectsInvalidPTYOptions(t *testing.T) {
	tests := []struct {
		build func(*lua.LState) lua.LValue
		name  string
	}{
		{name: "non-table", build: func(*lua.LState) lua.LValue { return lua.LString("terminal") }},
		{name: "non-integer width", build: func(l *lua.LState) lua.LValue {
			table := l.NewTable()
			table.RawSetString("width", lua.LString("100"))
			return table
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			l := setupState()
			defer l.Close()
			ctx := securityapi.SetStrictMode(ctxapi.NewRootContext(), false)
			l.SetContext(ctx)
			factory := &mockProcessExecutor{}

			value.PushTypedUserData(l, NewExecutor(ctx, nil, factory), executorTypeName)
			l.Push(lua.LString("test-command"))
			options := l.NewTable()
			options.RawSetString("pty", test.build(l))
			l.Push(options)

			if returns := executorExec(l); returns != 2 {
				t.Fatalf("executorExec returned %d values, want 2", returns)
			}
			if factory.newProcessN != 0 {
				t.Fatalf("NewProcess called %d times for invalid options", factory.newProcessN)
			}
			if l.Get(-2) != lua.LNil || l.Get(-1) == lua.LNil {
				t.Fatalf("result = (%v, %v), want (nil, error)", l.Get(-2), l.Get(-1))
			}
		})
	}
}

func TestExecutorExecRejectsMalformedProcessOptions(t *testing.T) {
	tests := []struct {
		build func(*lua.LState) lua.LValue
		name  string
	}{
		{name: "options are not a table", build: func(*lua.LState) lua.LValue {
			return lua.LString("options")
		}},
		{name: "work directory is not a string", build: func(l *lua.LState) lua.LValue {
			table := l.NewTable()
			table.RawSetString("work_dir", lua.LInteger(42))
			return table
		}},
		{name: "environment is not a table", build: func(l *lua.LState) lua.LValue {
			table := l.NewTable()
			table.RawSetString("env", lua.LString("FOO=bar"))
			return table
		}},
		{name: "environment value is not a string", build: func(l *lua.LState) lua.LValue {
			table := l.NewTable()
			env := l.NewTable()
			env.RawSetString("PORT", lua.LInteger(8080))
			table.RawSetString("env", env)
			return table
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			l := setupState()
			defer l.Close()
			ctx := securityapi.SetStrictMode(ctxapi.NewRootContext(), false)
			l.SetContext(ctx)
			factory := &mockProcessExecutor{}

			value.PushTypedUserData(l, NewExecutor(ctx, nil, factory), executorTypeName)
			l.Push(lua.LString("test-command"))
			l.Push(test.build(l))

			if returns := executorExec(l); returns != 2 {
				t.Fatalf("executorExec returned %d values, want 2", returns)
			}
			if factory.newProcessN != 0 {
				t.Fatalf("NewProcess called %d times for invalid options", factory.newProcessN)
			}
			if l.Get(-2) != lua.LNil || l.Get(-1) == lua.LNil {
				t.Fatalf("result = (%v, %v), want (nil, error)", l.Get(-2), l.Get(-1))
			}
		})
	}
}

func (m *mockProcess) Start() error {
	return m.startErr
}

func (m *mockProcess) Signal(_ int) error {
	m.signalCalled++
	return m.signalErr
}

func (m *mockProcess) WriteStdin(_ []byte) error {
	return m.writeStdinErr
}

func (m *mockProcess) Wait() error {
	return m.waitErr
}

func (m *mockProcess) Stdout() io.ReadCloser {
	return io.NopCloser(strings.NewReader(""))
}

func (m *mockProcess) Stderr() io.ReadCloser {
	return io.NopCloser(strings.NewReader(""))
}
