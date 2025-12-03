package sql

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/resource"
	rtresource "github.com/wippyai/runtime/api/runtime/resource"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	"github.com/wippyai/runtime/runtime/lua/security"
	sqlres "github.com/wippyai/runtime/service/sql"
	lua "github.com/yuin/gopher-lua"
)

type DB struct {
	resource      resource.Resource[any]
	db            *sql.DB
	dbType        string
	released      bool
	cancelCleanup func()
}

func (d *DB) GetRawDB() *sql.DB {
	return d.db
}

func (d *DB) GetDBType() string {
	return d.dbType
}

func NewDB(ctx context.Context, res resource.Resource[any], db *sql.DB, dbType string) *DB {
	dbWrapper := &DB{
		resource: res,
		db:       db,
		dbType:   dbType,
		released: false,
	}

	store := rtresource.GetStore(ctx)
	if store != nil {
		dbWrapper.cancelCleanup = store.AddCleanup(func() error {
			if !dbWrapper.released && dbWrapper.resource != nil {
				dbWrapper.resource.Release()
			}
			return nil
		})
	}

	return dbWrapper
}

var dbMethods = map[string]lua.LGFunction{
	"type":    dbType,
	"query":   dbQuery,
	"execute": dbExecute,
	"prepare": dbPrepare,
	"begin":   dbBegin,
	"release": dbRelease,
	"stats":   dbStats,
}

func checkDB(l *lua.LState, idx int) *DB {
	ud := l.CheckUserData(idx)
	if v, ok := ud.Value.(*DB); ok {
		return v
	}
	l.ArgError(idx, "database expected")
	return nil
}

func sqlGet(l *lua.LState) int {
	ctx := l.Context()
	if ctx == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no context"))
		return 2
	}

	id := l.CheckString(1)
	if id == "" {
		l.Push(lua.LNil)
		l.Push(lua.LString("resource id is required"))
		return 2
	}

	if !security.IsAllowed(ctx, "db.get", id, nil) {
		l.RaiseError("not allowed to access database: %s", id)
		return 0
	}

	reg := resource.GetRegistry(ctx)
	if reg == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("resource registry not found"))
		return 2
	}

	resID := registry.ParseID(id)
	res, err := reg.Acquire(ctx, resID, resource.ModeNormal)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("failed to acquire resource: %v", err)))
		return 2
	}

	dbRes, err := res.Get()
	if err != nil {
		res.Release()
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("failed to get resource: %v", err)))
		return 2
	}

	sqlDBRes, ok := dbRes.(sqlres.DBResource)
	if !ok {
		res.Release()
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("resource is not a database: %T", dbRes)))
		return 2
	}

	db := NewDB(ctx, res, sqlDBRes.DB, sqlDBRes.Type)

	value.PushUserData(l, db, dbMetatable)
	l.Push(lua.LNil)
	return 2
}

func dbType(l *lua.LState) int {
	db := checkDB(l, 1)
	if db == nil {
		return 0
	}
	mappedType := mapDBTypeFromResourceKind(db.dbType)
	l.Push(lua.LString(mappedType))
	l.Push(lua.LNil)
	return 2
}

func dbQuery(l *lua.LState) int {
	db := checkDB(l, 1)
	if db == nil {
		return 0
	}
	query := l.CheckString(2)
	params, err := checkParams(l, 3)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	yield := AcquireQueryYield()
	yield.DB = db.db
	yield.Query = query
	yield.Params = params
	l.Push(yield)
	return -1
}

func dbExecute(l *lua.LState) int {
	db := checkDB(l, 1)
	if db == nil {
		return 0
	}
	query := l.CheckString(2)
	params, err := checkParams(l, 3)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	yield := AcquireExecuteYield()
	yield.DB = db.db
	yield.Query = query
	yield.Params = params
	l.Push(yield)
	return -1
}

func dbPrepare(l *lua.LState) int {
	db := checkDB(l, 1)
	if db == nil {
		return 0
	}
	ctx := l.Context()
	query := l.CheckString(2)

	yield := AcquirePrepareYield()
	yield.DB = db.db
	yield.Query = query
	yield.WrapStmt = func(stmt *sql.Stmt) lua.LValue {
		return NewStatementUserData(l, NewStatement(ctx, stmt, db))
	}

	l.Push(yield)
	return -1
}

func dbBegin(l *lua.LState) int {
	db := checkDB(l, 1)
	if db == nil {
		return 0
	}
	ctx := l.Context()
	var txOptions *sql.TxOptions

	if l.GetTop() >= 2 && l.Get(2).Type() == lua.LTTable {
		optsTable := l.CheckTable(2)
		txOptions = &sql.TxOptions{}

		if readOnlyVal := optsTable.RawGet(lua.LString("read_only")); readOnlyVal != lua.LNil {
			if readOnlyBool, ok := readOnlyVal.(lua.LBool); ok {
				txOptions.ReadOnly = bool(readOnlyBool)
			}
		}

		if isolationVal := optsTable.RawGet(lua.LString("isolation")); isolationVal != lua.LNil {
			if isolationStr, ok := isolationVal.(lua.LString); ok {
				switch string(isolationStr) {
				case IsolationDefault:
					txOptions.Isolation = sql.LevelDefault
				case IsolationReadUncommitted:
					txOptions.Isolation = sql.LevelReadUncommitted
				case IsolationReadCommitted:
					txOptions.Isolation = sql.LevelReadCommitted
				case IsolationWriteCommitted:
					txOptions.Isolation = sql.LevelWriteCommitted
				case IsolationRepeatableRead:
					txOptions.Isolation = sql.LevelRepeatableRead
				case IsolationSerializable:
					txOptions.Isolation = sql.LevelSerializable
				default:
					l.Push(lua.LNil)
					l.Push(lua.LString(fmt.Sprintf("invalid isolation level: %s", string(isolationStr))))
					return 2
				}
			}
		}
	}

	yield := AcquireBeginYield()
	yield.DB = db.db
	yield.Options = txOptions
	yield.WrapTx = func(tx *sql.Tx) lua.LValue {
		return NewTransactionUserData(l, NewTransaction(ctx, tx, db))
	}

	l.Push(yield)
	return -1
}

func dbRelease(l *lua.LState) int {
	db := checkDB(l, 1)
	if db == nil {
		return 0
	}
	if !db.released && db.resource != nil {
		db.resource.Release()
		db.resource = nil
		db.released = true
		if db.cancelCleanup != nil {
			db.cancelCleanup()
			db.cancelCleanup = nil
		}
	}

	l.Push(lua.LTrue)
	l.Push(lua.LNil)
	return 2
}

func dbStats(l *lua.LState) int {
	db := checkDB(l, 1)
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

func dbToString(l *lua.LState) int {
	db := checkDB(l, 1)
	if db == nil {
		return 0
	}
	l.Push(lua.LString(fmt.Sprintf("sql.DB{type=%s}", db.dbType)))
	return 1
}
