package sql

import (
	"testing"

	lua "github.com/yuin/gopher-lua"
)

func TestExecutorToString(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	wrapper := &queryExecutorWrapper{
		db:    nil,
		query: "SELECT * FROM users",
		args:  []any{},
	}

	ud := l.NewUserData()
	ud.Value = wrapper
	l.Push(ud)

	executorToString(l)

	result := l.Get(-1)
	str := string(result.(lua.LString))
	expected := "QueryExecutor: SELECT * FROM users [Args: []]"
	if str != expected {
		t.Errorf("expected %s, got %s", expected, str)
	}
}
