package workflow

import (
	lua "github.com/wippyai/go-lua"
	attrsapi "github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	workflowapi "github.com/wippyai/runtime/api/runtime/workflow"
	luaconv "github.com/wippyai/runtime/runtime/lua/engine/payload"
	"github.com/wippyai/runtime/runtime/lua/security"
)

var (
	moduleTable *lua.LTable
	yieldTypes  []luaapi.YieldType
)

func init() {
	moduleTable = lua.CreateTable(0, 6)
	moduleTable.RawSetString("exec", lua.LGoFunc(exec))
	moduleTable.RawSetString("version", lua.LGoFunc(version))
	moduleTable.RawSetString("attrs", lua.LGoFunc(attrs))
	moduleTable.RawSetString("history_length", lua.LGoFunc(historyLength))
	moduleTable.RawSetString("history_size", lua.LGoFunc(historySize))
	moduleTable.RawSetString("info", lua.LGoFunc(info))
	moduleTable.Immutable = true

	yieldTypes = []luaapi.YieldType{
		{Sample: &ExecYield{}, CmdID: workflowapi.Exec},
		{Sample: &VersionYield{}, CmdID: workflowapi.Version},
		{Sample: &UpsertAttrsYield{}, CmdID: workflowapi.UpsertAttrs},
	}
}

// Module is the workflow module definition.
var Module = &luaapi.ModuleDef{
	Name:        "workflow",
	Description: "Workflow execution and information",
	Class:       []string{luaapi.ClassWorkflow, luaapi.ClassNondeterministic},
	Build: func() (*lua.LTable, []luaapi.YieldType) {
		return moduleTable, yieldTypes
	},
	Types: ModuleTypes,
}

// Error helpers

func invalidError(l *lua.LState, msg string) int {
	err := lua.NewLuaError(l, msg).
		WithKind(lua.Invalid).
		WithRetryable(false)
	l.Push(lua.LNil)
	l.Push(err)
	return 2
}

// exec executes a child workflow and waits for its result.
// In workflow context: yields to execute child workflow via Temporal.
// In non-workflow context: executes workflow synchronously via process function.
func exec(l *lua.LState) int {
	target := l.CheckString(1)
	if target == "" {
		return invalidError(l, "workflow ID required")
	}

	regID := registry.ParseID(target)
	if regID.NS == "" || regID.Name == "" {
		return invalidError(l, "invalid workflow ID format (namespace:name required)")
	}

	ctx := l.Context()
	if !security.IsAllowed(ctx, "workflow.exec", target, nil) {
		err := lua.NewLuaError(l, "not allowed: "+target).
			WithKind(lua.PermissionDenied).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	// Collect arguments
	var payloads []payload.Payload
	for i := 2; i <= l.GetTop(); i++ {
		payloads = append(payloads, luaconv.ExportPayload(l.Get(i)))
	}

	// Check if we're in a workflow context
	if workflowapi.IsDeterministic(ctx) {
		// In workflow context - yield to execute child workflow
		yield := &ExecYield{
			ExecCmd: workflowapi.ExecCmd{
				ID:   regID,
				Args: payloads,
			},
		}
		l.Push(yield)
		return -1
	}

	// In non-workflow context - not supported yet
	// TODO: Execute via process function service for non-workflow context
	return invalidError(l, "workflow.exec outside workflow context not yet supported")
}

// version returns the version number for a code change.
// This enables safe code updates by branching on version numbers.
func version(l *lua.LState) int {
	changeID := l.CheckString(1)
	if changeID == "" {
		return invalidError(l, "change ID required")
	}

	minSupported := l.CheckInt(2)
	maxSupported := l.CheckInt(3)

	if minSupported > maxSupported {
		return invalidError(l, "min_supported cannot be greater than max_supported")
	}

	ctx := l.Context()
	if !workflowapi.IsDeterministic(ctx) {
		// Outside workflow context, just return max version
		l.Push(lua.LNumber(maxSupported))
		l.Push(lua.LNil)
		return 2
	}

	// In workflow context - yield to get deterministic version
	yield := &VersionYield{
		VersionCmd: workflowapi.VersionCmd{
			ChangeID:     changeID,
			MinSupported: minSupported,
			MaxSupported: maxSupported,
		},
	}
	l.Push(yield)
	return -1
}

// historyLength returns the current event history length.
// Returns 0 outside of workflow context.
func historyLength(l *lua.LState) int {
	ctx := l.Context()
	info := workflowapi.GetInfo(ctx)
	if info == nil {
		l.Push(lua.LNumber(0))
		l.Push(lua.LNil)
		return 2
	}

	l.Push(lua.LNumber(info.HistoryLength))
	l.Push(lua.LNil)
	return 2
}

// historySize returns the current event history size in bytes.
// Returns 0 outside of workflow context.
func historySize(l *lua.LState) int {
	ctx := l.Context()
	info := workflowapi.GetInfo(ctx)
	if info == nil {
		l.Push(lua.LNumber(0))
		l.Push(lua.LNil)
		return 2
	}

	l.Push(lua.LNumber(info.HistorySize))
	l.Push(lua.LNil)
	return 2
}

// info returns workflow execution information as a table.
// Returns nil outside of workflow context.
func info(l *lua.LState) int {
	ctx := l.Context()
	wfInfo := workflowapi.GetInfo(ctx)
	if wfInfo == nil {
		l.Push(lua.LNil)
		l.Push(lua.LNil)
		return 2
	}

	t := l.CreateTable(0, 8)
	t.RawSetString("workflow_id", lua.LString(wfInfo.WorkflowID))
	t.RawSetString("run_id", lua.LString(wfInfo.RunID))
	t.RawSetString("workflow_type", lua.LString(wfInfo.WorkflowType))
	t.RawSetString("task_queue", lua.LString(wfInfo.TaskQueue))
	t.RawSetString("namespace", lua.LString(wfInfo.Namespace))
	t.RawSetString("attempt", lua.LNumber(wfInfo.Attempt))
	t.RawSetString("history_length", lua.LNumber(wfInfo.HistoryLength))
	t.RawSetString("history_size", lua.LNumber(wfInfo.HistorySize))

	l.Push(t)
	l.Push(lua.LNil)
	return 2
}

// attrs upserts workflow search attributes and/or memo.
// Takes a table with optional "search" and "memo" keys.
// search: indexed attributes for workflow queries
// memo: arbitrary non-indexed data
func attrs(l *lua.LState) int {
	tbl := l.CheckTable(1)

	ctx := l.Context()
	if !workflowapi.IsDeterministic(ctx) {
		return invalidError(l, "workflow.attrs only available in workflow context")
	}

	cmd := workflowapi.UpsertAttrsCmd{}

	// Extract search attributes
	if searchVal := tbl.RawGetString("search"); searchVal != lua.LNil {
		searchTbl, ok := searchVal.(*lua.LTable)
		if !ok {
			return invalidError(l, "search must be a table")
		}
		cmd.SearchAttrs = luaTableToBag(searchTbl)
	}

	// Extract memo
	if memoVal := tbl.RawGetString("memo"); memoVal != lua.LNil {
		memoTbl, ok := memoVal.(*lua.LTable)
		if !ok {
			return invalidError(l, "memo must be a table")
		}
		cmd.Memo = luaTableToBag(memoTbl)
	}

	if cmd.SearchAttrs == nil && cmd.Memo == nil {
		return invalidError(l, "at least one of search or memo required")
	}

	yield := &UpsertAttrsYield{UpsertAttrsCmd: cmd}
	l.Push(yield)
	return -1
}

// luaTableToBag converts a Lua table to attrs.Bag.
func luaTableToBag(tbl *lua.LTable) attrsapi.Bag {
	result := attrsapi.NewBag()
	tbl.ForEach(func(k, v lua.LValue) {
		if key, ok := k.(lua.LString); ok {
			result[string(key)] = luaValueToGo(v)
		}
	})
	return result
}

func luaTableToSlice(tbl *lua.LTable) ([]any, bool) {
	maxIndex := 0
	count := 0
	valid := true

	tbl.ForEach(func(k, _ lua.LValue) {
		if !valid {
			return
		}
		index, ok := k.(lua.LNumber)
		if !ok || index <= 0 || lua.LNumber(int(index)) != index {
			valid = false
			return
		}
		i := int(index)
		if i > maxIndex {
			maxIndex = i
		}
		count++
	})

	if !valid || count == 0 || count != maxIndex {
		return nil, false
	}

	result := make([]any, maxIndex)
	for i := 1; i <= maxIndex; i++ {
		value := tbl.RawGetInt(i)
		if value == lua.LNil {
			return nil, false
		}
		result[i-1] = luaValueToGo(value)
	}
	return result, true
}

// luaValueToGo converts a Lua value to a Go value.
func luaValueToGo(v lua.LValue) any {
	switch val := v.(type) {
	case lua.LString:
		return string(val)
	case lua.LNumber:
		return float64(val)
	case lua.LBool:
		return bool(val)
	case *lua.LTable:
		if sequence, ok := luaTableToSlice(val); ok {
			return sequence
		}
		return luaTableToBag(val)
	default:
		return nil
	}
}
