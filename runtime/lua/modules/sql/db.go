package sql

import (
	"database/sql"
	"errors"
	"fmt"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/resource"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/coroutine"
	"github.com/ponyruntime/pony/runtime/uow"
	sqlres "github.com/ponyruntime/pony/service/sql"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

// DB represents a database connection wrapper for Lua
type DB struct {
	resource resource.Resource[any]
	db       *sql.DB
	dbType   string
	log      *zap.Logger
}

// WrapDB wraps a DB as a Lua userdata
func WrapDB(l *lua.LState, db *DB) *lua.LUserData {
	ud := l.NewUserData()
	ud.Value = db
	l.SetMetatable(ud, l.GetTypeMetatable("sql.DB"))
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
	// Register module functions
	mod.RawSetString("get", l.NewFunction(func(l *lua.LState) int {
		return dbGet(l, log)
	}))

	// Register database metatable
	mt := l.NewTypeMetatable("sql.DB")
	methods := l.NewTable()

	methods.RawSetString("type", l.NewFunction(dbType))
	methods.RawSetString("query", l.NewFunction(dbQuery))
	methods.RawSetString("execute", l.NewFunction(dbExecute))
	methods.RawSetString("prepare", l.NewFunction(dbPrepare))
	methods.RawSetString("begin", l.NewFunction(dbBegin))
	methods.RawSetString("release", l.NewFunction(dbRelease))

	l.SetField(mt, "__index", methods)
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

// dbGet retrieves a database resource by ID
func dbGet(l *lua.LState, log *zap.Logger) int {
	// Get resource ID
	id := l.CheckString(1)
	if id == "" {
		l.Push(lua.LNil)
		l.Push(lua.LString("resource ID is required"))
		return 2
	}

	// Get resource registry from context
	ctx := l.Context()
	if ctx == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no context found"))
		return 2
	}

	reg := resource.GetResources(ctx)
	if reg == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("resource registry not found"))
		return 2
	}

	// Parse resource ID
	resID := registry.ParseID(id)

	// Acquire resource
	res, err := reg.Acquire(ctx, resID, resource.ModeNormal)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("failed to acquire resource: %v", err)))
		return 2
	}

	// Get DB instance
	dbRes, err := res.Get()
	if err != nil {
		// Release resource immediately since we failed
		releaseErr := res.Release()
		if releaseErr != nil {
			log.Error("failed to release resource after get failure",
				zap.Error(err),
				zap.Error(releaseErr))
		}
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("failed to get resource: %v", err)))
		return 2
	}

	// Check if it's a DBResource
	sqlRes, ok := dbRes.(sqlres.DBResource)
	if !ok {
		// Release resource immediately since it's not the right type
		releaseErr := res.Release()
		if releaseErr != nil {
			log.Error("failed to release non-DB resource",
				zap.String("resource_type", fmt.Sprintf("%T", dbRes)),
				zap.Error(releaseErr))
		}
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("resource is not a database: %T", dbRes)))
		return 2
	}

	// Create and wrap DB
	db := &DB{
		resource: res,
		db:       sqlRes.DB,
		dbType:   string(sqlRes.Type),
		log:      log,
	}

	// Add to unit of work for proper cleanup
	uw := uow.FromContext(ctx)
	if uw == nil {
		// If there's no unit of work, we can't properly manage this resource
		// Release it immediately and report an error
		releaseErr := res.Release()
		if releaseErr != nil {
			log.Error("failed to release DB resource due to missing UOW",
				zap.Error(releaseErr))
		}
		l.Push(lua.LNil)
		l.Push(lua.LString("no unit of work found to manage DB resource"))
		return 2
	}

	// Register with UOW for cleanup
	uw.AddCleanup(func() error {
		return res.Release()
	})

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

	coroutine.Wrap(l, func() *engine.Result {
		var rows *sql.Rows
		var err error

		// Execute query with appropriate parameter style
		switch p := params.(type) {
		case nil:
			rows, err = db.db.Query(query)
		case []interface{}:
			rows, err = db.db.Query(query, p...)
		case map[string]interface{}:
			// Support for named parameters (placeholder for future implementation)
			return engine.NewResult(nil, nil, errors.New("named parameters not yet implemented"))
		default:
			return engine.NewResult(nil, nil, fmt.Errorf("unsupported parameter type: %T", params))
		}

		if err != nil {
			return engine.NewResult(nil, []lua.LValue{lua.LNil, lua.LString(err.Error())}, nil)
		}

		var resultTable *lua.LTable
		// Use a named return parameter to capture errors from both rowsToTable and rows.Close
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
			return engine.NewResult(nil, nil, err)
		}

		return engine.NewResult(nil, []lua.LValue{resultTable, lua.LNil}, nil)
	})

	return -1 // Yield
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

	coroutine.Wrap(l, func() *engine.Result {
		var result sql.Result
		var err error

		// Execute with appropriate parameter style
		switch p := params.(type) {
		case nil:
			result, err = db.db.Exec(query)
		case []interface{}:
			result, err = db.db.Exec(query, p...)
		case map[string]interface{}:
			// Support for named parameters (placeholder for future implementation)
			return engine.NewResult(nil, nil, errors.New("named parameters not yet implemented"))
		default:
			return engine.NewResult(nil, nil, fmt.Errorf("unsupported parameter type: %T", params))
		}

		if err != nil {
			return engine.NewResult(nil, nil, err)
		}

		// Convert result to Lua table
		resultTable := resultToTable(l, result)

		return engine.NewResult(nil, []lua.LValue{resultTable, lua.LNil}, nil)
	})

	return -1 // Yield
}

// dbPrepare prepares a statement for repeated execution
// dbPrepare prepares a statement for repeated execution
func dbPrepare(l *lua.LState) int {
	// Check and get database
	db := CheckDB(l)
	if db == nil {
		return 0
	}

	// Get query
	query := l.CheckString(2)

	coroutine.Wrap(l, func() *engine.Result {
		// Prepare statement
		stmt, err := db.db.Prepare(query)
		if err != nil {
			// Return the error to Lua instead of failing the test
			return engine.NewResult(nil, []lua.LValue{lua.LNil, lua.LString(err.Error())}, nil)
		}

		// Create statement wrapper
		stmtObj := &Statement{
			stmt: stmt,
			db:   db,
			log:  db.log,
		}

		// Register for cleanup
		uw := uow.FromContext(l.Context())
		if uw == nil {
			// No UOW found, close the statement immediately
			closeErr := stmt.Close()
			if closeErr != nil {
				db.log.Error("failed to close statement due to missing UOW",
					zap.Error(closeErr))
			}
			return engine.NewResult(nil, []lua.LValue{lua.LNil, lua.LString("no unit of work found to manage statement")}, nil)
		}

		uw.AddCleanup(func() error {
			return stmt.Close()
		})

		// Create userdata
		ud := WrapStatement(l, stmtObj)

		return engine.NewResult(nil, []lua.LValue{ud, lua.LNil}, nil)
	})

	return -1 // Yield
}

// dbBegin starts a new transaction
func dbBegin(l *lua.LState) int {
	// Check and get database
	db := CheckDB(l)
	if db == nil {
		return 0
	}

	coroutine.Wrap(l, func() *engine.Result {
		// Begin transaction
		tx, err := db.db.Begin()
		if err != nil {
			return engine.NewResult(nil, nil, err)
		}

		// Create transaction wrapper
		txObj := &Transaction{
			tx:     tx,
			db:     db,
			log:    db.log,
			active: true,
		}

		// Register for cleanup (rollback if not committed)
		uw := uow.FromContext(l.Context())
		if uw == nil {
			// No UOW found, roll back the transaction immediately
			rollbackErr := tx.Rollback()
			if rollbackErr != nil {
				db.log.Error("failed to rollback transaction due to missing UOW",
					zap.Error(rollbackErr))
			}
			return engine.NewResult(nil, nil, errors.New("no unit of work found to manage transaction"))
		}

		uw.AddCleanup(func() error {
			// Only rollback if still active
			if txObj.active {
				return tx.Rollback()
			}
			return nil
		})

		// Create userdata
		ud := WrapTransaction(l, txObj)

		return engine.NewResult(nil, []lua.LValue{ud, lua.LNil}, nil)
	})

	return -1 // Yield
}

// dbRelease releases a database resource
// This is a direct operation without coroutine since resource release is fast
func dbRelease(l *lua.LState) int {
	// Check and get database
	db := CheckDB(l)
	if db == nil {
		return 0
	}

	// Release resource directly - no need for coroutine
	if db.resource != nil {
		if err := db.resource.Release(); err != nil {
			l.Push(lua.LNil)
			l.Push(lua.LString(err.Error()))
			return 2
		}
		db.resource = nil
	}

	l.Push(lua.LTrue)
	l.Push(lua.LNil)
	return 2
}
