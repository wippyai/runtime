package datacore_test

import (
	"context"
	"testing"

	"github.com/ponyruntime/go-lua"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

type tmodule struct {
	log *zap.Logger
}

func newTestModule(log *zap.Logger) *tmodule {
	return &tmodule{
		log: log,
	}
}

func (m *tmodule) Loader(l *lua.LState) int {
	t := l.NewTable()

	lapi := map[string]lua.LGFunction{
		"test": m.test,
	}

	l.SetFuncs(t, lapi)
	l.Push(t)

	return 1
}

func (m *tmodule) test(l *lua.LState) int {
	req := l.Get(-1)
	if req.Type() != lua.LTTable {
		panic("should be a table")
	}

	tbl := req.(*lua.LTable)
	engine.TableToMap(tbl, m.log)

	return 0
}

func TestLuaLNil(t *testing.T) {
	luacode := `
	local tt = require("test")

	local outerTable = {
    innerTable = {
        field1 = "value1",
        field2 = "value2",
        field3 = "value3"
    }
}

tt.test(outterTable)`

	zl, _ := zap.NewDevelopment()
	le := engine.NewLuaEngine(context.Background(), zl)
	ttt := newTestModule(zl)

	le.L.PreloadModule("test", ttt.Loader)
	err := le.DoString(luacode, "<string>")
	assert.NoError(t, err)
}
