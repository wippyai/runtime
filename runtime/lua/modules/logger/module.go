package logger

import (
	"sync"

	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

var (
	defaultLogger *zap.Logger
	loggerMod     *LoggerModule
	moduleTable   lua.LValue
	initOnce      sync.Once
)

// Module is the singleton logger module instance.
var Module = &loggerModule{}

type loggerModule struct{}

func (m *loggerModule) Info() luaapi.ModuleInfo {
	return luaapi.ModuleInfo{
		Name:        "logger",
		Description: "Structured logging with levels",
		Class:       []string{luaapi.ClassIO, luaapi.ClassNondeterministic},
	}
}

func (m *loggerModule) Register(l *lua.LState) *luaapi.Registration {
	initOnce.Do(func() {
		if defaultLogger == nil {
			defaultLogger = zap.NewNop()
		}
		loggerMod = NewLoggerModule(defaultLogger)
	})

	loggerMod.Loader(l)
	moduleTable = l.Get(-1)
	l.Pop(1)

	return &luaapi.Registration{
		Table:      moduleTable.(*lua.LTable),
		YieldTypes: nil,
	}
}

// SetLogger sets the base logger for the module.
func SetLogger(l *zap.Logger) {
	defaultLogger = l
	loggerMod = NewLoggerModule(l)
}

func (m *loggerModule) Loader(l *lua.LState) int {
	reg := m.Register(l)
	l.Push(reg.Table)
	return 1
}

// Bind is deprecated. Use luaapi.LoadModule(l, Module) instead.
func Bind(l *lua.LState) {
	luaapi.LoadModule(l, Module)
}
