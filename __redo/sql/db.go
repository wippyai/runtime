package sql

import (
	"database/sql"

	"go.uber.org/zap"

	"github.com/yuin/gopher-lua"
	// SQLite3 driver
	_ "github.com/mattn/go-sqlite3"
)

// DB represents a database connection with methods for operations.
type DB struct {
	conn        *sql.DB
	transaction *sql.Tx
	prepstmap   map[string]*sql.Stmt
	log         *zap.Logger
}

// NewDB creates a new DB instance.
func NewDB(dbType, connStr string, logger *zap.Logger) (*DB, error) {
	db, err := sql.Open(dbType, connStr)
	if err != nil {
		logger.Error("Failed to open database", zap.Error(err))
		return nil, err
	}

	// Verify the connection
	if err := db.Ping(); err != nil {
		logger.Error("Failed to ping database", zap.Error(err))
		return nil, err
	}

	return &DB{
		conn:      db,
		prepstmap: make(map[string]*sql.Stmt),
		log:       logger,
	}, nil
}

func (db *DB) wrapResult(l *lua.LState, result sql.Result) (*lua.LTable, error) {
	rsp := l.NewTable()

	ra, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}

	rsp.RawSetString("rows_affected", lua.LNumber(ra))

	ri, err := result.LastInsertId()
	if err == nil {
		rsp.RawSetString("last_insert_id", lua.LNumber(ri))
	}

	return rsp, nil
}

// WrapDB wraps the DB instance as Lua userdata.
func WrapDB(l *lua.LState, db *DB) *lua.LUserData {
	ud := l.NewUserData()
	ud.Value = db
	l.SetMetatable(ud, l.GetTypeMetatable("DB"))

	return ud
}
