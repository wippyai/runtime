package sql

import "github.com/yuin/gopher-lua"

func (m *Module) Loader(l *lua.LState) int {
	// Create the SQL module table
	t := l.NewTable()

	// generate the Connect function
	l.SetFuncs(t, map[string]lua.LGFunction{
		"connect": m.Connect,
	})

	// generate the DB methods as a metatable
	dbMt := l.NewTypeMetatable("DB")
	l.SetField(dbMt, "__index", l.SetFuncs(l.NewTable(), map[string]lua.LGFunction{
		"execute":           execute,
		"query":             query,
		"prepare":           prepare,
		"execute_prepared":  executePrepared,
		"begin_transaction": beginTransaction,
		"commit":            commit,
		"rollback":          rollback,
		"close":             closeDB,
	}))

	// Push the SQL module table
	l.Push(t)

	return 1
}
