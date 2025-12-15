package exec

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/wippyai/runtime/api/registry"
	execapi "github.com/wippyai/runtime/api/service/exec"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
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

func TestNewExecutor(t *testing.T) {
	ctx := context.Background()
	mockRes := &mockResource{}
	mockFactory := &mockProcessExecutor{}

	e := NewExecutor(ctx, mockRes, mockFactory)

	if e == nil {
		t.Fatal("expected executor to be created")
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
		name     string
		data     any
		err      error
		wantErr  bool
		exitCode int
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
	y1 := AcquireProcessWaitYield()
	ReleaseProcessWaitYield(y1)

	y2 := AcquireProcessWaitYield()
	defer ReleaseProcessWaitYield(y2)

	if y1 != y2 {
		t.Error("pool should reuse yield objects")
	}
}

func TestModuleBuild(t *testing.T) {
	table, yields := Module.Build()

	if table == nil {
		t.Fatal("module table should not be nil")
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
}

func (m *mockProcessExecutor) NewProcess(_ string, _ execapi.ProcessOptions) (execapi.Process, error) {
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
