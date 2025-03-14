package logger

import (
	"errors"

	"github.com/ponyruntime/pony/runtime/lua/engine/value"

	transcoder "github.com/ponyruntime/pony/system/payload/lua"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

// todo: can it work via dot notation?

// Module represents a logger Lua module
type Module struct {
	baseLogger *zap.Logger
}

// Logger represents a Lua userdata object wrapping zap.Logger
type Logger struct {
	logger *zap.Logger
}

// NewLoggerModule creates a new logger module
func NewLoggerModule(log *zap.Logger) *Module {
	return &Module{
		baseLogger: log,
	}
}

// Name returns the module name
func (m *Module) Name() string {
	return "logger"
}

// Loader is the entry point for loading the plugin
func (m *Module) Loader(l *lua.LState) int {
	// Register all methods at once using the efficient method from util.go
	methods := map[string]lua.LGFunction{
		"debug": loggerDebug,
		"info":  loggerInfo,
		"warn":  loggerWarn,
		"error": loggerError,
		"with":  loggerWith,
		"named": loggerNamed,
	}

	// Base logger is our module entry
	ud := l.NewUserData()
	ud.Value = &Logger{logger: m.baseLogger}
	ud.Metatable = value.RegisterMethods(l, "logger.Logger", methods)

	l.Push(ud)
	return 1
}

// Helper function to convert Lua table to zap fields
func tableToFields(table *lua.LTable) []zap.Field {
	fields := make([]zap.Field, 0)
	table.ForEach(func(k, v lua.LValue) {
		key, ok := k.(lua.LString)
		if !ok {
			return
		}

		switch v.Type() {
		case lua.LTString:
			fields = append(fields, zap.String(string(key), string(v.(lua.LString))))
		case lua.LTNumber:
			fields = append(fields, zap.Float64(string(key), float64(v.(lua.LNumber))))
		case lua.LTBool:
			fields = append(fields, zap.Bool(string(key), bool(v.(lua.LBool))))
		case lua.LTNil, lua.LTFunction, lua.LTUserData, lua.LTThread, lua.LTTable, lua.LTChannel:
			fallthrough
		default:
			fields = append(fields, zap.Any(string(key), transcoder.ToGoAny(v)))
		}
	})

	return fields
}

// Logger methods implementations
func loggerDebug(l *lua.LState) int {
	ud := l.CheckUserData(1)
	logger, ok := ud.Value.(*Logger)
	if !ok {
		l.ArgError(1, "logger expected")
		return 0
	}

	msg := l.CheckString(2)
	var fields []zap.Field

	if l.GetTop() > 2 {
		if tbl := l.CheckTable(3); tbl != nil {
			fields = tableToFields(tbl)
		}
	}

	logger.logger.Debug(msg, fields...)
	return 0
}

func loggerInfo(l *lua.LState) int {
	ud := l.CheckUserData(1)
	logger, ok := ud.Value.(*Logger)
	if !ok {
		l.ArgError(1, "logger expected")
		return 0
	}

	msg := l.CheckString(2)
	var fields []zap.Field

	if l.GetTop() > 2 {
		if tbl := l.CheckTable(3); tbl != nil {
			fields = tableToFields(tbl)
		}
	}

	logger.logger.Info(msg, fields...)
	return 0
}

func loggerWarn(l *lua.LState) int {
	ud := l.CheckUserData(1)
	logger, ok := ud.Value.(*Logger)
	if !ok {
		l.ArgError(1, "logger expected")
		return 0
	}

	msg := l.CheckString(2)
	var fields []zap.Field

	if l.GetTop() > 2 {
		if tbl := l.CheckTable(3); tbl != nil {
			fields = tableToFields(tbl)
		}
	}

	logger.logger.Warn(msg, fields...)
	return 0
}

func loggerError(l *lua.LState) int {
	ud := l.CheckUserData(1)
	logger, ok := ud.Value.(*Logger)
	if !ok {
		l.ArgError(1, "logger expected")
		return 0
	}

	msg := l.CheckString(2)
	var fields []zap.Field

	if l.GetTop() > 2 {
		if tbl := l.CheckTable(3); tbl != nil {
			// Handle special error field
			if errValue := tbl.RawGetString("error"); errValue != lua.LNil {
				fields = append(fields, zap.Error(errors.New(errValue.String())))
				tbl.RawSetString("error", lua.LNil) // Done error from table
			}

			fields = append(fields, tableToFields(tbl)...)
		}
	}

	logger.logger.Error(msg, fields...)
	return 0
}

func loggerWith(l *lua.LState) int {
	ud := l.CheckUserData(1)
	logger, ok := ud.Value.(*Logger)
	if !ok {
		l.ArgError(1, "logger expected")
		return 0
	}

	fields := l.CheckTable(2)
	if fields == nil {
		l.ArgError(2, "table expected")
		return 0
	}

	// Spawn new logger with fields
	newLogger := logger.logger.With(tableToFields(fields)...)

	// Spawn new userdata
	newUd := l.NewUserData()
	newUd.Value = &Logger{logger: newLogger}
	newUd.Metatable = value.GetTypeMetatable(l, "logger.Logger")

	l.Push(newUd)

	return 1
}

func loggerNamed(l *lua.LState) int {
	ud := l.CheckUserData(1)
	logger, ok := ud.Value.(*Logger)
	if !ok {
		l.ArgError(1, "logger expected")
		return 0
	}

	name := l.CheckString(2)
	if name == "" {
		l.ArgError(2, "name cannot be empty")
		return 0
	}

	// Spawn new named logger
	newLogger := logger.logger.Named(name)

	// Spawn new userdata
	newUd := l.NewUserData()
	newUd.Value = &Logger{logger: newLogger}
	newUd.Metatable = value.GetTypeMetatable(l, "logger.Logger")
	l.Push(newUd)

	return 1
}
