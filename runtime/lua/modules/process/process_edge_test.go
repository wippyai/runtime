// SPDX-License-Identifier: MPL-2.0

package process

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	hraft "github.com/hashicorp/raft"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	ctxapi "github.com/wippyai/runtime/api/context"
	globalregapi "github.com/wippyai/runtime/api/globalreg"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	runtimeapi "github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/api/security"
	"github.com/wippyai/runtime/api/topology"
	"github.com/wippyai/runtime/system/globalreg"
)

// --- test helpers ---

func newLuaWithPID(t *testing.T) (*lua.LState, pid.PID) {
	t.Helper()
	l := lua.NewState()
	t.Cleanup(l.Close)
	bindProcess(l)

	testPID := pid.PID{Host: "h1", UniqID: "p1"}
	ctx := ctxapi.NewRootContext()
	security.SetStrictMode(ctx, false)
	ctx, fc := ctxapi.OpenFrameContext(ctx)
	t.Cleanup(func() { ctxapi.ReleaseFrameContext(fc) })
	require.NoError(t, runtimeapi.SetFramePID(ctx, testPID))
	l.SetContext(ctx)

	return l, testPID
}

func newLuaWithPIDAndRegistry(t *testing.T, reg topology.PIDRegistry) (*lua.LState, pid.PID) {
	t.Helper()
	l := lua.NewState()
	t.Cleanup(l.Close)
	bindProcess(l)

	testPID := pid.PID{Host: "h1", UniqID: "p1"}
	ctx := ctxapi.NewRootContext()
	security.SetStrictMode(ctx, false)
	topology.WithRegistry(ctx, reg)
	ctx, fc := ctxapi.OpenFrameContext(ctx)
	t.Cleanup(func() { ctxapi.ReleaseFrameContext(fc) })
	require.NoError(t, runtimeapi.SetFramePID(ctx, testPID))
	l.SetContext(ctx)

	return l, testPID
}

type fakePIDRegistry struct {
	entries map[string]pid.PID
}

func (r *fakePIDRegistry) Register(name string, p pid.PID) (pid.PID, error) {
	if r.entries == nil {
		r.entries = make(map[string]pid.PID)
	}
	if existing, ok := r.entries[name]; ok && existing != p {
		return existing, fmt.Errorf("name %q already registered", name)
	}
	r.entries[name] = p
	return p, nil
}

func (r *fakePIDRegistry) Lookup(name string) (pid.PID, bool) {
	if r.entries == nil {
		return pid.PID{}, false
	}
	p, ok := r.entries[name]
	return p, ok
}

func (r *fakePIDRegistry) Unregister(name string) bool {
	if r.entries == nil {
		return false
	}
	_, ok := r.entries[name]
	if ok {
		delete(r.entries, name)
	}
	return ok
}

func (r *fakePIDRegistry) Remove(p pid.PID) {
	if r.entries == nil {
		return
	}
	for name, registered := range r.entries {
		if registered == p {
			delete(r.entries, name)
		}
	}
}

// --- Yield HandleResult edge cases ---

func TestCancelYield_HandleResult_Error(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	yield := AcquireCancelYield()
	defer yield.Release()

	result := yield.HandleResult(l, nil, errors.New("cancel denied"))
	require.Len(t, result, 2)
	assert.Equal(t, lua.LNil, result[0])
	assert.Contains(t, result[1].String(), "cancel")
}

func TestMonitorYield_HandleResult_Error(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	yield := AcquireMonitorYield()
	defer yield.Release()

	result := yield.HandleResult(l, nil, errors.New("target unreachable"))
	require.Len(t, result, 2)
	assert.Equal(t, lua.LNil, result[0])
	assert.Contains(t, result[1].String(), "target unreachable")
}

func TestUnmonitorYield_HandleResult_Error(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	yield := AcquireUnmonitorYield()
	defer yield.Release()

	result := yield.HandleResult(l, nil, errors.New("not monitoring"))
	require.Len(t, result, 2)
	assert.Equal(t, lua.LNil, result[0])
	assert.Contains(t, result[1].String(), "not monitoring")
}

func TestLinkYield_HandleResult_Error(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	yield := AcquireLinkYield()
	defer yield.Release()

	result := yield.HandleResult(l, nil, errors.New("link denied"))
	require.Len(t, result, 2)
	assert.Equal(t, lua.LNil, result[0])
	assert.Contains(t, result[1].String(), "link")
}

func TestUnlinkYield_HandleResult_Error(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	yield := AcquireUnlinkYield()
	defer yield.Release()

	result := yield.HandleResult(l, nil, errors.New("not linked"))
	require.Len(t, result, 2)
	assert.Equal(t, lua.LNil, result[0])
	assert.Contains(t, result[1].String(), "not linked")
}

func TestSpawnYield_HandleResult_NilData(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	yield := AcquireSpawnYield()
	defer yield.Release()

	result := yield.HandleResult(l, nil, nil)
	require.Len(t, result, 2)
	assert.Equal(t, lua.LNil, result[0])
	assert.Contains(t, result[1].String(), "no response")
}

func TestSpawnYield_HandleResult_WrongType(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	yield := AcquireSpawnYield()
	defer yield.Release()

	result := yield.HandleResult(l, "not a SpawnResult", nil)
	require.Len(t, result, 2)
	assert.Equal(t, lua.LNil, result[0])
	assert.Contains(t, result[1].String(), "invalid response type")
}

func TestSendYield_HandleResult_NilData(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	yield := AcquireSendYield()
	defer yield.Release()

	result := yield.HandleResult(l, nil, nil)
	require.Len(t, result, 2)
	assert.Equal(t, lua.LTrue, result[0])
	assert.Equal(t, lua.LNil, result[1])
}

// --- ExecYield HandleResult ---

func TestExecYield_HandleResult_Error(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	yield := AcquireExecYield()
	defer yield.Release()

	result := yield.HandleResult(l, nil, errors.New("exec failed"))
	require.Len(t, result, 2)
	assert.Equal(t, lua.LNil, result[0])
	assert.Contains(t, result[1].String(), "exec")
}

func TestExecYield_HandleResult_NilData(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	yield := AcquireExecYield()
	defer yield.Release()

	result := yield.HandleResult(l, nil, nil)
	require.Len(t, result, 2)
	assert.Equal(t, lua.LNil, result[0])
	assert.Equal(t, lua.LNil, result[1])
}

func TestExecYield_HandleResult_WrongType(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	yield := AcquireExecYield()
	defer yield.Release()

	result := yield.HandleResult(l, "not ExecResult", nil)
	require.Len(t, result, 2)
	assert.Equal(t, lua.LNil, result[0])
	assert.Contains(t, result[1].String(), "invalid exec result type")
}

func TestExecYield_HandleResult_NilResult(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	yield := AcquireExecYield()
	defer yield.Release()

	result := yield.HandleResult(l, process.ExecResult{Result: nil}, nil)
	require.Len(t, result, 2)
	assert.Equal(t, lua.LNil, result[0])
	assert.Equal(t, lua.LNil, result[1])
}

func TestExecYield_HandleResult_ResultWithError(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	yield := AcquireExecYield()
	defer yield.Release()

	data := process.ExecResult{
		Result: &runtimeapi.Result{
			Error: errors.New("process crashed"),
		},
	}
	result := yield.HandleResult(l, data, nil)
	require.Len(t, result, 2)
	assert.Equal(t, lua.LNil, result[0])
	assert.Contains(t, result[1].String(), "crashed")
}

func TestExecYield_HandleResult_NilValue(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	yield := AcquireExecYield()
	defer yield.Release()

	data := process.ExecResult{
		Result: &runtimeapi.Result{Value: nil},
	}
	result := yield.HandleResult(l, data, nil)
	require.Len(t, result, 2)
	assert.Equal(t, lua.LNil, result[0])
	assert.Equal(t, lua.LNil, result[1])
}

func TestExecYield_HandleResult_LuaPayload(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	yield := AcquireExecYield()
	defer yield.Release()

	luaVal := lua.LString("result-value")
	data := process.ExecResult{
		Result: &runtimeapi.Result{
			Value: payload.NewPayload(luaVal, payload.Lua),
		},
	}
	result := yield.HandleResult(l, data, nil)
	require.Len(t, result, 2)
	assert.Equal(t, luaVal, result[0])
	assert.Equal(t, lua.LNil, result[1])
}

func TestExecYield_HandleResult_NonLuaPayload_NoTranscoder(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	yield := AcquireExecYield()
	defer yield.Release()

	data := process.ExecResult{
		Result: &runtimeapi.Result{
			Value: payload.NewPayload([]byte("data"), payload.Bytes),
		},
	}
	result := yield.HandleResult(l, data, nil)
	require.Len(t, result, 2)
	assert.Equal(t, lua.LNil, result[0])
	assert.Contains(t, result[1].String(), "transcoder not found")
}

// --- Yield Type/String ---

func TestYield_TypeAndString(t *testing.T) {
	tests := []struct {
		name     string
		acquire  func() lua.LValue
		expected string
	}{
		{"Send", func() lua.LValue { return AcquireSendYield() }, "<process_send_yield>"},
		{"Spawn", func() lua.LValue { return AcquireSpawnYield() }, "<process_spawn_yield>"},
		{"Terminate", func() lua.LValue { return AcquireTerminateYield() }, "<process_terminate_yield>"},
		{"Cancel", func() lua.LValue { return AcquireCancelYield() }, "<process_cancel_yield>"},
		{"Monitor", func() lua.LValue { return AcquireMonitorYield() }, "<process_monitor_yield>"},
		{"Unmonitor", func() lua.LValue { return AcquireUnmonitorYield() }, "<process_unmonitor_yield>"},
		{"Link", func() lua.LValue { return AcquireLinkYield() }, "<process_link_yield>"},
		{"Unlink", func() lua.LValue { return AcquireUnlinkYield() }, "<process_unlink_yield>"},
		{"Exec", func() lua.LValue { return AcquireExecYield() }, "<process_exec_yield>"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			y := tt.acquire()
			assert.Equal(t, lua.LTUserData, y.Type())
			assert.Equal(t, tt.expected, y.String())
		})
	}
}

// --- Yield pool acquire/release ---

func TestYieldPool_SpawnYield(t *testing.T) {
	y := AcquireSpawnYield()
	assert.NotNil(t, y.SpawnCmd)
	y.Release()
	assert.Nil(t, y.SpawnCmd)
}

func TestYieldPool_TerminateYield(t *testing.T) {
	y := AcquireTerminateYield()
	assert.NotNil(t, y.TerminateCmd)
	y.Release()
	assert.Nil(t, y.TerminateCmd)
}

func TestYieldPool_CancelYield(t *testing.T) {
	y := AcquireCancelYield()
	assert.NotNil(t, y.CancelCmd)
	y.Release()
	assert.Nil(t, y.CancelCmd)
}

func TestYieldPool_MonitorYield(t *testing.T) {
	y := AcquireMonitorYield()
	assert.NotNil(t, y.MonitorCmd)
	y.Release()
	assert.Nil(t, y.MonitorCmd)
}

func TestYieldPool_UnmonitorYield(t *testing.T) {
	y := AcquireUnmonitorYield()
	assert.NotNil(t, y.UnmonitorCmd)
	y.Release()
	assert.Nil(t, y.UnmonitorCmd)
}

func TestYieldPool_LinkYield(t *testing.T) {
	y := AcquireLinkYield()
	assert.NotNil(t, y.LinkCmd)
	y.Release()
	assert.Nil(t, y.LinkCmd)
}

func TestYieldPool_UnlinkYield(t *testing.T) {
	y := AcquireUnlinkYield()
	assert.NotNil(t, y.UnlinkCmd)
	y.Release()
	assert.Nil(t, y.UnlinkCmd)
}

func TestYieldPool_ExecYield(t *testing.T) {
	y := AcquireExecYield()
	assert.NotNil(t, y.ExecCmd)
	y.Release()
	assert.Nil(t, y.ExecCmd)
}

// --- ExecYield CmdID ---

func TestExecYield_CmdID(t *testing.T) {
	y := AcquireExecYield()
	defer y.Release()
	assert.Equal(t, process.Exec, y.CmdID())
}

// --- buildSpawnerContext ---

func TestBuildSpawnerContext_NilSpawner(t *testing.T) {
	pairs := buildSpawnerContext(nil)
	assert.Nil(t, pairs)
}

func TestBuildSpawnerContext_EmptySpawner(t *testing.T) {
	s := &Spawner{}
	pairs := buildSpawnerContext(s)
	assert.Nil(t, pairs)
}

func TestBuildSpawnerContext_WithValues(t *testing.T) {
	values := ctxapi.NewValues()
	values.Set("key", "value")

	s := &Spawner{values: values}
	pairs := buildSpawnerContext(s)
	require.Len(t, pairs, 1)
}

func TestBuildSpawnerContext_EmptyValues(t *testing.T) {
	values := ctxapi.NewValues()
	s := &Spawner{values: values}
	pairs := buildSpawnerContext(s)
	assert.Nil(t, pairs)
}

// --- Lua listen validation ---

func TestListen_EmptyTopic(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindProcess(l)

	err := l.DoString(`
		local ch, err = process.listen("")
		if ch ~= nil then error("expected nil channel") end
		if err:kind() ~= "Invalid" then
			error("expected Invalid kind, got: " .. tostring(err:kind()))
		end
		if tostring(err) ~= "topic cannot be empty" then
			error("expected 'topic cannot be empty', got: " .. tostring(err))
		end
	`)
	assert.NoError(t, err)
}

func TestListen_AtTopicRejected(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindProcess(l)

	err := l.DoString(`
		local ch, err = process.listen("@events")
		if ch ~= nil then error("expected nil channel") end
		if err:kind() ~= "Invalid" then
			error("expected Invalid kind, got: " .. tostring(err:kind()))
		end
		if tostring(err) ~= "cannot listen to @ topics" then
			error("expected '@ topics' error, got: " .. tostring(err))
		end
	`)
	assert.NoError(t, err)
}

// --- Lua exec validation ---

func TestExec_NoArgs(t *testing.T) {
	l, _ := newLuaWithPID(t)

	err := l.DoString(`
		local val, err = process.exec()
		if val ~= nil then error("expected nil") end
		if err == nil then error("expected error") end
	`)
	assert.NoError(t, err)
}

func TestExec_EmptyID(t *testing.T) {
	l, _ := newLuaWithPID(t)

	err := l.DoString(`
		local val, err = process.exec("", "host")
		if val ~= nil then error("expected nil") end
		if err:kind() ~= "Invalid" then
			error("expected Invalid kind, got: " .. tostring(err:kind()))
		end
		if tostring(err) ~= "process ID required" then
			error("expected 'process ID required', got: " .. tostring(err))
		end
	`)
	assert.NoError(t, err)
}

func TestExec_InvalidFormat(t *testing.T) {
	l, _ := newLuaWithPID(t)

	err := l.DoString(`
		local val, err = process.exec("nonamespace", "host")
		if val ~= nil then error("expected nil") end
		if err:kind() ~= "Invalid" then
			error("expected Invalid kind, got: " .. tostring(err:kind()))
		end
		if tostring(err) ~= "invalid process ID format (namespace:name required)" then
			error("expected format error, got: " .. tostring(err))
		end
	`)
	assert.NoError(t, err)
}

func TestExec_EmptyHost(t *testing.T) {
	l, _ := newLuaWithPID(t)

	err := l.DoString(`
		local val, err = process.exec("ns:name", "")
		if val ~= nil then error("expected nil") end
		if err:kind() ~= "Invalid" then
			error("expected Invalid kind, got: " .. tostring(err:kind()))
		end
		if tostring(err) ~= "host ID required" then
			error("expected 'host ID required', got: " .. tostring(err))
		end
	`)
	assert.NoError(t, err)
}

// --- Lua send validation ---

func TestSend_AtTopicRejected(t *testing.T) {
	l, selfPID := newLuaWithPID(t)
	pidStr := selfPID.String()

	err := l.DoString(fmt.Sprintf(`
		local ok, err = process.send("%s", "@system")
		if ok ~= nil then error("expected nil") end
		if err:kind() ~= "Invalid" then
			error("expected Invalid kind, got: " .. tostring(err:kind()))
		end
		if tostring(err) ~= "cannot send to @ topics" then
			error("expected '@ topics' error, got: " .. tostring(err))
		end
	`, pidStr))
	assert.NoError(t, err)
}

func TestSend_TooFewArgs(t *testing.T) {
	l, _ := newLuaWithPID(t)

	err := l.DoString(`
		local ok, err = process.send()
		if ok ~= nil then error("expected nil") end
		if err == nil then error("expected error") end
	`)
	assert.NoError(t, err)
}

// --- Lua cancel validation ---

func TestCancel_InvalidDurationString(t *testing.T) {
	l, selfPID := newLuaWithPID(t)
	pidStr := selfPID.String()

	err := l.DoString(fmt.Sprintf(`
		local ok, err = process.cancel("%s", "not-a-duration")
		if ok ~= nil then error("expected nil") end
		if err:kind() ~= "Invalid" then
			error("expected Invalid kind, got: " .. tostring(err:kind()))
		end
		if not string.find(tostring(err), "invalid duration format") then
			error("expected duration format error, got: " .. tostring(err))
		end
	`, pidStr))
	assert.NoError(t, err)
}

func TestCancel_InvalidDeadlineType(t *testing.T) {
	l, selfPID := newLuaWithPID(t)
	pidStr := selfPID.String()

	err := l.DoString(fmt.Sprintf(`
		local ok, err = process.cancel("%s", true)
		if ok ~= nil then error("expected nil") end
		if err:kind() ~= "Invalid" then
			error("expected Invalid kind, got: " .. tostring(err:kind()))
		end
		if tostring(err) ~= "deadline must be either a duration string or milliseconds number" then
			error("expected deadline type error, got: " .. tostring(err))
		end
	`, pidStr))
	assert.NoError(t, err)
}

func TestCancel_TableDeadlineType(t *testing.T) {
	l, selfPID := newLuaWithPID(t)
	pidStr := selfPID.String()

	err := l.DoString(fmt.Sprintf(`
		local ok, err = process.cancel("%s", {})
		if ok ~= nil then error("expected nil") end
		if err:kind() ~= "Invalid" then
			error("expected Invalid kind, got: " .. tostring(err:kind()))
		end
		if tostring(err) ~= "deadline must be either a duration string or milliseconds number" then
			error("expected deadline type error, got: " .. tostring(err))
		end
	`, pidStr))
	assert.NoError(t, err)
}

// --- Lua registry operations ---

func TestRegistryLookup_NoRegistry(t *testing.T) {
	l, _ := newLuaWithPID(t)

	err := l.DoString(`
		local pid, err = process.registry.lookup("service")
		if pid ~= nil then error("expected nil") end
		if err == nil then error("expected error") end
	`)
	assert.NoError(t, err)
}

func TestRegistryRegister_NoRegistry(t *testing.T) {
	l, _ := newLuaWithPID(t)

	err := l.DoString(`
		local ok, err = process.registry.register("service")
		if ok ~= nil then error("expected nil") end
		if err == nil then error("expected error") end
	`)
	assert.NoError(t, err)
}

func TestRegistryLookup_NotFound(t *testing.T) {
	reg := &fakePIDRegistry{}
	l, _ := newLuaWithPIDAndRegistry(t, reg)

	err := l.DoString(`
		local pid, err = process.registry.lookup("nonexistent")
		if pid ~= nil then error("expected nil, got: " .. tostring(pid)) end
		if err:kind() ~= "NotFound" then
			error("expected NotFound kind, got: " .. tostring(err:kind()))
		end
		if tostring(err) ~= "name not registered" then
			error("expected 'name not registered', got: " .. tostring(err))
		end
	`)
	assert.NoError(t, err)
}

func TestRegistryRegister_AndLookup(t *testing.T) {
	reg := &fakePIDRegistry{}
	l, _ := newLuaWithPIDAndRegistry(t, reg)

	err := l.DoString(`
		local ok, err = process.registry.register("my-service")
		if not ok then error("register failed: " .. tostring(err)) end

		local pid, err = process.registry.lookup("my-service")
		if pid == nil then error("lookup failed: " .. tostring(err)) end
		if type(pid) ~= "string" then error("expected string PID") end
	`)
	assert.NoError(t, err)
}

func TestRegistryUnregister_Success(t *testing.T) {
	reg := &fakePIDRegistry{
		entries: map[string]pid.PID{
			"to-remove": {Host: "h1", UniqID: "p1"},
		},
	}
	l, _ := newLuaWithPIDAndRegistry(t, reg)

	err := l.DoString(`
		local ok = process.registry.unregister("to-remove")
		if not ok then error("unregister should return true") end

		local pid, err = process.registry.lookup("to-remove")
		if pid ~= nil then error("name should be gone after unregister") end
	`)
	assert.NoError(t, err)
}

func TestRegistryUnregister_NotFound(t *testing.T) {
	reg := &fakePIDRegistry{}
	l, _ := newLuaWithPIDAndRegistry(t, reg)

	err := l.DoString(`
		local ok = process.registry.unregister("nonexistent")
		if ok then error("unregister nonexistent should return false") end
	`)
	assert.NoError(t, err)
}

// --- Scoped registry unregister owner-check ---

// fakeScopedRegistry drives a real globalreg.FSM through the public
// globalreg.Registry interface so we can exercise the STRONG/CONSISTENT
// unregister authority check without standing up a real Raft cluster.
type fakeScopedRegistry struct {
	fsm      *globalreg.FSM
	mu       sync.Mutex
	logIndex uint64
}

func newFakeScopedRegistry() *fakeScopedRegistry {
	return &fakeScopedRegistry{fsm: globalreg.NewFSM()}
}

func (f *fakeScopedRegistry) apply(cmd *globalreg.Command) any {
	data, err := globalreg.EncodeCommand(cmd)
	if err != nil {
		return err
	}
	f.logIndex++
	return f.fsm.Apply(&hraft.Log{Data: data, Index: f.logIndex})
}

func (f *fakeScopedRegistry) Register(ctx context.Context, name string, p pid.PID) (pid.PID, error) {
	out, err := f.RegisterScope(ctx, name, p, globalregapi.Consistent)
	if err != nil {
		return out.ExistingPID, err
	}
	return out.PID, nil
}

func (f *fakeScopedRegistry) RegisterScope(_ context.Context, name string, p pid.PID, _ globalregapi.RegistrationMode) (globalregapi.RegisterOutcome, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cmd := &globalreg.Command{Type: globalreg.CmdRegister, Name: name, PID: p, NodeID: p.Node}
	resp := f.apply(cmd)
	r, ok := resp.(*globalreg.RegisterResult)
	if !ok {
		return globalregapi.RegisterOutcome{}, resp.(error)
	}
	if r.Err != nil {
		return globalregapi.RegisterOutcome{ExistingPID: r.ExistingPID}, globalregapi.ErrNameAlreadyRegistered
	}
	return globalregapi.RegisterOutcome{PID: r.PID, Epoch: r.FenceToken, State: globalregapi.RegisterStateActive}, nil
}

func (f *fakeScopedRegistry) Unregister(ctx context.Context, name string) (bool, error) {
	return f.UnregisterScope(ctx, name, globalregapi.Consistent)
}

func (f *fakeScopedRegistry) UnregisterScope(_ context.Context, name string, _ globalregapi.RegistrationMode) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cmd := &globalreg.Command{Type: globalreg.CmdUnregister, Name: name}
	resp := f.apply(cmd)
	r, ok := resp.(*globalreg.UnregisterResult)
	if !ok {
		return false, resp.(error)
	}
	return r.Removed, nil
}

func (f *fakeScopedRegistry) Lookup(_ context.Context, name string, opts ...globalregapi.LookupOption) (globalregapi.LookupResult, error) {
	var o globalregapi.LookupOptions
	for _, opt := range opts {
		opt(&o)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	state := f.fsm.State()
	if o.ByPID != nil {
		names := state.LookupByPID(*o.ByPID)
		return globalregapi.LookupResult{PID: *o.ByPID, NamesForPID: names, Found: len(names) > 0}, nil
	}
	p, found := state.Lookup(name)
	return globalregapi.LookupResult{PID: p, Found: found}, nil
}

func (f *fakeScopedRegistry) LookupByPID(p pid.PID) []string {
	r, _ := f.Lookup(context.Background(), "", globalregapi.ByPID(p))
	return r.NamesForPID
}

func (f *fakeScopedRegistry) Remove(_ context.Context, p pid.PID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cmd := &globalreg.Command{Type: globalreg.CmdRemovePID, PID: p}
	f.apply(cmd)
	return nil
}

func (f *fakeScopedRegistry) RemoveNode(_ context.Context, n pid.NodeID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cmd := &globalreg.Command{Type: globalreg.CmdRemoveNode, NodeID: n}
	f.apply(cmd)
	return nil
}

var _ globalregapi.Registry = (*fakeScopedRegistry)(nil)

// newLuaWithScopedRegistry binds the process module on a Lua state wired
// with a runtime PID, a local PIDRegistry, and a global registry backing
// STRONG/CONSISTENT.
func newLuaWithScopedRegistry(t *testing.T, p pid.PID, scoped globalregapi.Registry, strict bool) *lua.LState {
	t.Helper()
	l := lua.NewState()
	t.Cleanup(l.Close)
	bindProcess(l)

	ctx := ctxapi.NewRootContext()
	security.SetStrictMode(ctx, strict)
	topology.WithRegistry(ctx, &fakePIDRegistry{})
	ctx = globalregapi.WithRegistry(ctx, scoped)
	ctx, fc := ctxapi.OpenFrameContext(ctx)
	t.Cleanup(func() { ctxapi.ReleaseFrameContext(fc) })
	require.NoError(t, runtimeapi.SetFramePID(ctx, p))
	l.SetContext(ctx)
	return l
}

// TestRegistryUnregister_Strong_ByHolder verifies the holder can drop its
// own STRONG name and the entry is removed from the registry.
func TestRegistryUnregister_Strong_ByHolder(t *testing.T) {
	reg := newFakeScopedRegistry()
	holder := pid.PID{Host: "h1", UniqID: "holder", Node: "node-1"}

	_, err := reg.RegisterScope(context.Background(), "mine-strong", holder, globalregapi.Strong)
	require.NoError(t, err)

	l := newLuaWithScopedRegistry(t, holder, reg, false)

	err = l.DoString(`
		local ok, err = process.registry.unregister("mine-strong", process.registry.STRONG)
		if not ok then error("expected true, got " .. tostring(ok) .. " err=" .. tostring(err)) end
	`)
	require.NoError(t, err)

	res, _ := reg.Lookup(context.Background(), "mine-strong")
	require.False(t, res.Found, "name must be gone after holder unregisters")
}

// TestRegistryUnregister_Strong_NotHolder verifies a non-holder with the
// permission cannot drop another PID's STRONG name; the registry entry
// stays held by the original owner.
func TestRegistryUnregister_Strong_NotHolder(t *testing.T) {
	reg := newFakeScopedRegistry()
	holder := pid.PID{Host: "h1", UniqID: "holder", Node: "node-1"}
	other := pid.PID{Host: "h1", UniqID: "other", Node: "node-1"}

	_, err := reg.RegisterScope(context.Background(), "owned", holder, globalregapi.Strong)
	require.NoError(t, err)

	l := newLuaWithScopedRegistry(t, other, reg, false)

	err = l.DoString(`
		local ok = process.registry.unregister("owned", process.registry.STRONG)
		if ok then error("non-holder must not be able to drop another PID's STRONG name") end
	`)
	require.NoError(t, err)

	res, _ := reg.Lookup(context.Background(), "owned")
	require.True(t, res.Found, "name must remain held by original holder")
	require.Equal(t, holder, res.PID)
}

// TestRegistryUnregister_Consistent_ByHolder verifies the holder can drop
// its own CONSISTENT name.
func TestRegistryUnregister_Consistent_ByHolder(t *testing.T) {
	reg := newFakeScopedRegistry()
	holder := pid.PID{Host: "h1", UniqID: "holder", Node: "node-1"}

	_, err := reg.RegisterScope(context.Background(), "mine-cons", holder, globalregapi.Consistent)
	require.NoError(t, err)

	l := newLuaWithScopedRegistry(t, holder, reg, false)

	err = l.DoString(`
		local ok, err = process.registry.unregister("mine-cons", process.registry.CONSISTENT)
		if not ok then error("expected true, got " .. tostring(ok) .. " err=" .. tostring(err)) end
	`)
	require.NoError(t, err)

	res, _ := reg.Lookup(context.Background(), "mine-cons")
	require.False(t, res.Found, "name must be gone after holder unregisters")
}

// TestRegistryUnregister_Consistent_NotHolder verifies the same authority
// check fires for CONSISTENT scope.
func TestRegistryUnregister_Consistent_NotHolder(t *testing.T) {
	reg := newFakeScopedRegistry()
	holder := pid.PID{Host: "h1", UniqID: "holder", Node: "node-1"}
	other := pid.PID{Host: "h1", UniqID: "other", Node: "node-1"}

	_, err := reg.RegisterScope(context.Background(), "owned-cons", holder, globalregapi.Consistent)
	require.NoError(t, err)

	l := newLuaWithScopedRegistry(t, other, reg, false)

	err = l.DoString(`
		local ok = process.registry.unregister("owned-cons", process.registry.CONSISTENT)
		if ok then error("non-holder must not be able to drop another PID's CONSISTENT name") end
	`)
	require.NoError(t, err)

	res, _ := reg.Lookup(context.Background(), "owned-cons")
	require.True(t, res.Found, "name must remain held by original holder")
	require.Equal(t, holder, res.PID)
}

// TestRegistryUnregister_Strong_PermissionDenied verifies the capability
// gate still fires before the owner check: strict security rejects with
// a permission-denied error even for the actual holder.
func TestRegistryUnregister_Strong_PermissionDenied(t *testing.T) {
	reg := newFakeScopedRegistry()
	holder := pid.PID{Host: "h1", UniqID: "holder", Node: "node-1"}

	_, err := reg.RegisterScope(context.Background(), "gated", holder, globalregapi.Strong)
	require.NoError(t, err)

	l := newLuaWithScopedRegistry(t, holder, reg, true)

	err = l.DoString(`
		local ok, err = process.registry.unregister("gated", process.registry.STRONG)
		if ok ~= nil then error("expected nil under strict security, got " .. tostring(ok)) end
		if err == nil then error("expected permission-denied error") end
	`)
	require.NoError(t, err)

	res, _ := reg.Lookup(context.Background(), "gated")
	require.True(t, res.Found, "name must remain on permission denial")
}

// --- Spawner fields ---

func TestSpawner_DefaultValues(t *testing.T) {
	s := &Spawner{}
	assert.Nil(t, s.values)
	assert.Nil(t, s.scope)
	assert.False(t, s.hasActor)
	assert.False(t, s.hasScope)
	assert.Empty(t, s.name)
	assert.Nil(t, s.messages)
}

// --- Message edge cases ---

func TestMessagePayload_NoPayloads(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindProcess(l)

	msg := NewMessage(pid.PID{}, "topic", nil)
	wrapped := WrapMessage(l, msg)
	l.SetGlobal("msg", wrapped)

	err := l.DoString(`
		local p = msg:payload()
		if p ~= nil then error("expected nil for empty payloads") end
	`)
	assert.NoError(t, err)
}

func TestMessagePayload_MultiplePayloads(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindProcess(l)

	payloads := payload.Payloads{
		payload.NewPayload(lua.LString("a"), payload.Lua),
		payload.NewPayload(lua.LString("b"), payload.Lua),
	}
	msg := NewMessage(pid.PID{}, "topic", payloads)
	wrapped := WrapMessage(l, msg)
	l.SetGlobal("msg", wrapped)

	err := l.DoString(`
		local p = msg:payload()
		if type(p) ~= "table" then
			error("expected table for multiple payloads, got: " .. type(p))
		end
	`)
	assert.NoError(t, err)
}

// --- processID validation ---

func TestProcessID_NoFrameContext(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindProcess(l)
	l.SetContext(ctxapi.NewRootContext())

	err := l.DoString(`
		local id, err = process.id()
		if id ~= nil then error("expected nil") end
		if err == nil then error("expected error") end
	`)
	assert.NoError(t, err)
}

// --- registry.register: new (name, scope?, pid?) signature ---

// TestRegistryRegister_Strong_Self exercises the clean signature: name + scope.
func TestRegistryRegister_Strong_Self(t *testing.T) {
	reg := newFakeScopedRegistry()
	self := pid.PID{Host: "h1", UniqID: "self", Node: "node-1"}
	l := newLuaWithScopedRegistry(t, self, reg, false)

	err := l.DoString(`
		local ok, err = process.registry.register("svc-strong", nil, process.registry.STRONG)
		if not ok then error("expected true, got " .. tostring(ok) .. " err=" .. tostring(err)) end
	`)
	require.NoError(t, err)

	res, _ := reg.Lookup(context.Background(), "svc-strong")
	require.True(t, res.Found)
	require.Equal(t, self, res.PID)
}

// TestRegistryRegister_ForeignPID_Permitted exercises (name, pid, scope)
// when the caller has the process.registry.foreign capability (lax security).
func TestRegistryRegister_ForeignPID_Permitted(t *testing.T) {
	reg := newFakeScopedRegistry()
	self := pid.PID{Host: "h1", UniqID: "owner", Node: "node-1"}
	other := pid.PID{Host: "h1", UniqID: "other", Node: "node-1"}
	l := newLuaWithScopedRegistry(t, self, reg, false)

	err := l.DoString(fmt.Sprintf(`
		local ok, err = process.registry.register("svc-foreign", %q, process.registry.STRONG)
		if not ok then error("expected true, got " .. tostring(ok) .. " err=" .. tostring(err)) end
	`, other.String()))
	require.NoError(t, err)

	res, _ := reg.Lookup(context.Background(), "svc-foreign")
	require.True(t, res.Found)
	require.Equal(t, other, res.PID, "foreign PID should be the registered binding")
}

// TestRegistryRegister_ForeignPID_Denied verifies the foreign-PID capability
// is its own axis: strict security denies it even when the per-scope register
// capability would have allowed registering self.
func TestRegistryRegister_ForeignPID_Denied(t *testing.T) {
	reg := newFakeScopedRegistry()
	self := pid.PID{Host: "h1", UniqID: "owner", Node: "node-1"}
	other := pid.PID{Host: "h1", UniqID: "other", Node: "node-1"}
	l := newLuaWithScopedRegistry(t, self, reg, true)

	err := l.DoString(fmt.Sprintf(`
		local ok, err = process.registry.register("svc-foreign", %q, process.registry.STRONG)
		if ok then error("expected false under strict security, got true") end
		if err == nil then error("expected permission error") end
		if err:kind() ~= "PermissionDenied" then
			error("expected PermissionDenied, got " .. tostring(err:kind()))
		end
	`, other.String()))
	require.NoError(t, err)

	res, _ := reg.Lookup(context.Background(), "svc-foreign")
	require.False(t, res.Found, "no binding should be installed when foreign-PID denied")
}

// TestRegistryRegister_InvalidScopeType verifies that passing a string for the
// scope argument is a type error, not a covert "parse as PID" path.
func TestRegistryRegister_InvalidScopeType(t *testing.T) {
	reg := newFakeScopedRegistry()
	self := pid.PID{Host: "h1", UniqID: "self", Node: "node-1"}
	l := newLuaWithScopedRegistry(t, self, reg, false)

	err := l.DoString(`
		local ok, err = process.registry.register("svc", nil, "not-a-scope")
		if ok then error("expected false, got true") end
		if err == nil then error("expected error") end
		if err:kind() ~= "Invalid" then
			error("expected Invalid, got " .. tostring(err:kind()))
		end
	`)
	require.NoError(t, err)
}

// TestRegistryRegister_InvalidPIDType — pid arg must be a string.
func TestRegistryRegister_InvalidPIDType(t *testing.T) {
	reg := newFakeScopedRegistry()
	self := pid.PID{Host: "h1", UniqID: "self", Node: "node-1"}
	l := newLuaWithScopedRegistry(t, self, reg, false)

	err := l.DoString(`
		local ok, err = process.registry.register("svc", 12345)
		if ok then error("expected false, got true") end
		if err == nil then error("expected error") end
		if err:kind() ~= "Invalid" then
			error("expected Invalid, got " .. tostring(err:kind()))
		end
	`)
	require.NoError(t, err)
}

// --- setOptions edge cases ---

func TestSetOptions_UnsupportedKey(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindProcess(l)

	// setOptions with table containing unsupported key (no process context)
	err := l.DoString(`
		local ok, err = process.set_options({unknown_key = true})
		if ok then error("should fail without process context") end
	`)
	assert.NoError(t, err)
}
