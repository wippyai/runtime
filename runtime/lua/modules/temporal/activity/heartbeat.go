package activity

import (
	"fmt"

	temporalapi "github.com/wippyai/runtime/api/service/temporal"
	luaconv "github.com/wippyai/runtime/system/payload/lua"
	lua "github.com/yuin/gopher-lua"
	"go.temporal.io/sdk/activity"
)

// heartbeat records a heartbeat with optional progress details
func (m *Module) heartbeat(l *lua.LState) int {
	activityCtx := temporalapi.GetActivityContext(l.Context())
	if activityCtx == nil {
		l.Push(newActivityContextError(l, "heartbeat"))
		return 1
	}

	// Get optional details parameter
	var details interface{}
	if l.GetTop() > 0 {
		detailsValue := l.Get(1)
		if detailsValue != lua.LNil {
			payload := luaconv.ExportPayload(detailsValue)
			details = payload.Data()
		}
	}

	// Record heartbeat
	activity.RecordHeartbeat(activityCtx, details)

	l.Push(lua.LNil)
	return 1
}

// getHeartbeatDetails retrieves the last heartbeat details (for retry resume)
func (m *Module) getHeartbeatDetails(l *lua.LState) int {
	activityCtx := temporalapi.GetActivityContext(l.Context())
	if activityCtx == nil {
		l.Push(lua.LNil)
		l.Push(newActivityContextError(l, "get_heartbeat_details"))
		return 2
	}

	// Get heartbeat details
	var details interface{}
	if err := activity.GetHeartbeatDetails(activityCtx, &details); err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	// Convert to Lua value
	if details == nil {
		l.Push(lua.LNil)
	} else {
		lval, err := luaconv.GoToLua(details)
		if err != nil {
			l.Push(lua.LNil)
			l.Push(lua.LString(fmt.Sprintf("failed to convert heartbeat details: %v", err)))
			return 2
		}
		l.Push(lval)
	}
	l.Push(lua.LNil)
	return 2
}

// hasHeartbeatDetails checks if heartbeat details exist
func (m *Module) hasHeartbeatDetails(l *lua.LState) int {
	activityCtx := temporalapi.GetActivityContext(l.Context())
	if activityCtx == nil {
		l.Push(lua.LFalse)
		return 1
	}

	has := activity.HasHeartbeatDetails(activityCtx)
	l.Push(lua.LBool(has))
	return 1
}
