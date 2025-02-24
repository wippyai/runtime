package execprocess

import (
	"github.com/ponyruntime/pony/internal/codeexec/native"
	lua "github.com/yuin/gopher-lua"
)

const (
	metatableName = "Process"
)

type Module struct {
	once *onceStream
}

func NewModule() *Module {
	return &Module{
		once: newOnceStream(),
	}
}

func getProcessExecutor(l *lua.LState) *native.Executor {
	ud := l.CheckUserData(1)
	if ud == nil {
		return nil
	}

	switch tt := ud.Value.(type) {
	case *native.Executor:
		return tt
	default:
		return nil
	}
}
