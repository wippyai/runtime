package sql

import (
	"database/sql"
	"sync"

	"github.com/wippyai/runtime/api/dispatcher"
)

func init() {
	dispatcher.MustRegisterCommands("sql",
		Query, Execute, Prepare, Begin,
		StmtQuery, StmtExecute, StmtClose,
		TxQuery, TxExecute, TxPrepare, TxCommit, TxRollback,
	)
}

// Command IDs for SQL operations.
// Range 100-149 is reserved for database commands.
const (
	Query   dispatcher.CommandID = 100 // Execute query, returns rows
	Execute dispatcher.CommandID = 101 // Execute statement, returns result
	Prepare dispatcher.CommandID = 102 // Prepare statement
	Begin   dispatcher.CommandID = 103 // Begin transaction

	StmtQuery   dispatcher.CommandID = 104 // Execute prepared statement query
	StmtExecute dispatcher.CommandID = 105 // Execute prepared statement
	StmtClose   dispatcher.CommandID = 106 // Close prepared statement

	TxQuery    dispatcher.CommandID = 107 // Execute query in transaction
	TxExecute  dispatcher.CommandID = 108 // Execute statement in transaction
	TxPrepare  dispatcher.CommandID = 109 // Prepare statement in transaction
	TxCommit   dispatcher.CommandID = 110 // Commit transaction
	TxRollback dispatcher.CommandID = 111 // Rollback transaction
)

// QueryCmd executes a query and returns rows.
type QueryCmd struct {
	DB     *sql.DB
	Query  string
	Params []any
}

var queryCmdPool = sync.Pool{New: func() any { return &QueryCmd{} }}

func AcquireQueryCmd() *QueryCmd                { return queryCmdPool.Get().(*QueryCmd) }
func (c *QueryCmd) CmdID() dispatcher.CommandID { return Query }
func (c *QueryCmd) Release() {
	c.DB = nil
	c.Query = ""
	c.Params = nil
	queryCmdPool.Put(c)
}

// ExecuteCmd executes a statement without returning rows.
type ExecuteCmd struct {
	DB     *sql.DB
	Query  string
	Params []any
}

var executeCmdPool = sync.Pool{New: func() any { return &ExecuteCmd{} }}

func AcquireExecuteCmd() *ExecuteCmd              { return executeCmdPool.Get().(*ExecuteCmd) }
func (c *ExecuteCmd) CmdID() dispatcher.CommandID { return Execute }
func (c *ExecuteCmd) Release() {
	c.DB = nil
	c.Query = ""
	c.Params = nil
	executeCmdPool.Put(c)
}

// PrepareCmd prepares a statement.
type PrepareCmd struct {
	DB    *sql.DB
	Query string
}

var prepareCmdPool = sync.Pool{New: func() any { return &PrepareCmd{} }}

func AcquirePrepareCmd() *PrepareCmd              { return prepareCmdPool.Get().(*PrepareCmd) }
func (c *PrepareCmd) CmdID() dispatcher.CommandID { return Prepare }
func (c *PrepareCmd) Release() {
	c.DB = nil
	c.Query = ""
	prepareCmdPool.Put(c)
}

// BeginCmd starts a transaction.
type BeginCmd struct {
	DB      *sql.DB
	Options *sql.TxOptions
}

var beginCmdPool = sync.Pool{New: func() any { return &BeginCmd{} }}

func AcquireBeginCmd() *BeginCmd                { return beginCmdPool.Get().(*BeginCmd) }
func (c *BeginCmd) CmdID() dispatcher.CommandID { return Begin }
func (c *BeginCmd) Release() {
	c.DB = nil
	c.Options = nil
	beginCmdPool.Put(c)
}

// StmtQueryCmd executes a prepared statement query.
type StmtQueryCmd struct {
	Stmt   *sql.Stmt
	Params []any
}

var stmtQueryCmdPool = sync.Pool{New: func() any { return &StmtQueryCmd{} }}

func AcquireStmtQueryCmd() *StmtQueryCmd            { return stmtQueryCmdPool.Get().(*StmtQueryCmd) }
func (c *StmtQueryCmd) CmdID() dispatcher.CommandID { return StmtQuery }
func (c *StmtQueryCmd) Release() {
	c.Stmt = nil
	c.Params = nil
	stmtQueryCmdPool.Put(c)
}

// StmtExecuteCmd executes a prepared statement.
type StmtExecuteCmd struct {
	Stmt   *sql.Stmt
	Params []any
}

var stmtExecuteCmdPool = sync.Pool{New: func() any { return &StmtExecuteCmd{} }}

func AcquireStmtExecuteCmd() *StmtExecuteCmd          { return stmtExecuteCmdPool.Get().(*StmtExecuteCmd) }
func (c *StmtExecuteCmd) CmdID() dispatcher.CommandID { return StmtExecute }
func (c *StmtExecuteCmd) Release() {
	c.Stmt = nil
	c.Params = nil
	stmtExecuteCmdPool.Put(c)
}

// StmtCloseCmd closes a prepared statement.
type StmtCloseCmd struct {
	Stmt *sql.Stmt
}

var stmtCloseCmdPool = sync.Pool{New: func() any { return &StmtCloseCmd{} }}

func AcquireStmtCloseCmd() *StmtCloseCmd            { return stmtCloseCmdPool.Get().(*StmtCloseCmd) }
func (c *StmtCloseCmd) CmdID() dispatcher.CommandID { return StmtClose }
func (c *StmtCloseCmd) Release() {
	c.Stmt = nil
	stmtCloseCmdPool.Put(c)
}

// TxQueryCmd executes a query in a transaction.
type TxQueryCmd struct {
	Tx     *sql.Tx
	Query  string
	Params []any
}

var txQueryCmdPool = sync.Pool{New: func() any { return &TxQueryCmd{} }}

func AcquireTxQueryCmd() *TxQueryCmd              { return txQueryCmdPool.Get().(*TxQueryCmd) }
func (c *TxQueryCmd) CmdID() dispatcher.CommandID { return TxQuery }
func (c *TxQueryCmd) Release() {
	c.Tx = nil
	c.Query = ""
	c.Params = nil
	txQueryCmdPool.Put(c)
}

// TxExecuteCmd executes a statement in a transaction.
type TxExecuteCmd struct {
	Tx     *sql.Tx
	Query  string
	Params []any
}

var txExecuteCmdPool = sync.Pool{New: func() any { return &TxExecuteCmd{} }}

func AcquireTxExecuteCmd() *TxExecuteCmd            { return txExecuteCmdPool.Get().(*TxExecuteCmd) }
func (c *TxExecuteCmd) CmdID() dispatcher.CommandID { return TxExecute }
func (c *TxExecuteCmd) Release() {
	c.Tx = nil
	c.Query = ""
	c.Params = nil
	txExecuteCmdPool.Put(c)
}

// TxPrepareCmd prepares a statement in a transaction.
type TxPrepareCmd struct {
	Tx    *sql.Tx
	Query string
}

var txPrepareCmdPool = sync.Pool{New: func() any { return &TxPrepareCmd{} }}

func AcquireTxPrepareCmd() *TxPrepareCmd            { return txPrepareCmdPool.Get().(*TxPrepareCmd) }
func (c *TxPrepareCmd) CmdID() dispatcher.CommandID { return TxPrepare }
func (c *TxPrepareCmd) Release() {
	c.Tx = nil
	c.Query = ""
	txPrepareCmdPool.Put(c)
}

// TxCommitCmd commits a transaction.
type TxCommitCmd struct {
	Tx *sql.Tx
}

var txCommitCmdPool = sync.Pool{New: func() any { return &TxCommitCmd{} }}

func AcquireTxCommitCmd() *TxCommitCmd             { return txCommitCmdPool.Get().(*TxCommitCmd) }
func (c *TxCommitCmd) CmdID() dispatcher.CommandID { return TxCommit }
func (c *TxCommitCmd) Release() {
	c.Tx = nil
	txCommitCmdPool.Put(c)
}

// TxRollbackCmd rolls back a transaction.
type TxRollbackCmd struct {
	Tx *sql.Tx
}

var txRollbackCmdPool = sync.Pool{New: func() any { return &TxRollbackCmd{} }}

func AcquireTxRollbackCmd() *TxRollbackCmd           { return txRollbackCmdPool.Get().(*TxRollbackCmd) }
func (c *TxRollbackCmd) CmdID() dispatcher.CommandID { return TxRollback }
func (c *TxRollbackCmd) Release() {
	c.Tx = nil
	txRollbackCmdPool.Put(c)
}

// QueryResponse contains query results.
type QueryResponse struct {
	Columns []string
	Rows    [][]any
	Error   error
}

// ExecuteResponse contains execute results.
type ExecuteResponse struct {
	LastInsertID int64
	RowsAffected int64
	Error        error
}

// PrepareResponse contains prepared statement.
type PrepareResponse struct {
	Stmt  *sql.Stmt
	Error error
}

// BeginResponse contains transaction.
type BeginResponse struct {
	Tx    *sql.Tx
	Error error
}
