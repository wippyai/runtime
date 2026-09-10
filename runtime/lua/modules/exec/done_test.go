// SPDX-License-Identifier: MPL-2.0

package exec

import (
	"context"
	osexec "os/exec"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	relayapi "github.com/wippyai/runtime/api/relay"
	runtimeapi "github.com/wippyai/runtime/api/runtime"
	securityapi "github.com/wippyai/runtime/api/security"
	execapi "github.com/wippyai/runtime/api/service/exec"
	"github.com/wippyai/runtime/runtime/lua/engine"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	svcexec "github.com/wippyai/runtime/service/exec"
	"github.com/wippyai/runtime/service/exec/native"
	sysrelay "github.com/wippyai/runtime/system/relay"
	"github.com/wippyai/runtime/system/scheduler"
	"github.com/wippyai/runtime/system/scheduler/pool/inline"
	sysstream "github.com/wippyai/runtime/system/stream"
	"go.uber.org/zap"
)

const doneTestHost = "exec.done:test"

// runExecScript runs a Lua script against a real native executor with the
// production wiring done() depends on: an exec dispatcher for the wait yield, a
// stream dispatcher for stdout reads, and a relay node that routes the exit
// package back into the running process.
func runExecScript(t *testing.T, script string) *lua.LTable {
	t.Helper()
	if _, err := osexec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}

	rootCtx := securityapi.SetStrictMode(ctxapi.NewRootContext(), false)
	ctx, cancel := context.WithTimeout(rootCtx, 30*time.Second)
	defer cancel()

	reg := scheduler.NewRegistry()
	register := func(id dispatcher.CommandID, h dispatcher.Handler) { reg.Register(id, h) }
	svcexec.NewDispatcher().RegisterAll(register)
	streams := sysstream.NewDispatcher()
	require.NoError(t, streams.Start(ctx))
	defer func() { _ = streams.Stop(ctx) }()
	streams.RegisterAll(register)

	factory := native.NewNativeExecutor(zap.NewNop(), &execapi.NativeExecutorConfig{})
	procFactory := func() (process.Process, error) {
		return engine.NewFactory(engine.FactoryConfig{
			Script:     script,
			ScriptName: "exec_done_test",
			ModuleBinders: append(engine.CoreBinders(), func(l *lua.LState) error {
				mod, _ := Module.Build()
				l.SetGlobal(Module.Name, mod)
				executor := value.PushTypedUserData(l, NewExecutor(ctx, nil, factory), executorTypeName)
				l.Pop(1)
				l.SetGlobal("executor", executor)
				return nil
			}),
		})()
	}

	pool, err := inline.New(procFactory, reg)
	require.NoError(t, err)
	defer pool.Stop()

	node := sysrelay.NewNode("exec-done-test-node")
	require.NoError(t, node.RegisterHost(doneTestHost, pool))

	frameCtx, frame := ctxapi.OpenFrameContext(ctx)
	defer ctxapi.ReleaseFrameContext(frame)
	rawPID := pid.PID{Host: doneTestHost, UniqID: "exec-done"}
	target := rawPID.Precomputed()
	require.NoError(t, runtimeapi.SetFramePID(frameCtx, target))
	frameCtx = relayapi.WithNode(frameCtx, node)

	result, err := pool.Call(frameCtx, "main", nil)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NoError(t, result.Error)
	require.NotNil(t, result.Value)

	out, ok := result.Value.Data().(*lua.LTable)
	require.True(t, ok, "script must return a table, got %T", result.Value.Data())
	return out
}

// luaInt reads a number the script returned under key.
func luaInt(t *testing.T, table *lua.LTable, key string) int {
	t.Helper()
	field := table.RawGetString(key)
	require.NotEqual(t, lua.LTNil, field.Type(), "script did not return %q", key)
	return int(lua.LVAsNumber(field))
}

// A supervisor needs the exit and the handle at the same time: it keeps feeding
// the child and steering it with signals while another coroutine waits for it to
// finish. wait() cannot serve that, because it consumes the handle.
func TestProcessDoneDeliversExitWhileHandleStaysUsable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("native signals are not supported on Windows")
	}
	out := runExecScript(t, `
local function main()
    local child, err = executor:exec("cat")
    if err then return nil, "exec: " .. tostring(err) end

    local stdout, serr = child:stdout_stream()
    if serr then return nil, "stdout_stream: " .. tostring(serr) end

    local started, terr = child:start()
    if not started then return nil, "start: " .. tostring(terr) end

    local exit, derr = child:done()
    if derr then return nil, "done: " .. tostring(derr) end

    local observed = channel.new(1)
    coroutine.spawn(function()
        local selected = channel.select{ exit:case_receive() }
        observed:send(selected.value)
    end)

    local wrote, werr = child:write_stdin("ping\n")
    if not wrote then return nil, "write_stdin after done: " .. tostring(werr) end

    local echoed, rerr = stdout:read()
    if rerr then return nil, "read: " .. tostring(rerr) end

    local signaled, sigerr = child:signal(15)
    if not signaled then return nil, "signal after done: " .. tostring(sigerr) end

    local status = observed:receive()
    if status == nil then return nil, "exit channel delivered no value" end

    return { code = status.code, signal = status.signal, echoed = echoed }
end

return { main = main }
`)

	require.Equal(t, lua.LString("ping\n"), out.RawGetString("echoed"))
	require.Equal(t, 143, luaInt(t, out, "code"))
	require.Equal(t, 15, luaInt(t, out, "signal"))
}

// The exit is recorded once and reported to whoever asks for it, so a
// supervisor that watched the child on done() can still read its code through
// wait() afterwards.
func TestProcessWaitAfterDoneReturnsExitCode(t *testing.T) {
	out := runExecScript(t, `
local function main()
    local child, err = executor:exec("sh -c \"exit 7\"")
    if err then return nil, "exec: " .. tostring(err) end

    local started, terr = child:start()
    if not started then return nil, "start: " .. tostring(terr) end

    local exit, derr = child:done()
    if derr then return nil, "done: " .. tostring(derr) end

    local status = exit:receive()
    if status == nil then return nil, "exit channel delivered no value" end

    local code, werr = child:wait()
    if werr then return nil, "wait after done: " .. tostring(werr) end

    return { done_code = status.code, wait_code = code }
end

return { main = main }
`)

	require.Equal(t, 7, luaInt(t, out, "done_code"))
	require.Equal(t, 7, luaInt(t, out, "wait_code"))
}

// Reading stdout to EOF is not an exit signal: a grandchild inherits the pipe
// and holds it open long after the child itself is gone. done() reports the
// child's own exit, so it arrives while the pipe is still open.
func TestProcessDoneFiresWhileGrandchildHoldsStdout(t *testing.T) {
	const grandchildLifetime = 5 * time.Second

	start := time.Now()
	out := runExecScript(t, `
local function main()
    local child, err = executor:exec("sh -c \"sleep 5 & exit 3\"")
    if err then return nil, "exec: " .. tostring(err) end

    local stdout, serr = child:stdout_stream()
    if serr then return nil, "stdout_stream: " .. tostring(serr) end
    if stdout == nil then return nil, "no stdout stream" end

    local started, terr = child:start()
    if not started then return nil, "start: " .. tostring(terr) end

    local exit, derr = child:done()
    if derr then return nil, "done: " .. tostring(derr) end

    local status = exit:receive()
    if status == nil then return nil, "exit channel delivered no value" end

    return { code = status.code }
end

return { main = main }
`)
	elapsed := time.Since(start)

	require.Equal(t, 3, luaInt(t, out, "code"))
	require.Less(t, elapsed, grandchildLifetime/2,
		"the exit was reported only once the inherited stdout pipe closed")
}

// The scenario above only means anything if the grandchild really keeps stdout
// open past the child's own exit, so that draining the pipe would report
// nothing for as long as the grandchild lives.
func TestGrandchildKeepsStdoutOpenAfterChildExits(t *testing.T) {
	if _, err := osexec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}
	cmd := osexec.CommandContext(t.Context(), "sh", "-c", "sleep 5 & exit 3")
	stdout, err := cmd.StdoutPipe()
	require.NoError(t, err)
	require.NoError(t, cmd.Start())

	// Process.Wait reaps the child without closing the pipes Cmd created, so
	// the pipe can still be observed after the child is gone.
	state, err := cmd.Process.Wait()
	require.NoError(t, err)
	require.Equal(t, 3, state.ExitCode())

	read := make(chan error, 1)
	go func() {
		_, err := stdout.Read(make([]byte, 1))
		read <- err
	}()
	select {
	case err := <-read:
		t.Fatalf("stdout ended with the child instead of with the grandchild: %v", err)
	case <-time.After(500 * time.Millisecond):
	}
	require.NoError(t, stdout.Close())
}

// Whichever path observes the exit first owns the reap: the child is waited on
// once, and close() still releases the handle afterwards.
func TestProcessReapsChildOnceAcrossPaths(t *testing.T) {
	probe := newReapProbe()
	p := &Process{handle: probe, started: true}

	require.Equal(t, execapi.ExitStatus{}, p.reap(probe))

	l := setupState()
	defer l.Close()
	value.PushTypedUserData(l, p, processTypeName)
	procClose(l)

	require.True(t, waitFor(t, time.Second, func() bool {
		return len(probe.sent()) == 1
	}), "close must still signal the child")
	require.Equal(t, 1, probe.waits(), "the child must be waited on exactly once")
}
