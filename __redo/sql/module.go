package sql

import (
	"github.com/yuin/gopher-lua"

	// PQ Driver
	_ "github.com/lib/pq"
	// SQLite3 driver
	_ "github.com/mattn/go-sqlite3"

	"go.uber.org/zap"
)

type Module struct {
	log *zap.Logger
}

func NewModule(log *zap.Logger) *Module {
	return &Module{log: log}
}

func (m *Module) Connect(l *lua.LState) int {
	m.log.Debug("calling sql.connect")

	// Expecting 1 argument: connection string
	numArgs := l.GetTop()
	if numArgs != 2 {
		errMsg := "expected 2 arguments: db type, connection string"
		m.log.Error(errMsg)
		l.Push(lua.LNil)
		l.Push(lua.LString(errMsg))
		return 2
	}

	// 1st argument: db type
	// 2nd argument: connection string
	dbType := l.CheckString(1)
	connStr := l.CheckString(2)

	// Create a new DB instance
	customDB, err := NewDB(dbType, connStr, m.log.Named("DB"))
	if err != nil {
		m.log.Error("Failed to create DB instance", zap.Error(err))
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	// Wrap *DB as Lua userdata
	ud := WrapDB(l, customDB)
	l.Push(ud)

	return 1
}

// close wraps the DB.close method.
func closeDB(l *lua.LState) int {
	ud := l.CheckUserData(1)
	if ud == nil {
		l.ArgError(1, "expected userdata for DB")
		return 0
	}

	db, ok := ud.Value.(*DB)
	if !ok {
		l.ArgError(1, "invalid userdata type for DB")
		return 0
	}

	// remove self from args
	l.Remove(1)

	return db.close(l)
}
