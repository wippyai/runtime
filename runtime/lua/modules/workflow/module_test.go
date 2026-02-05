package workflow

import (
	"errors"
	"testing"

	lua "github.com/wippyai/go-lua"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	workflowapi "github.com/wippyai/runtime/api/runtime/workflow"
	"github.com/wippyai/runtime/runtime/lua/engine/value"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Module definition ---

func TestModuleBuild(t *testing.T) {
	table, yields := Module.Build()

	require.NotNil(t, table)
	assert.Equal(t, lua.LTFunction, table.RawGetString("exec").Type())
	assert.Equal(t, lua.LTFunction, table.RawGetString("version").Type())
	assert.Equal(t, lua.LTFunction, table.RawGetString("attrs").Type())
	assert.Equal(t, lua.LTFunction, table.RawGetString("history_length").Type())
	assert.Equal(t, lua.LTFunction, table.RawGetString("history_size").Type())
	assert.Equal(t, lua.LTFunction, table.RawGetString("info").Type())

	assert.Len(t, yields, 3)
}

func TestModuleBuildReuse(t *testing.T) {
	t1, _ := Module.Build()
	t2, _ := Module.Build()
	assert.Same(t, t1, t2)
}

func TestModuleImmutable(t *testing.T) {
	table, _ := Module.Build()
	assert.True(t, table.Immutable)
}

func TestModuleInfo(t *testing.T) {
	assert.Equal(t, "workflow", Module.Name)
	assert.NotEmpty(t, Module.Description)
	assert.Contains(t, Module.Class, "workflow")
}

func TestModuleTypes(t *testing.T) {
	m := ModuleTypes()
	require.NotNil(t, m)
}

// --- luaTableToMap / luaValueToGo ---

func TestLuaValueToGo_String(t *testing.T) {
	assert.Equal(t, "hello", luaValueToGo(lua.LString("hello")))
}

func TestLuaValueToGo_Number(t *testing.T) {
	assert.Equal(t, float64(42), luaValueToGo(lua.LNumber(42)))
}

func TestLuaValueToGo_Bool(t *testing.T) {
	assert.Equal(t, true, luaValueToGo(lua.LTrue))
	assert.Equal(t, false, luaValueToGo(lua.LFalse))
}

func TestLuaValueToGo_Nil(t *testing.T) {
	assert.Nil(t, luaValueToGo(lua.LNil))
}

func TestLuaTableToMap(t *testing.T) {
	tbl := lua.CreateTable(0, 3)
	tbl.RawSetString("name", lua.LString("test"))
	tbl.RawSetString("count", lua.LNumber(5))
	tbl.RawSetString("active", lua.LTrue)

	result := luaTableToMap(tbl)
	assert.Equal(t, "test", result["name"])
	assert.Equal(t, float64(5), result["count"])
	assert.Equal(t, true, result["active"])
}

func TestLuaTableToMap_NestedTable(t *testing.T) {
	inner := lua.CreateTable(0, 1)
	inner.RawSetString("key", lua.LString("value"))

	outer := lua.CreateTable(0, 1)
	outer.RawSetString("nested", inner)

	result := luaTableToMap(outer)
	nested, ok := result["nested"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "value", nested["key"])
}

func TestLuaTableToMap_IntegerKeysSkipped(t *testing.T) {
	tbl := lua.CreateTable(2, 0)
	tbl.RawSetInt(1, lua.LString("first"))
	tbl.RawSetString("key", lua.LString("value"))

	result := luaTableToMap(tbl)
	assert.Equal(t, "value", result["key"])
	assert.Nil(t, result["1"])
}

// --- helpers ---

func newLuaWithWorkflowCtx(t *testing.T) (*lua.LState, func()) {
	t.Helper()
	l := lua.NewState()
	tbl, _ := Module.Build()
	l.SetGlobal("workflow", tbl)

	ctx, fc := ctxapi.OpenFrameContext(l.Context())
	require.NoError(t, workflowapi.SetDeterministic(ctx))
	l.SetContext(ctx)

	return l, func() {
		ctxapi.ReleaseFrameContext(fc)
		l.Close()
	}
}

func newLuaWithoutWorkflowCtx(t *testing.T) (*lua.LState, func()) {
	t.Helper()
	l := lua.NewState()
	tbl, _ := Module.Build()
	l.SetGlobal("workflow", tbl)

	ctx, fc := ctxapi.OpenFrameContext(l.Context())
	l.SetContext(ctx)

	return l, func() {
		ctxapi.ReleaseFrameContext(fc)
		l.Close()
	}
}

// --- version ---

func TestVersion_OutsideWorkflow_ReturnsMax(t *testing.T) {
	l, cleanup := newLuaWithoutWorkflowCtx(t)
	defer cleanup()

	err := l.DoString(`
		local v, e = workflow.version("change-1", 1, 3)
		assert(v == 3, "expected max version 3, got " .. tostring(v))
		assert(e == nil, "expected no error")
	`)
	assert.NoError(t, err)
}

func TestVersion_MinGreaterThanMax(t *testing.T) {
	l, cleanup := newLuaWithoutWorkflowCtx(t)
	defer cleanup()

	err := l.DoString(`
		local v, e = workflow.version("change-1", 5, 3)
		assert(v == nil, "expected nil on validation error")
		assert(e ~= nil, "expected error for min > max")
	`)
	assert.NoError(t, err)
}

// --- historyLength / historySize ---

type fakeInfoProvider struct {
	info workflowapi.Info
}

func (p *fakeInfoProvider) GetWorkflowInfo() workflowapi.Info { return p.info }

func TestHistoryLength_NoInfo(t *testing.T) {
	l, cleanup := newLuaWithoutWorkflowCtx(t)
	defer cleanup()

	err := l.DoString(`
		local v, e = workflow.history_length()
		assert(v == 0, "expected 0 without info, got " .. tostring(v))
		assert(e == nil)
	`)
	assert.NoError(t, err)
}

func TestHistoryLength_WithInfo(t *testing.T) {
	l, cleanup := newLuaWithWorkflowCtx(t)
	defer cleanup()

	ctx := l.Context()
	err := workflowapi.SetInfoProvider(ctx, &fakeInfoProvider{
		info: workflowapi.Info{HistoryLength: 42},
	})
	require.NoError(t, err)

	err = l.DoString(`
		local v, e = workflow.history_length()
		assert(v == 42, "expected 42, got " .. tostring(v))
		assert(e == nil)
	`)
	assert.NoError(t, err)
}

func TestHistorySize_NoInfo(t *testing.T) {
	l, cleanup := newLuaWithoutWorkflowCtx(t)
	defer cleanup()

	err := l.DoString(`
		local v, e = workflow.history_size()
		assert(v == 0, "expected 0 without info, got " .. tostring(v))
		assert(e == nil)
	`)
	assert.NoError(t, err)
}

func TestHistorySize_WithInfo(t *testing.T) {
	l, cleanup := newLuaWithWorkflowCtx(t)
	defer cleanup()

	ctx := l.Context()
	err := workflowapi.SetInfoProvider(ctx, &fakeInfoProvider{
		info: workflowapi.Info{HistorySize: 1024},
	})
	require.NoError(t, err)

	err = l.DoString(`
		local v, e = workflow.history_size()
		assert(v == 1024, "expected 1024, got " .. tostring(v))
		assert(e == nil)
	`)
	assert.NoError(t, err)
}

// --- info ---

func TestInfo_NoWorkflowInfo(t *testing.T) {
	l, cleanup := newLuaWithoutWorkflowCtx(t)
	defer cleanup()

	err := l.DoString(`
		local v, e = workflow.info()
		assert(v == nil, "expected nil without info")
		assert(e == nil)
	`)
	assert.NoError(t, err)
}

func TestInfo_WithWorkflowInfo(t *testing.T) {
	l, cleanup := newLuaWithWorkflowCtx(t)
	defer cleanup()

	ctx := l.Context()
	err := workflowapi.SetInfoProvider(ctx, &fakeInfoProvider{
		info: workflowapi.Info{
			WorkflowID:    "wf-123",
			RunID:         "run-456",
			WorkflowType:  "test-type",
			TaskQueue:     "default",
			Namespace:     "production",
			Attempt:       2,
			HistoryLength: 100,
			HistorySize:   5000,
		},
	})
	require.NoError(t, err)

	err = l.DoString(`
		local info, e = workflow.info()
		assert(e == nil, "expected no error")
		assert(info ~= nil, "expected info table")
		assert(info.workflow_id == "wf-123", "wrong workflow_id: " .. tostring(info.workflow_id))
		assert(info.run_id == "run-456", "wrong run_id")
		assert(info.workflow_type == "test-type", "wrong type")
		assert(info.task_queue == "default", "wrong queue")
		assert(info.namespace == "production", "wrong namespace")
		assert(info.attempt == 2, "wrong attempt: " .. tostring(info.attempt))
		assert(info.history_length == 100, "wrong history_length")
		assert(info.history_size == 5000, "wrong history_size")
	`)
	assert.NoError(t, err)
}

// --- Yield types ---

func TestExecYield_Type(t *testing.T) {
	y := &ExecYield{}
	assert.Equal(t, lua.LTUserData, y.Type())
	assert.Equal(t, "<exec_yield>", y.String())
	assert.Equal(t, workflowapi.Exec, y.CmdID())
}

func TestExecYield_ToCommand(t *testing.T) {
	y := &ExecYield{}
	cmd := y.ToCommand()
	require.NotNil(t, cmd)
	assert.Equal(t, workflowapi.Exec, cmd.CmdID())
}

func TestVersionYield_Type(t *testing.T) {
	y := &VersionYield{}
	assert.Equal(t, lua.LTUserData, y.Type())
	assert.Equal(t, "<version_yield>", y.String())
	assert.Equal(t, workflowapi.Version, y.CmdID())
}

func TestVersionYield_ToCommand(t *testing.T) {
	y := &VersionYield{}
	cmd := y.ToCommand()
	require.NotNil(t, cmd)
	assert.Equal(t, workflowapi.Version, cmd.CmdID())
}

func TestUpsertAttrsYield_Type(t *testing.T) {
	y := &UpsertAttrsYield{}
	assert.Equal(t, lua.LTUserData, y.Type())
	assert.Equal(t, "<upsert_attrs_yield>", y.String())
	assert.Equal(t, workflowapi.UpsertAttrs, y.CmdID())
}

func TestUpsertAttrsYield_ToCommand(t *testing.T) {
	y := &UpsertAttrsYield{}
	cmd := y.ToCommand()
	require.NotNil(t, cmd)
	assert.Equal(t, workflowapi.UpsertAttrs, cmd.CmdID())
}

// --- ExecYield.HandleResult ---

func TestExecYield_HandleResult_Error(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	y := &ExecYield{}
	result := y.HandleResult(l, nil, errors.New("connection lost"))

	require.Len(t, result, 2)
	assert.Equal(t, lua.LNil, result[0])
	assert.NotEqual(t, lua.LNil, result[1])
}

func TestExecYield_HandleResult_WrongType(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	y := &ExecYield{}
	result := y.HandleResult(l, "not an ExecResult", nil)

	require.Len(t, result, 2)
	assert.Equal(t, lua.LNil, result[0])
	assert.NotEqual(t, lua.LNil, result[1])
}

func TestExecYield_HandleResult_ResultWithError(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	y := &ExecYield{}
	execResult := workflowapi.ExecResult{Error: errors.New("child failed")}
	result := y.HandleResult(l, execResult, nil)

	require.Len(t, result, 2)
	assert.Equal(t, lua.LNil, result[0])
	assert.NotEqual(t, lua.LNil, result[1])
}

func TestExecYield_HandleResult_NilValue(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	y := &ExecYield{}
	execResult := workflowapi.ExecResult{Value: nil, Error: nil}
	result := y.HandleResult(l, execResult, nil)

	require.Len(t, result, 2)
	assert.Equal(t, lua.LNil, result[0])
	assert.Equal(t, lua.LNil, result[1])
}

func TestExecYield_HandleResult_LuaPayload(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	lv := lua.LString("hello from child")
	p := payload.NewPayload(lv, payload.Lua)

	y := &ExecYield{}
	execResult := workflowapi.ExecResult{Value: p}
	result := y.HandleResult(l, execResult, nil)

	require.Len(t, result, 2)
	assert.Equal(t, lua.LString("hello from child"), result[0])
	assert.Equal(t, lua.LNil, result[1])
}

func TestExecYield_HandleResult_NonLuaPayload_NoContext(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	p := payload.NewPayload([]byte(`"test"`), payload.JSON)
	y := &ExecYield{}
	execResult := workflowapi.ExecResult{Value: p}
	result := y.HandleResult(l, execResult, nil)

	require.Len(t, result, 2)
	assert.Equal(t, lua.LNil, result[0])
	assert.NotEqual(t, lua.LNil, result[1])
}

// --- VersionYield.HandleResult ---

func TestVersionYield_HandleResult_Success(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	y := &VersionYield{VersionCmd: workflowapi.VersionCmd{MaxSupported: 5}}
	result := y.HandleResult(l, workflowapi.VersionResult{Version: 3}, nil)

	require.Len(t, result, 2)
	assert.Equal(t, lua.LNumber(3), result[0])
	assert.Equal(t, lua.LNil, result[1])
}

func TestVersionYield_HandleResult_Error_FallsBackToMax(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	y := &VersionYield{VersionCmd: workflowapi.VersionCmd{MaxSupported: 7}}
	result := y.HandleResult(l, nil, errors.New("version lookup failed"))

	require.Len(t, result, 2)
	assert.Equal(t, lua.LNumber(7), result[0])
	assert.Equal(t, lua.LNil, result[1])
}

func TestVersionYield_HandleResult_WrongType_FallsBackToMax(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	y := &VersionYield{VersionCmd: workflowapi.VersionCmd{MaxSupported: 4}}
	result := y.HandleResult(l, "not a version result", nil)

	require.Len(t, result, 2)
	assert.Equal(t, lua.LNumber(4), result[0])
	assert.Equal(t, lua.LNil, result[1])
}

// --- UpsertAttrsYield.HandleResult ---

func TestUpsertAttrsYield_HandleResult_Success(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	y := &UpsertAttrsYield{}
	result := y.HandleResult(l, nil, nil)

	require.Len(t, result, 2)
	assert.Equal(t, lua.LTrue, result[0])
	assert.Equal(t, lua.LNil, result[1])
}

func TestUpsertAttrsYield_HandleResult_Error(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	y := &UpsertAttrsYield{}
	result := y.HandleResult(l, nil, errors.New("upsert failed"))

	require.Len(t, result, 2)
	assert.Equal(t, lua.LNil, result[0])
	assert.NotEqual(t, lua.LNil, result[1])
}

// --- exec validation ---

func TestExec_EmptyID(t *testing.T) {
	l, cleanup := newLuaWithWorkflowCtx(t)
	defer cleanup()

	err := l.DoString(`
		local v, e = workflow.exec("")
		assert(v == nil, "expected nil on empty ID")
		assert(e ~= nil, "expected error for empty ID")
	`)
	assert.NoError(t, err)
}

func TestExec_InvalidIDFormat(t *testing.T) {
	l, cleanup := newLuaWithWorkflowCtx(t)
	defer cleanup()

	err := l.DoString(`
		local v, e = workflow.exec("no-namespace")
		assert(v == nil, "expected nil on invalid ID")
		assert(e ~= nil, "expected error for invalid ID format")
	`)
	assert.NoError(t, err)
}

func TestExec_OutsideWorkflow(t *testing.T) {
	l, cleanup := newLuaWithoutWorkflowCtx(t)
	defer cleanup()

	err := l.DoString(`
		local v, e = workflow.exec("ns:name")
		assert(v == nil, "expected nil outside workflow")
		assert(e ~= nil, "expected error outside workflow")
	`)
	assert.NoError(t, err)
}

// --- attrs validation ---

func TestAttrs_OutsideWorkflow(t *testing.T) {
	l, cleanup := newLuaWithoutWorkflowCtx(t)
	defer cleanup()

	err := l.DoString(`
		local ok, e = workflow.attrs({search = {key = "val"}})
		assert(ok == nil, "expected nil outside workflow")
		assert(e ~= nil, "expected error outside workflow")
	`)
	assert.NoError(t, err)
}

// --- value.GetTypeMetatable ---

func TestYieldTypes_Registered(t *testing.T) {
	_, yields := Module.Build()
	assert.Len(t, yields, 3)
	assert.Equal(t, workflowapi.Exec, yields[0].CmdID)
	assert.Equal(t, workflowapi.Version, yields[1].CmdID)
	assert.Equal(t, workflowapi.UpsertAttrs, yields[2].CmdID)
}

// silence unused import
var _ = value.GetTypeMetatable
