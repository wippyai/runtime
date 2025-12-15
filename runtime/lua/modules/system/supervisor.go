package system

import (
	"fmt"

	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/supervisor"
	"github.com/wippyai/runtime/runtime/lua/security"
	lua "github.com/yuin/gopher-lua"
)

func supervisorState(l *lua.LState) int {
	if !security.IsAllowed(l.Context(), "system.read", "supervisor", nil) {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "permission denied: system.read on supervisor").WithKind(lua.Invalid).WithRetryable(false))
		return 2
	}

	serviceIDStr := l.CheckString(1)
	if serviceIDStr == "" {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "service ID required").WithKind(lua.Invalid).WithRetryable(false))
		return 2
	}

	serviceInfo := supervisor.GetServiceInfo(l.Context())
	if serviceInfo == nil {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "service info not available").WithKind(lua.Internal).WithRetryable(false))
		return 2
	}

	serviceID := registry.ParseID(serviceIDStr)
	state, err := serviceInfo.GetState(serviceID)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.WrapErrorWithLua(l, err, "get service state").WithKind(lua.Internal).WithRetryable(false))
		return 2
	}

	stateTable := l.CreateTable(0, 7)
	stateTable.RawSetString("id", lua.LString(state.ID.String()))
	stateTable.RawSetString("status", lua.LString(state.Status))
	stateTable.RawSetString("desired", lua.LString(state.Desired))
	stateTable.RawSetString("retry_count", lua.LNumber(state.RetryCount))
	stateTable.RawSetString("last_update", lua.LNumber(state.LastUpdate.UnixNano()))
	stateTable.RawSetString("started_at", lua.LNumber(state.StartedAt.UnixNano()))
	if state.Details != nil {
		stateTable.RawSetString("details", lua.LString(fmt.Sprintf("%v", state.Details)))
	}

	l.Push(stateTable)
	l.Push(lua.LNil)
	return 2
}

func supervisorStates(l *lua.LState) int {
	if !security.IsAllowed(l.Context(), "system.read", "supervisor", nil) {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "permission denied: system.read on supervisor").WithKind(lua.Invalid).WithRetryable(false))
		return 2
	}

	serviceInfo := supervisor.GetServiceInfo(l.Context())
	if serviceInfo == nil {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "service info not available").WithKind(lua.Internal).WithRetryable(false))
		return 2
	}

	allStates := serviceInfo.GetAllStates()
	result := l.CreateTable(len(allStates), 0)

	for i, state := range allStates {
		stateTable := l.CreateTable(0, 7)
		stateTable.RawSetString("id", lua.LString(state.ID.String()))
		stateTable.RawSetString("status", lua.LString(state.Status))
		stateTable.RawSetString("desired", lua.LString(state.Desired))
		stateTable.RawSetString("retry_count", lua.LNumber(state.RetryCount))
		stateTable.RawSetString("last_update", lua.LNumber(state.LastUpdate.UnixNano()))
		stateTable.RawSetString("started_at", lua.LNumber(state.StartedAt.UnixNano()))
		if state.Details != nil {
			stateTable.RawSetString("details", lua.LString(fmt.Sprintf("%v", state.Details)))
		}

		result.RawSetInt(i+1, stateTable)
	}

	l.Push(result)
	l.Push(lua.LNil)
	return 2
}
