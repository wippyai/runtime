package sql

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/resource"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/coroutine"
	"github.com/ponyruntime/pony/runtime/lua/engine/value"
	"github.com/ponyruntime/pony/runtime/lua/modules/sql/sqlutil"
	"github.com/ponyruntime/pony/runtime/lua/security"
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

// GetRawDB exposes the underlying sql.DB for external components like QueryBuilder
func (d *DB) GetRawDB() *sql.DB {
	return d.db
}

func (d *DB) GetDBType() string {
	return d.dbType
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
		"stats":   dbStats,
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

	// Add security check for accessing the database
	if !security.IsAllowed(l.Context(), "db.get", id, nil) {
		l.RaiseError("not allowed to access database: %s", id)
		return 0
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
	db := NewDB(uw, res, sqlRes.DB, sqlRes.Type, log)

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
	params, err := sqlutil.CheckParams(l, 3)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	coroutine.Wrap(l, func() *engine.Update {
		ctx := l.Context()
		var rows *sql.Rows

		// Serve query with appropriate parameter style
		switch p := params.(type) {
		case nil:
			rows, err = db.db.QueryContext(ctx, query)
		case []any:
			rows, err = db.db.QueryContext(ctx, query, p...)
		case map[string]any:
			return engine.NewUpdate(
				l,
				[]lua.LValue{lua.LNil, lua.LString("named parameters not yet implemented")},
				nil,
			)
		default:
			return engine.NewUpdate(
				l,
				[]lua.LValue{lua.LNil, lua.LString(fmt.Sprintf("unsupported parameter type: %T", params))},
				nil,
			)
		}

		if err != nil {
			return engine.NewUpdate(
				l,
				[]lua.LValue{lua.LNil, lua.LString(err.Error())},
				nil,
			)
		}

		var resultTable *lua.LTable
		err = func() error {
			defer func() {
				closeErr := rows.Close()
				if closeErr != nil {
					db.log.Error("failed to close rows", zap.Error(closeErr))
					if err == nil {
						err = closeErr
					}
				}
			}()

			var tableErr error
			resultTable, tableErr = sqlutil.RowsToTable(l, rows)
			return tableErr
		}()

		if err != nil {
			return engine.NewUpdate(
				l,
				[]lua.LValue{lua.LNil, lua.LString(err.Error())},
				nil,
			)
		}

		return engine.NewUpdate(
			l,
			[]lua.LValue{resultTable, lua.LNil},
			nil,
		)
	})

	return -1
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
	params, err := sqlutil.CheckParams(l, 3)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	coroutine.Wrap(l, func() *engine.Update {
		ctx := l.Context()
		var result sql.Result

		// Serve with appropriate parameter style
		switch p := params.(type) {
		case nil:
			result, err = db.db.ExecContext(ctx, query)
		case []interface{}:
			result, err = db.db.ExecContext(ctx, query, p...)
		case map[string]interface{}:
			return engine.NewUpdate(
				l,
				[]lua.LValue{lua.LNil, lua.LString("named parameters not yet implemented")},
				nil,
			)
		default:
			return engine.NewUpdate(
				l,
				[]lua.LValue{lua.LNil, lua.LString(fmt.Sprintf("unsupported parameter type: %T", params))},
				nil,
			)
		}

		if err != nil {
			return engine.NewUpdate(
				l,
				[]lua.LValue{lua.LNil, lua.LString(err.Error())},
				nil,
			)
		}

		// Convert result to Lua table
		resultTable := sqlutil.ResultToTable(l, result)

		return engine.NewUpdate(
			l,
			[]lua.LValue{resultTable, lua.LNil},
			nil,
		)
	})

	return -1
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
		ctx := l.Context()

		// Prepare statement
		stmt, err := db.db.PrepareContext(ctx, query)
		if err != nil {
			return engine.NewUpdate(
				l,
				[]lua.LValue{lua.LNil, lua.LString(err.Error())},
				nil,
			)
		}

		// Create statement wrapper using the constructor
		stmtObj := NewStatement(uw, stmt, db, db.log)

		// Create userdata
		ud := WrapStatement(l, stmtObj)

		return engine.NewUpdate(
			l,
			[]lua.LValue{ud, lua.LNil},
			nil,
		)
	})

	return -1
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

	// Parse transaction options if provided
	var txOptions *sql.TxOptions
	if l.GetTop() >= 2 && l.Get(2).Type() == lua.LTTable {
		optsTable := l.CheckTable(2)
		txOptions = &sql.TxOptions{}

		// Parse read_only option
		if readOnlyVal := optsTable.RawGet(lua.LString("read_only")); readOnlyVal != lua.LNil {
			if readOnlyBool, ok := readOnlyVal.(lua.LBool); ok {
				txOptions.ReadOnly = bool(readOnlyBool)
			}
		}

		// Parse isolation level
		if isolationVal := optsTable.RawGet(lua.LString("isolation")); isolationVal != lua.LNil {
			if isolationStr, ok := isolationVal.(lua.LString); ok {
				switch string(isolationStr) {
				case "default":
					txOptions.Isolation = sql.LevelDefault
				case "read_uncommitted":
					txOptions.Isolation = sql.LevelReadUncommitted
				case "read_committed":
					txOptions.Isolation = sql.LevelReadCommitted
				case "write_committed":
					txOptions.Isolation = sql.LevelWriteCommitted
				case "repeatable_read":
					txOptions.Isolation = sql.LevelRepeatableRead
				case "serializable":
					txOptions.Isolation = sql.LevelSerializable
				default:
					l.Push(lua.LNil)
					l.Push(lua.LString(fmt.Sprintf("invalid isolation level: %s", string(isolationStr))))
					return 2
				}
			}
		}
	}

	coroutine.Wrap(l, func() *engine.Update {
		ctx := l.Context()

		// Begin transaction
		tx, err := db.db.BeginTx(ctx, txOptions)
		if err != nil {
			return engine.NewUpdate(
				l,
				[]lua.LValue{lua.LNil, lua.LString(err.Error())},
				nil,
			)
		}

		// Create transaction wrapper using the constructor
		txObj := NewTransaction(uw, tx, db, db.log)

		// Create userdata
		ud := WrapTransaction(l, txObj)

		return engine.NewUpdate(
			l,
			[]lua.LValue{ud, lua.LNil},
			nil,
		)
	})

	return -1
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

// dbStats returns database connection pool statistics
func dbStats(l *lua.LState) int {
	// Check and get database
	db := CheckDB(l)
	if db == nil {
		return 0
	}

	stats := db.db.Stats()

	statsTable := l.CreateTable(0, 9)
	statsTable.RawSetString("max_open_connections", lua.LNumber(stats.MaxOpenConnections))
	statsTable.RawSetString("open_connections", lua.LNumber(stats.OpenConnections))
	statsTable.RawSetString("in_use", lua.LNumber(stats.InUse))
	statsTable.RawSetString("idle", lua.LNumber(stats.Idle))
	statsTable.RawSetString("wait_count", lua.LNumber(stats.WaitCount))
	statsTable.RawSetString("wait_duration", lua.LString(stats.WaitDuration.String()))
	statsTable.RawSetString("max_idle_closed", lua.LNumber(stats.MaxIdleClosed))
	statsTable.RawSetString("max_idle_time_closed", lua.LNumber(stats.MaxIdleTimeClosed))
	statsTable.RawSetString("max_lifetime_closed", lua.LNumber(stats.MaxLifetimeClosed))
	statsTable.Immutable = true

	l.Push(statsTable)
	l.Push(lua.LNil)
	return 2
}
