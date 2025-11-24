package activity

import (
	temporalapi "github.com/wippyai/runtime/api/service/temporal"
	lua "github.com/yuin/gopher-lua"
	"go.temporal.io/sdk/activity"
)

// info returns activity execution information as a Lua table
func (m *Module) info(l *lua.LState) int {
	activityCtx := temporalapi.GetActivityContext(l.Context())
	if activityCtx == nil {
		l.Push(lua.LNil)
		l.Push(newActivityContextError(l, "info"))
		return 2
	}

	info := activity.GetInfo(activityCtx)

	// Create Lua table with activity info
	tbl := l.CreateTable(0, 20)

	// Identifiers
	tbl.RawSetString("workflow_id", lua.LString(info.WorkflowExecution.ID))
	tbl.RawSetString("run_id", lua.LString(info.WorkflowExecution.RunID))
	tbl.RawSetString("activity_id", lua.LString(info.ActivityID))
	tbl.RawSetString("activity_type", lua.LString(info.ActivityType.Name))
	tbl.RawSetString("workflow_type", lua.LString(info.WorkflowType.Name))

	// Queue and namespace
	tbl.RawSetString("task_queue", lua.LString(info.TaskQueue))
	tbl.RawSetString("namespace", lua.LString(info.WorkflowNamespace))

	// Attempt and flags
	tbl.RawSetString("attempt", lua.LNumber(info.Attempt))
	tbl.RawSetString("is_local", lua.LBool(info.IsLocalActivity))

	// Timeouts (convert to seconds for Lua)
	tbl.RawSetString("heartbeat_timeout", lua.LNumber(info.HeartbeatTimeout.Seconds()))
	tbl.RawSetString("schedule_to_close_timeout", lua.LNumber(info.ScheduleToCloseTimeout.Seconds()))
	tbl.RawSetString("start_to_close_timeout", lua.LNumber(info.StartToCloseTimeout.Seconds()))

	// Timestamps (Unix seconds)
	tbl.RawSetString("scheduled_time", lua.LNumber(info.ScheduledTime.Unix()))
	tbl.RawSetString("started_time", lua.LNumber(info.StartedTime.Unix()))
	tbl.RawSetString("deadline", lua.LNumber(info.Deadline.Unix()))

	// Retry policy (may be nil)
	if info.RetryPolicy != nil {
		retryPolicy := l.CreateTable(0, 5)
		retryPolicy.RawSetString("initial_interval", lua.LNumber(info.RetryPolicy.InitialInterval.Seconds()))
		retryPolicy.RawSetString("backoff_coefficient", lua.LNumber(info.RetryPolicy.BackoffCoefficient))
		retryPolicy.RawSetString("maximum_interval", lua.LNumber(info.RetryPolicy.MaximumInterval.Seconds()))
		retryPolicy.RawSetString("maximum_attempts", lua.LNumber(info.RetryPolicy.MaximumAttempts))
		tbl.RawSetString("retry_policy", retryPolicy)
	}

	l.Push(tbl)
	l.Push(lua.LNil)
	return 2
}
