package sql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/resource"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/coroutine"
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
	dbWrapper.onRelease = uw.AddCleanup(resource.Release)

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

	coroutine.Wrap(l, func() *engine.Update {
		var rows *sql.Rows
		var err error

		// Start query with appropriate parameter style
		switch p := params.(type) {
		case nil:
			rows, err = db.db.Query(query)
		case []interface{}:
			rows, err = db.db.Query(query, p...)
		case map[string]interface{}:
			// Support for named parameters (placeholder for future implementation)
			return engine.NewUpdate(nil, nil, errors.New("named parameters not yet implemented"))
		default:
			return engine.NewUpdate(nil, nil, fmt.Errorf("unsupported parameter type: %T", params))
		}

		if err != nil {
			return engine.NewUpdate(nil, []lua.LValue{lua.LNil, lua.LString(err.Error())}, nil)
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
			return engine.NewUpdate(nil, nil, err)
		}

		return engine.NewUpdate(nil, []lua.LValue{resultTable, lua.LNil}, nil)
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

	coroutine.Wrap(l, func() *engine.Update {
		var result sql.Result
		var err error

		// Start with appropriate parameter style
		switch p := params.(type) {
		case nil:
			result, err = db.db.Exec(query)
		case []interface{}:
			result, err = db.db.Exec(query, p...)
		case map[string]interface{}:
			// Support for named parameters (placeholder for future implementation)
			return engine.NewUpdate(nil, nil, errors.New("named parameters not yet implemented"))
		default:
			return engine.NewUpdate(nil, nil, fmt.Errorf("unsupported parameter type: %T", params))
		}

		if err != nil {
			return engine.NewUpdate(nil, []lua.LValue{lua.LNil, lua.LString(err.Error())}, nil)
		}

		// Convert result to Lua table
		resultTable := resultToTable(l, result)

		return engine.NewUpdate(nil, []lua.LValue{resultTable, lua.LNil}, nil)
	})

	return -1 // Yield
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

	coroutine.Wrap(l, func() *engine.Update {
		// Prepare statement
		stmt, err := db.db.Prepare(query)
		if err != nil {
			// Return the error to Lua instead of failing the test
			return engine.NewUpdate(nil, []lua.LValue{lua.LNil, lua.LString(err.Error())}, nil)
		}

		// Create statement wrapper using the constructor
		stmtObj := NewStatement(uw, stmt, db, db.log)

		// Create userdata
		ud := WrapStatement(l, stmtObj)

		return engine.NewUpdate(nil, []lua.LValue{ud, lua.LNil}, nil)
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

	uw := engine.GetUnitOfWork(l.Context())
	if uw == nil {
		l.RaiseError("no unit of work found in context")
		return 0
	}

	coroutine.Wrap(l, func() *engine.Update {
		// Begin transaction
		tx, err := db.db.Begin()
		if err != nil {
			return engine.NewUpdate(nil, nil, err)
		}

		// Create transaction wrapper using the constructor
		txObj := NewTransaction(uw, tx, db, db.log)

		// Create userdata
		ud := WrapTransaction(l, txObj)

		return engine.NewUpdate(nil, []lua.LValue{ud, lua.LNil}, nil)
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

	// Release the resource directly
	if db.resource != nil {
		err := db.resource.Release()
		if err != nil {
			l.Push(lua.LNil)
			l.Push(lua.LString(err.Error()))
			return 2
		}
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
