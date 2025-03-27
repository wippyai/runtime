package sql

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/resource"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/value"
	sqlres "github.com/ponyruntime/pony/service/sql"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

// DB represents a database connection wrapper for Lua
type DB struct {
	resource  resource.Resource[any]
	db        *sql.DB
	dbType    string
	log       *zap.Logger
	onRelease context.CancelFunc
}

// NewDB creates a new database connection wrapper with UoW integration
func NewDB(uw engine.UnitOfWork, resource resource.Resource[any], db *sql.DB, dbType string, log *zap.Logger) *DB {
	dbWrapper := &DB{
		resource: resource,
		db:       db,
		dbType:   dbType,
		log:      log,
	}

	// Register unconditional cleanup in UoW - directly pass resource.Release
	dbWrapper.onRelease = uw.AddCleanup(func() error {
		resource.Release()
		return nil
	})

	return dbWrapper
}

// WrapDB wraps a DB as a Lua userdata
func WrapDB(l *lua.LState, db *DB) *lua.LUserData {
	ud := l.NewUserData()
	ud.Value = db
	ud.Metatable = value.GetTypeMetatable(l, "sql.DB")
	return ud
}

// CheckDB checks if the first argument is a DB and returns it
func CheckDB(l *lua.LState) *DB {
	ud := l.CheckUserData(1)
	if db, ok := ud.Value.(*DB); ok {
		return db
	}
	l.ArgError(1, "expected database object")
	return nil
}

// registerDB registers database functions in the module
func registerDB(l *lua.LState, mod *lua.LTable, log *zap.Logger) {
	mod.RawSetString("get", l.NewFunction(func(l *lua.LState) int {
		return dbGet(l, log)
	}))

	methods := map[string]lua.LGFunction{
		"type":    dbType,
		"query":   dbQuery,
		"execute": dbExecute,
		"prepare": dbPrepare,
		"begin":   dbBegin,
		"release": dbRelease,
	}

	value.RegisterMethods(l, "sql.DB", methods)
}

// dbType returns the database type (kind)
func dbType(l *lua.LState) int {
	// Check and get database
	db := CheckDB(l)
	if db == nil {
		return 0
	}

	// Map the resource type to our constant type
	mappedType := mapDBTypeFromResourceKind(db.dbType)

	// Return the database type as a string
	l.Push(lua.LString(mappedType))
	l.Push(lua.LNil)
	return 2
}

// dbGet retrieves a database resource by Source
func dbGet(l *lua.LState, log *zap.Logger) int {
	// Get resource Source
	id := l.CheckString(1)
	if id == "" {
		l.Push(lua.LNil)
		l.Push(lua.LString("resource Source is required"))
		return 2
	}

	uw := engine.GetUnitOfWork(l.Context())
	if uw == nil {
		l.RaiseError("no unit of work found in context")
		return 0
	}

	reg := resource.GetResources(uw.Context())
	if reg == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("resource registry not found"))
		return 2
	}

	// Parse resource Source
	resID := registry.ParseID(id)

	// Acquire resource
	res, err := reg.Acquire(uw.Context(), resID, resource.ModeNormal)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("failed to acquire resource: %v", err)))
		return 2
	}

	// Get DB instance
	dbRes, err := res.Get()
	if err != nil {
		res.Release()
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("failed to get resource: %v", err)))
		return 2
	}

	// Check if it's a DBResource
	sqlRes, ok := dbRes.(sqlres.DBResource)
	if !ok {
		res.Release()

		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("resource is not a database: %T", dbRes)))
		return 2
	}

	// Create and wrap DB with UoW integration
	db := NewDB(uw, res, sqlRes.DB, string(sqlRes.Type), log)

	// Create userdata
	ud := WrapDB(l, db)
	l.Push(ud)
	l.Push(lua.LNil)
	return 2
}

// dbQuery executes a query and returns rows
func dbQuery(l *lua.LState) int {
	// Check and get database
	db := CheckDB(l)
	if db == nil {
		return 0
	}

	// Get query and parameters
	query := l.CheckString(2)
	params, err := checkParams(l, 3)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	var rows *sql.Rows

	// Serve query with appropriate parameter style
	switch p := params.(type) {
	case nil:
		rows, err = db.db.Query(query)
	case []interface{}:
		rows, err = db.db.Query(query, p...)
	case map[string]interface{}:
		// Support for named parameters (placeholder for future implementation)
		l.Push(lua.LNil)
		l.Push(lua.LString("named parameters not yet implemented"))
		return 2
	default:
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("unsupported parameter type: %T", params)))
		return 2
	}

	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	var resultTable *lua.LTable
	// Use a named return parameter to capture errors from both rowsToTable and rows.close
	err = func() error {
		defer func() {
			closeErr := rows.Close()
			if closeErr != nil {
				db.log.Error("failed to close rows", zap.Error(closeErr))
				// If we don't already have an error, use the close error
				if err == nil {
					err = closeErr
				}
			}
		}()

		// Convert rows to Lua table
		var tableErr error
		resultTable, tableErr = rowsToTable(l, rows)
		return tableErr
	}()

	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	l.Push(resultTable)
	l.Push(lua.LNil)
	return 2
}

// dbExecute executes a statement that doesn't return rows
func dbExecute(l *lua.LState) int {
	// Check and get database
	db := CheckDB(l)
	if db == nil {
		return 0
	}

	// Get query and parameters
	query := l.CheckString(2)
	params, err := checkParams(l, 3)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	var result sql.Result

	// Serve with appropriate parameter style
	switch p := params.(type) {
	case nil:
		result, err = db.db.Exec(query)
	case []interface{}:
		result, err = db.db.Exec(query, p...)
	case map[string]interface{}:
		// Support for named parameters (placeholder for future implementation)
		l.Push(lua.LNil)
		l.Push(lua.LString("named parameters not yet implemented"))
		return 2
	default:
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("unsupported parameter type: %T", params)))
		return 2
	}

	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	// Convert result to Lua table
	resultTable := resultToTable(l, result)

	l.Push(resultTable)
	l.Push(lua.LNil)
	return 2
}

// dbPrepare prepares a statement for repeated execution
func dbPrepare(l *lua.LState) int {
	// Check and get database
	db := CheckDB(l)
	if db == nil {
		return 0
	}

	uw := engine.GetUnitOfWork(l.Context())
	if uw == nil {
		l.RaiseError("no unit of work found in context")
		return 0
	}

	// Get query
	query := l.CheckString(2)

	// Prepare statement
	stmt, err := db.db.Prepare(query)
	if err != nil {
		// Return the error to Lua instead of failing the test
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	// Create statement wrapper using the constructor
	stmtObj := NewStatement(uw, stmt, db, db.log)

	// Create userdata
	ud := WrapStatement(l, stmtObj)

	l.Push(ud)
	l.Push(lua.LNil)
	return 2
}

// dbBegin starts a new transaction
func dbBegin(l *lua.LState) int {
	// Check and get database
	db := CheckDB(l)
	if db == nil {
		return 0
	}

	uw := engine.GetUnitOfWork(l.Context())
	if uw == nil {
		l.RaiseError("no unit of work found in context")
		return 0
	}

	// Begin transaction
	tx, err := db.db.Begin()
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	// Create transaction wrapper using the constructor
	txObj := NewTransaction(uw, tx, db, db.log)

	// Create userdata
	ud := WrapTransaction(l, txObj)

	l.Push(ud)
	l.Push(lua.LNil)
	return 2
}

// dbRelease releases a database resource
// This is a direct operation without coroutine since resource release is fast
func dbRelease(l *lua.LState) int {
	// Check and get database
	db := CheckDB(l)
	if db == nil {
		return 0
	}

	// Release the resource directly
	if db.resource != nil {
		db.resource.Release()
		db.resource = nil
	}

	// Cancel the cleanup function in UoW (don't execute it, just remove it)
	if db.onRelease != nil {
		db.onRelease()
		db.onRelease = nil
	}

	l.Push(lua.LTrue)
	l.Push(lua.LNil)
	return 2
}
