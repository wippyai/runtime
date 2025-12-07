package sql

import (
	"database/sql"
	"fmt"
	"sync"

	"github.com/wippyai/runtime/api/dispatcher"
	sqlapi "github.com/wippyai/runtime/api/dispatcher/sql"
	lua "github.com/yuin/gopher-lua"
)

// anyToLua converts Go values to Lua values for SQL results.
func anyToLua(l *lua.LState, v any) lua.LValue {
	if v == nil {
		return lua.LNil
	}
	switch val := v.(type) {
	case lua.LValue:
		return val
	case string:
		return lua.LString(val)
	case []byte:
		return lua.LString(val)
	case int:
		return lua.LInteger(int64(val))
	case int32:
		return lua.LInteger(int64(val))
	case int64:
		return lua.LInteger(val)
	case float32:
		return lua.LNumber(float64(val))
	case float64:
		return lua.LNumber(val)
	case bool:
		return lua.LBool(val)
	case error:
		return lua.LString(val.Error())
	default:
		return lua.LString(fmt.Sprintf("%v", val))
	}
}

// queryResultToLua converts query response to v1 format: array of row tables with column names as keys.
// Usage: rows[1].column_name
func queryResultToLua(l *lua.LState, resp sqlapi.QueryResponse) lua.LValue {
	rows := l.CreateTable(len(resp.Rows), 0)
	for i, row := range resp.Rows {
		rowTbl := l.CreateTable(0, len(resp.Columns))
		for j, val := range row {
			if j < len(resp.Columns) {
				rowTbl.RawSetString(resp.Columns[j], anyToLua(l, val))
			}
		}
		rows.RawSetInt(i+1, rowTbl)
	}
	return rows
}

func executeResultToLua(l *lua.LState, resp sqlapi.ExecuteResponse) lua.LValue {
	tbl := l.CreateTable(0, 2)
	tbl.RawSetString("last_insert_id", lua.LInteger(resp.LastInsertID))
	tbl.RawSetString("rows_affected", lua.LInteger(resp.RowsAffected))
	return tbl
}

// QueryYield wraps QueryCmd for Lua.
type QueryYield struct {
	*sqlapi.QueryCmd
}

var queryYieldPool = sync.Pool{New: func() any { return &QueryYield{} }}

func AcquireQueryYield() *QueryYield {
	y := queryYieldPool.Get().(*QueryYield)
	y.QueryCmd = sqlapi.AcquireQueryCmd()
	return y
}

func ReleaseQueryYield(y *QueryYield) {
	if y.QueryCmd != nil {
		y.QueryCmd.Release()
		y.QueryCmd = nil
	}
	queryYieldPool.Put(y)
}

func (y *QueryYield) String() string                { return "<sql_query_yield>" }
func (y *QueryYield) Type() lua.LValueType          { return lua.LTUserData }
func (y *QueryYield) CmdID() dispatcher.CommandID   { return sqlapi.CmdQuery }
func (y *QueryYield) ToCommand() dispatcher.Command { return y.QueryCmd }
func (y *QueryYield) Release()                      { ReleaseQueryYield(y) }

func (y *QueryYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		return []lua.LValue{lua.LNil, lua.WrapErrorWithLua(l, err, "query")}
	}
	resp, ok := data.(sqlapi.QueryResponse)
	if !ok {
		return []lua.LValue{lua.LNil, lua.NewLuaError(l, "invalid response type").WithKind(lua.KindInternal)}
	}
	if resp.Error != nil {
		return []lua.LValue{lua.LNil, lua.WrapErrorWithLua(l, resp.Error, "query")}
	}
	return []lua.LValue{queryResultToLua(l, resp), lua.LNil}
}

// ExecuteYield wraps ExecuteCmd for Lua.
type ExecuteYield struct {
	*sqlapi.ExecuteCmd
}

var executeYieldPool = sync.Pool{New: func() any { return &ExecuteYield{} }}

func AcquireExecuteYield() *ExecuteYield {
	y := executeYieldPool.Get().(*ExecuteYield)
	y.ExecuteCmd = sqlapi.AcquireExecuteCmd()
	return y
}

func ReleaseExecuteYield(y *ExecuteYield) {
	if y.ExecuteCmd != nil {
		y.ExecuteCmd.Release()
		y.ExecuteCmd = nil
	}
	executeYieldPool.Put(y)
}

func (y *ExecuteYield) String() string                { return "<sql_execute_yield>" }
func (y *ExecuteYield) Type() lua.LValueType          { return lua.LTUserData }
func (y *ExecuteYield) CmdID() dispatcher.CommandID   { return sqlapi.CmdExecute }
func (y *ExecuteYield) ToCommand() dispatcher.Command { return y.ExecuteCmd }
func (y *ExecuteYield) Release()                      { ReleaseExecuteYield(y) }

func (y *ExecuteYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		return []lua.LValue{lua.LNil, lua.WrapErrorWithLua(l, err, "execute")}
	}
	resp, ok := data.(sqlapi.ExecuteResponse)
	if !ok {
		return []lua.LValue{lua.LNil, lua.NewLuaError(l, "invalid response type").WithKind(lua.KindInternal)}
	}
	if resp.Error != nil {
		return []lua.LValue{lua.LNil, lua.WrapErrorWithLua(l, resp.Error, "execute")}
	}
	return []lua.LValue{executeResultToLua(l, resp), lua.LNil}
}

// PrepareYield wraps PrepareCmd for Lua.
type PrepareYield struct {
	*sqlapi.PrepareCmd
	WrapStmt func(*sql.Stmt) lua.LValue
}

var prepareYieldPool = sync.Pool{New: func() any { return &PrepareYield{} }}

func AcquirePrepareYield() *PrepareYield {
	y := prepareYieldPool.Get().(*PrepareYield)
	y.PrepareCmd = sqlapi.AcquirePrepareCmd()
	y.WrapStmt = nil
	return y
}

func ReleasePrepareYield(y *PrepareYield) {
	if y.PrepareCmd != nil {
		y.PrepareCmd.Release()
		y.PrepareCmd = nil
	}
	y.WrapStmt = nil
	prepareYieldPool.Put(y)
}

func (y *PrepareYield) String() string                { return "<sql_prepare_yield>" }
func (y *PrepareYield) Type() lua.LValueType          { return lua.LTUserData }
func (y *PrepareYield) CmdID() dispatcher.CommandID   { return sqlapi.CmdPrepare }
func (y *PrepareYield) ToCommand() dispatcher.Command { return y.PrepareCmd }
func (y *PrepareYield) Release()                      { ReleasePrepareYield(y) }

func (y *PrepareYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		return []lua.LValue{lua.LNil, lua.WrapErrorWithLua(l, err, "prepare")}
	}
	resp, ok := data.(sqlapi.PrepareResponse)
	if !ok {
		return []lua.LValue{lua.LNil, lua.NewLuaError(l, "invalid response type").WithKind(lua.KindInternal)}
	}
	if resp.Error != nil {
		return []lua.LValue{lua.LNil, lua.WrapErrorWithLua(l, resp.Error, "prepare")}
	}
	if y.WrapStmt == nil {
		return []lua.LValue{lua.LNil, lua.NewLuaError(l, "no statement wrapper").WithKind(lua.KindInternal)}
	}
	return []lua.LValue{y.WrapStmt(resp.Stmt), lua.LNil}
}

// BeginYield wraps BeginCmd for Lua.
type BeginYield struct {
	*sqlapi.BeginCmd
	WrapTx func(*sql.Tx) lua.LValue
}

var beginYieldPool = sync.Pool{New: func() any { return &BeginYield{} }}

func AcquireBeginYield() *BeginYield {
	y := beginYieldPool.Get().(*BeginYield)
	y.BeginCmd = sqlapi.AcquireBeginCmd()
	y.WrapTx = nil
	return y
}

func ReleaseBeginYield(y *BeginYield) {
	if y.BeginCmd != nil {
		y.BeginCmd.Release()
		y.BeginCmd = nil
	}
	y.WrapTx = nil
	beginYieldPool.Put(y)
}

func (y *BeginYield) String() string                { return "<sql_begin_yield>" }
func (y *BeginYield) Type() lua.LValueType          { return lua.LTUserData }
func (y *BeginYield) CmdID() dispatcher.CommandID   { return sqlapi.CmdBegin }
func (y *BeginYield) ToCommand() dispatcher.Command { return y.BeginCmd }
func (y *BeginYield) Release()                      { ReleaseBeginYield(y) }

func (y *BeginYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		return []lua.LValue{lua.LNil, lua.WrapErrorWithLua(l, err, "begin")}
	}
	resp, ok := data.(sqlapi.BeginResponse)
	if !ok {
		return []lua.LValue{lua.LNil, lua.NewLuaError(l, "invalid response type").WithKind(lua.KindInternal)}
	}
	if resp.Error != nil {
		return []lua.LValue{lua.LNil, lua.WrapErrorWithLua(l, resp.Error, "begin")}
	}
	if y.WrapTx == nil {
		return []lua.LValue{lua.LNil, lua.NewLuaError(l, "no transaction wrapper").WithKind(lua.KindInternal)}
	}
	return []lua.LValue{y.WrapTx(resp.Tx), lua.LNil}
}

// StmtQueryYield wraps StmtQueryCmd for Lua.
type StmtQueryYield struct {
	*sqlapi.StmtQueryCmd
}

var stmtQueryYieldPool = sync.Pool{New: func() any { return &StmtQueryYield{} }}

func AcquireStmtQueryYield() *StmtQueryYield {
	y := stmtQueryYieldPool.Get().(*StmtQueryYield)
	y.StmtQueryCmd = sqlapi.AcquireStmtQueryCmd()
	return y
}

func ReleaseStmtQueryYield(y *StmtQueryYield) {
	if y.StmtQueryCmd != nil {
		y.StmtQueryCmd.Release()
		y.StmtQueryCmd = nil
	}
	stmtQueryYieldPool.Put(y)
}

func (y *StmtQueryYield) String() string                { return "<sql_stmt_query_yield>" }
func (y *StmtQueryYield) Type() lua.LValueType          { return lua.LTUserData }
func (y *StmtQueryYield) CmdID() dispatcher.CommandID   { return sqlapi.CmdStmtQuery }
func (y *StmtQueryYield) ToCommand() dispatcher.Command { return y.StmtQueryCmd }
func (y *StmtQueryYield) Release()                      { ReleaseStmtQueryYield(y) }

func (y *StmtQueryYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		return []lua.LValue{lua.LNil, lua.WrapErrorWithLua(l, err, "stmt query")}
	}
	resp, ok := data.(sqlapi.QueryResponse)
	if !ok {
		return []lua.LValue{lua.LNil, lua.NewLuaError(l, "invalid response type").WithKind(lua.KindInternal)}
	}
	if resp.Error != nil {
		return []lua.LValue{lua.LNil, lua.WrapErrorWithLua(l, resp.Error, "stmt query")}
	}
	return []lua.LValue{queryResultToLua(l, resp), lua.LNil}
}

// StmtExecuteYield wraps StmtExecuteCmd for Lua.
type StmtExecuteYield struct {
	*sqlapi.StmtExecuteCmd
}

var stmtExecuteYieldPool = sync.Pool{New: func() any { return &StmtExecuteYield{} }}

func AcquireStmtExecuteYield() *StmtExecuteYield {
	y := stmtExecuteYieldPool.Get().(*StmtExecuteYield)
	y.StmtExecuteCmd = sqlapi.AcquireStmtExecuteCmd()
	return y
}

func ReleaseStmtExecuteYield(y *StmtExecuteYield) {
	if y.StmtExecuteCmd != nil {
		y.StmtExecuteCmd.Release()
		y.StmtExecuteCmd = nil
	}
	stmtExecuteYieldPool.Put(y)
}

func (y *StmtExecuteYield) String() string                { return "<sql_stmt_execute_yield>" }
func (y *StmtExecuteYield) Type() lua.LValueType          { return lua.LTUserData }
func (y *StmtExecuteYield) CmdID() dispatcher.CommandID   { return sqlapi.CmdStmtExecute }
func (y *StmtExecuteYield) ToCommand() dispatcher.Command { return y.StmtExecuteCmd }
func (y *StmtExecuteYield) Release()                      { ReleaseStmtExecuteYield(y) }

func (y *StmtExecuteYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		return []lua.LValue{lua.LNil, lua.WrapErrorWithLua(l, err, "stmt execute")}
	}
	resp, ok := data.(sqlapi.ExecuteResponse)
	if !ok {
		return []lua.LValue{lua.LNil, lua.NewLuaError(l, "invalid response type").WithKind(lua.KindInternal)}
	}
	if resp.Error != nil {
		return []lua.LValue{lua.LNil, lua.WrapErrorWithLua(l, resp.Error, "stmt execute")}
	}
	return []lua.LValue{executeResultToLua(l, resp), lua.LNil}
}

// StmtCloseYield wraps StmtCloseCmd for Lua.
type StmtCloseYield struct {
	*sqlapi.StmtCloseCmd
	OnClose func()
}

var stmtCloseYieldPool = sync.Pool{New: func() any { return &StmtCloseYield{} }}

func AcquireStmtCloseYield() *StmtCloseYield {
	y := stmtCloseYieldPool.Get().(*StmtCloseYield)
	y.StmtCloseCmd = sqlapi.AcquireStmtCloseCmd()
	y.OnClose = nil
	return y
}

func ReleaseStmtCloseYield(y *StmtCloseYield) {
	if y.StmtCloseCmd != nil {
		y.StmtCloseCmd.Release()
		y.StmtCloseCmd = nil
	}
	y.OnClose = nil
	stmtCloseYieldPool.Put(y)
}

func (y *StmtCloseYield) String() string                { return "<sql_stmt_close_yield>" }
func (y *StmtCloseYield) Type() lua.LValueType          { return lua.LTUserData }
func (y *StmtCloseYield) CmdID() dispatcher.CommandID   { return sqlapi.CmdStmtClose }
func (y *StmtCloseYield) ToCommand() dispatcher.Command { return y.StmtCloseCmd }
func (y *StmtCloseYield) Release()                      { ReleaseStmtCloseYield(y) }

func (y *StmtCloseYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if y.OnClose != nil {
		y.OnClose()
	}
	if err != nil {
		return []lua.LValue{lua.LNil, lua.WrapErrorWithLua(l, err, "stmt close")}
	}
	return []lua.LValue{lua.LTrue, lua.LNil}
}

// TxQueryYield wraps TxQueryCmd for Lua.
type TxQueryYield struct {
	*sqlapi.TxQueryCmd
}

var txQueryYieldPool = sync.Pool{New: func() any { return &TxQueryYield{} }}

func AcquireTxQueryYield() *TxQueryYield {
	y := txQueryYieldPool.Get().(*TxQueryYield)
	y.TxQueryCmd = sqlapi.AcquireTxQueryCmd()
	return y
}

func ReleaseTxQueryYield(y *TxQueryYield) {
	if y.TxQueryCmd != nil {
		y.TxQueryCmd.Release()
		y.TxQueryCmd = nil
	}
	txQueryYieldPool.Put(y)
}

func (y *TxQueryYield) String() string                { return "<sql_tx_query_yield>" }
func (y *TxQueryYield) Type() lua.LValueType          { return lua.LTUserData }
func (y *TxQueryYield) CmdID() dispatcher.CommandID   { return sqlapi.CmdTxQuery }
func (y *TxQueryYield) ToCommand() dispatcher.Command { return y.TxQueryCmd }
func (y *TxQueryYield) Release()                      { ReleaseTxQueryYield(y) }

func (y *TxQueryYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		return []lua.LValue{lua.LNil, lua.WrapErrorWithLua(l, err, "tx query")}
	}
	resp, ok := data.(sqlapi.QueryResponse)
	if !ok {
		return []lua.LValue{lua.LNil, lua.NewLuaError(l, "invalid response type").WithKind(lua.KindInternal)}
	}
	if resp.Error != nil {
		return []lua.LValue{lua.LNil, lua.WrapErrorWithLua(l, resp.Error, "tx query")}
	}
	return []lua.LValue{queryResultToLua(l, resp), lua.LNil}
}

// TxExecuteYield wraps TxExecuteCmd for Lua.
type TxExecuteYield struct {
	*sqlapi.TxExecuteCmd
}

var txExecuteYieldPool = sync.Pool{New: func() any { return &TxExecuteYield{} }}

func AcquireTxExecuteYield() *TxExecuteYield {
	y := txExecuteYieldPool.Get().(*TxExecuteYield)
	y.TxExecuteCmd = sqlapi.AcquireTxExecuteCmd()
	return y
}

func ReleaseTxExecuteYield(y *TxExecuteYield) {
	if y.TxExecuteCmd != nil {
		y.TxExecuteCmd.Release()
		y.TxExecuteCmd = nil
	}
	txExecuteYieldPool.Put(y)
}

func (y *TxExecuteYield) String() string                { return "<sql_tx_execute_yield>" }
func (y *TxExecuteYield) Type() lua.LValueType          { return lua.LTUserData }
func (y *TxExecuteYield) CmdID() dispatcher.CommandID   { return sqlapi.CmdTxExecute }
func (y *TxExecuteYield) ToCommand() dispatcher.Command { return y.TxExecuteCmd }
func (y *TxExecuteYield) Release()                      { ReleaseTxExecuteYield(y) }

func (y *TxExecuteYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		return []lua.LValue{lua.LNil, lua.WrapErrorWithLua(l, err, "tx execute")}
	}
	resp, ok := data.(sqlapi.ExecuteResponse)
	if !ok {
		return []lua.LValue{lua.LNil, lua.NewLuaError(l, "invalid response type").WithKind(lua.KindInternal)}
	}
	if resp.Error != nil {
		return []lua.LValue{lua.LNil, lua.WrapErrorWithLua(l, resp.Error, "tx execute")}
	}
	return []lua.LValue{executeResultToLua(l, resp), lua.LNil}
}

// TxSavepointYield wraps TxExecuteCmd for savepoint operations, returns true on success (v1 compatible).
type TxSavepointYield struct {
	*sqlapi.TxExecuteCmd
}

var txSavepointYieldPool = sync.Pool{New: func() any { return &TxSavepointYield{} }}

func AcquireTxSavepointYield() *TxSavepointYield {
	y := txSavepointYieldPool.Get().(*TxSavepointYield)
	y.TxExecuteCmd = sqlapi.AcquireTxExecuteCmd()
	return y
}

func ReleaseTxSavepointYield(y *TxSavepointYield) {
	if y.TxExecuteCmd != nil {
		y.TxExecuteCmd.Release()
		y.TxExecuteCmd = nil
	}
	txSavepointYieldPool.Put(y)
}

func (y *TxSavepointYield) String() string                { return "<sql_tx_savepoint_yield>" }
func (y *TxSavepointYield) Type() lua.LValueType          { return lua.LTUserData }
func (y *TxSavepointYield) CmdID() dispatcher.CommandID   { return sqlapi.CmdTxExecute }
func (y *TxSavepointYield) ToCommand() dispatcher.Command { return y.TxExecuteCmd }
func (y *TxSavepointYield) Release()                      { ReleaseTxSavepointYield(y) }

func (y *TxSavepointYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		return []lua.LValue{lua.LNil, lua.WrapErrorWithLua(l, err, "savepoint")}
	}
	resp, ok := data.(sqlapi.ExecuteResponse)
	if !ok {
		return []lua.LValue{lua.LNil, lua.NewLuaError(l, "invalid response type").WithKind(lua.KindInternal)}
	}
	if resp.Error != nil {
		return []lua.LValue{lua.LNil, lua.WrapErrorWithLua(l, resp.Error, "savepoint")}
	}
	return []lua.LValue{lua.LTrue, lua.LNil}
}

// TxPrepareYield wraps TxPrepareCmd for Lua.
type TxPrepareYield struct {
	*sqlapi.TxPrepareCmd
	WrapStmt func(*sql.Stmt) lua.LValue
}

var txPrepareYieldPool = sync.Pool{New: func() any { return &TxPrepareYield{} }}

func AcquireTxPrepareYield() *TxPrepareYield {
	y := txPrepareYieldPool.Get().(*TxPrepareYield)
	y.TxPrepareCmd = sqlapi.AcquireTxPrepareCmd()
	y.WrapStmt = nil
	return y
}

func ReleaseTxPrepareYield(y *TxPrepareYield) {
	if y.TxPrepareCmd != nil {
		y.TxPrepareCmd.Release()
		y.TxPrepareCmd = nil
	}
	y.WrapStmt = nil
	txPrepareYieldPool.Put(y)
}

func (y *TxPrepareYield) String() string                { return "<sql_tx_prepare_yield>" }
func (y *TxPrepareYield) Type() lua.LValueType          { return lua.LTUserData }
func (y *TxPrepareYield) CmdID() dispatcher.CommandID   { return sqlapi.CmdTxPrepare }
func (y *TxPrepareYield) ToCommand() dispatcher.Command { return y.TxPrepareCmd }
func (y *TxPrepareYield) Release()                      { ReleaseTxPrepareYield(y) }

func (y *TxPrepareYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		return []lua.LValue{lua.LNil, lua.WrapErrorWithLua(l, err, "tx prepare")}
	}
	resp, ok := data.(sqlapi.PrepareResponse)
	if !ok {
		return []lua.LValue{lua.LNil, lua.NewLuaError(l, "invalid response type").WithKind(lua.KindInternal)}
	}
	if resp.Error != nil {
		return []lua.LValue{lua.LNil, lua.WrapErrorWithLua(l, resp.Error, "tx prepare")}
	}
	if y.WrapStmt == nil {
		return []lua.LValue{lua.LNil, lua.NewLuaError(l, "no statement wrapper").WithKind(lua.KindInternal)}
	}
	return []lua.LValue{y.WrapStmt(resp.Stmt), lua.LNil}
}

// TxCommitYield wraps TxCommitCmd for Lua.
type TxCommitYield struct {
	*sqlapi.TxCommitCmd
	OnComplete func()
}

var txCommitYieldPool = sync.Pool{New: func() any { return &TxCommitYield{} }}

func AcquireTxCommitYield() *TxCommitYield {
	y := txCommitYieldPool.Get().(*TxCommitYield)
	y.TxCommitCmd = sqlapi.AcquireTxCommitCmd()
	y.OnComplete = nil
	return y
}

func ReleaseTxCommitYield(y *TxCommitYield) {
	if y.TxCommitCmd != nil {
		y.TxCommitCmd.Release()
		y.TxCommitCmd = nil
	}
	y.OnComplete = nil
	txCommitYieldPool.Put(y)
}

func (y *TxCommitYield) String() string                { return "<sql_tx_commit_yield>" }
func (y *TxCommitYield) Type() lua.LValueType          { return lua.LTUserData }
func (y *TxCommitYield) CmdID() dispatcher.CommandID   { return sqlapi.CmdTxCommit }
func (y *TxCommitYield) ToCommand() dispatcher.Command { return y.TxCommitCmd }
func (y *TxCommitYield) Release()                      { ReleaseTxCommitYield(y) }

func (y *TxCommitYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if y.OnComplete != nil {
		y.OnComplete()
	}
	if err != nil {
		return []lua.LValue{lua.LNil, lua.WrapErrorWithLua(l, err, "tx commit")}
	}
	return []lua.LValue{lua.LTrue, lua.LNil}
}

// TxRollbackYield wraps TxRollbackCmd for Lua.
type TxRollbackYield struct {
	*sqlapi.TxRollbackCmd
	OnComplete func()
}

var txRollbackYieldPool = sync.Pool{New: func() any { return &TxRollbackYield{} }}

func AcquireTxRollbackYield() *TxRollbackYield {
	y := txRollbackYieldPool.Get().(*TxRollbackYield)
	y.TxRollbackCmd = sqlapi.AcquireTxRollbackCmd()
	y.OnComplete = nil
	return y
}

func ReleaseTxRollbackYield(y *TxRollbackYield) {
	if y.TxRollbackCmd != nil {
		y.TxRollbackCmd.Release()
		y.TxRollbackCmd = nil
	}
	y.OnComplete = nil
	txRollbackYieldPool.Put(y)
}

func (y *TxRollbackYield) String() string                { return "<sql_tx_rollback_yield>" }
func (y *TxRollbackYield) Type() lua.LValueType          { return lua.LTUserData }
func (y *TxRollbackYield) CmdID() dispatcher.CommandID   { return sqlapi.CmdTxRollback }
func (y *TxRollbackYield) ToCommand() dispatcher.Command { return y.TxRollbackCmd }
func (y *TxRollbackYield) Release()                      { ReleaseTxRollbackYield(y) }

func (y *TxRollbackYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if y.OnComplete != nil {
		y.OnComplete()
	}
	if err != nil {
		return []lua.LValue{lua.LNil, lua.WrapErrorWithLua(l, err, "tx rollback")}
	}
	return []lua.LValue{lua.LTrue, lua.LNil}
}
