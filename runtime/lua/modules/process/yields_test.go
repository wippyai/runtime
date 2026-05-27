// SPDX-License-Identifier: MPL-2.0

package process

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	runtimeapi "github.com/wippyai/runtime/api/runtime"
)

func TestSendYield_HandleResult_Success(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	yield := AcquireSendYield()
	defer yield.Release()

	result := yield.HandleResult(l, process.SendResult{Error: nil}, nil)
	assert.Len(t, result, 2)
	assert.Equal(t, lua.LTrue, result[0])
	assert.Equal(t, lua.LNil, result[1])
}

func TestSendYield_HandleResult_Error(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	yield := AcquireSendYield()
	defer yield.Release()

	sendErr := errors.New("process not found")
	result := yield.HandleResult(l, process.SendResult{Error: sendErr}, nil)
	assert.Len(t, result, 2)
	assert.Equal(t, lua.LNil, result[0])
	assert.NotEqual(t, lua.LNil, result[1])

	// Verify error message contains the original error
	assert.Contains(t, result[1].String(), "not found")
}

func TestSendYield_HandleResult_DirectError(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	yield := AcquireSendYield()
	defer yield.Release()

	directErr := errors.New("direct error")
	result := yield.HandleResult(l, nil, directErr)
	assert.Len(t, result, 2)
	assert.Equal(t, lua.LNil, result[0])
	assert.NotEqual(t, lua.LNil, result[1])
}

func TestSpawnYield_HandleResult_Success(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	yield := AcquireSpawnYield()
	defer yield.Release()

	spawnedPID := pid.PID{Host: "test", UniqID: "spawned"}
	result := yield.HandleResult(l, process.SpawnResult{PID: spawnedPID}, nil)
	assert.Len(t, result, 2)
	assert.Equal(t, lua.LString(spawnedPID.String()), result[0])
	assert.Equal(t, lua.LNil, result[1])
}

func TestSpawnYield_HandleResult_Error(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	yield := AcquireSpawnYield()
	defer yield.Release()

	spawnErr := errors.New("spawn failed")
	result := yield.HandleResult(l, process.SpawnResult{Error: spawnErr}, nil)
	assert.Len(t, result, 2)
	assert.Equal(t, lua.LNil, result[0])
	assert.NotEqual(t, lua.LNil, result[1])
}

func TestSpawnYield_HandleResult_PreservesLuaErrorMetadata(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	yield := AcquireSpawnYield()
	defer yield.Release()

	original := lua.NewError("spawn failed").WithKind(lua.NotFound).WithRetryable(false)
	result := yield.HandleResult(l, process.SpawnResult{Error: original}, nil)
	assert.Len(t, result, 2)

	luaErr, ok := lua.AsError(result[1])
	assert.True(t, ok, "expected lua error userdata")
	assert.Equal(t, lua.NotFound, luaErr.Kind())
	assert.Equal(t, lua.TernaryFalse, luaErr.Retryable())
}

func TestTerminateYield_HandleResult_Success(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	yield := AcquireTerminateYield()
	defer yield.Release()

	result := yield.HandleResult(l, nil, nil)
	assert.Len(t, result, 2)
	assert.Equal(t, lua.LTrue, result[0])
	assert.Equal(t, lua.LNil, result[1])
}

func TestTerminateYield_HandleResult_Error(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	yield := AcquireTerminateYield()
	defer yield.Release()

	termErr := errors.New("terminate failed")
	result := yield.HandleResult(l, nil, termErr)
	assert.Len(t, result, 2)
	assert.Equal(t, lua.LNil, result[0])
	assert.NotEqual(t, lua.LNil, result[1])
}

func TestCancelYield_HandleResult_Success(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	yield := AcquireCancelYield()
	defer yield.Release()

	result := yield.HandleResult(l, nil, nil)
	assert.Len(t, result, 2)
	assert.Equal(t, lua.LTrue, result[0])
	assert.Equal(t, lua.LNil, result[1])
}

func TestMonitorYield_HandleResult_Success(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	yield := AcquireMonitorYield()
	defer yield.Release()

	result := yield.HandleResult(l, nil, nil)
	assert.Len(t, result, 2)
	assert.Equal(t, lua.LTrue, result[0])
	assert.Equal(t, lua.LNil, result[1])
}

func TestUnmonitorYield_HandleResult_Success(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	yield := AcquireUnmonitorYield()
	defer yield.Release()

	result := yield.HandleResult(l, nil, nil)
	assert.Len(t, result, 2)
	assert.Equal(t, lua.LTrue, result[0])
	assert.Equal(t, lua.LNil, result[1])
}

func TestLinkYield_HandleResult_Success(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	yield := AcquireLinkYield()
	defer yield.Release()

	result := yield.HandleResult(l, nil, nil)
	assert.Len(t, result, 2)
	assert.Equal(t, lua.LTrue, result[0])
	assert.Equal(t, lua.LNil, result[1])
}

func TestUnlinkYield_HandleResult_Success(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	yield := AcquireUnlinkYield()
	defer yield.Release()

	result := yield.HandleResult(l, nil, nil)
	assert.Len(t, result, 2)
	assert.Equal(t, lua.LTrue, result[0])
	assert.Equal(t, lua.LNil, result[1])
}

func TestExecYield_HandleResult_PreservesLuaErrorMetadata(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	yield := AcquireExecYield()
	defer yield.Release()

	original := lua.NewError("child failed").WithKind(lua.NotFound).WithRetryable(false)
	result := yield.HandleResult(l, process.ExecResult{
		Result: &runtimeapi.Result{Error: original},
	}, nil)
	assert.Len(t, result, 2)

	luaErr, ok := lua.AsError(result[1])
	assert.True(t, ok, "expected lua error userdata")
	assert.Equal(t, lua.NotFound, luaErr.Kind())
	assert.Equal(t, lua.TernaryFalse, luaErr.Retryable())
}

func TestYieldPooling(t *testing.T) {
	// Test that yields are properly pooled and reusable
	y1 := AcquireSendYield()
	y1.From = pid.PID{Host: "test", UniqID: "from"}
	y1.Release()

	y2 := AcquireSendYield()
	// After release and re-acquire, fields should be reset
	assert.Equal(t, pid.PID{}, y2.From)
	y2.Release()
}

func TestYieldCmdID(t *testing.T) {
	tests := []struct {
		acquire  func() interface{ CmdID() dispatcher.CommandID }
		name     string
		expected dispatcher.CommandID
	}{
		{func() interface{ CmdID() dispatcher.CommandID } { return AcquireSendYield() }, "Send", process.Send},
		{func() interface{ CmdID() dispatcher.CommandID } { return AcquireSpawnYield() }, "Spawn", process.Spawn},
		{func() interface{ CmdID() dispatcher.CommandID } { return AcquireTerminateYield() }, "Terminate", process.Terminate},
		{func() interface{ CmdID() dispatcher.CommandID } { return AcquireCancelYield() }, "Cancel", process.Cancel},
		{func() interface{ CmdID() dispatcher.CommandID } { return AcquireMonitorYield() }, "Monitor", process.Monitor},
		{func() interface{ CmdID() dispatcher.CommandID } { return AcquireUnmonitorYield() }, "Unmonitor", process.Unmonitor},
		{func() interface{ CmdID() dispatcher.CommandID } { return AcquireLinkYield() }, "Link", process.Link},
		{func() interface{ CmdID() dispatcher.CommandID } { return AcquireUnlinkYield() }, "Unlink", process.Unlink},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			y := tt.acquire()
			assert.Equal(t, tt.expected, y.CmdID())
		})
	}
}

func TestSendYield_Release_ResetsFields(t *testing.T) {
	y := AcquireSendYield()
	y.From = pid.PID{Host: "test", UniqID: "from"}
	y.Topic = "hello"
	y.Release()

	y2 := AcquireSendYield()
	assert.Equal(t, pid.PID{}, y2.From, "From should be reset after release")
	assert.Equal(t, "", y2.Topic, "Topic should be reset after release")
	y2.Release()
}
