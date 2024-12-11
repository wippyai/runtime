package sql

import "github.com/ponyruntime/go-lua"

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
		"tasks":            execute,
		"query":            query,
		"prepare":          prepare,
		"executePrepared":  executePrepared,
		"beginTransaction": beginTransaction,
		"commit":           commit,
		"rollback":         rollback,
		"close":            closeDB,
	}))

	// Push the SQL module table
	l.Push(t)

	return 1
}
