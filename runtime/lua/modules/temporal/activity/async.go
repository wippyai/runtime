package activity

import (
	temporalapi "github.com/wippyai/runtime/api/service/temporal"
	lua "github.com/yuin/gopher-lua"
)

// asyncComplete marks the activity for asynchronous completion
// The activity should return after calling this, and be completed later via client.CompleteActivity()
func (m *Module) asyncComplete(l *lua.LState) int {
	activityCtx := temporalapi.GetActivityContext(l.Context())
	if activityCtx == nil {
		l.Push(newActivityContextError(l, "async_complete"))
		return 1
	}

	// Return nil error - the activity handler will return activity.ErrResultPending
	// based on special handling (needs to be implemented in worker)
	l.Push(lua.LNil)
	return 1
}

// isCanceled checks if the activity has been canceled
func (m *Module) isCanceled(l *lua.LState) int {
	activityCtx := temporalapi.GetActivityContext(l.Context())
	if activityCtx == nil {
		l.Push(lua.LFalse)
		return 1
	}

	// Check if context is canceled
	select {
	case <-activityCtx.Done():
		l.Push(lua.LTrue)
	default:
		l.Push(lua.LFalse)
	}
	return 1
}
