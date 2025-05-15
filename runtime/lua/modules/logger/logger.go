package logger

import (
	"errors"
	"github.com/ponyruntime/pony/api/logs"
	"github.com/ponyruntime/pony/runtime/lua/engine/value"
	transcoder "github.com/ponyruntime/pony/system/payload/lua"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

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
	// Create UserData without initializing the logger - we'll use context logger
	ud := l.NewUserData()
	ud.Value = &Logger{logger: nil}
	ud.Metatable = value.RegisterMethods(l, "logger.Logger", methods)
	l.Push(ud)
	return 1
}

// Helper function to get logger either from UserData or context
// NEVER returns nil - always gets a valid logger or panics if truly impossible
func getEffectiveLogger(l *lua.LState) *zap.Logger {
	// First try to get from UserData
	if l.GetTop() >= 1 {
		if ud := l.Get(1); ud.Type() == lua.LTUserData {
			if userdata, ok := ud.(*lua.LUserData); ok {
				if logger, ok := userdata.Value.(*Logger); ok && logger != nil && logger.logger != nil {
					return logger.logger
				}
			}
		}
	}

	// If we get here, use context logger
	return logs.GetLogger(l.Context())
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
	// Get the effective logger - either from UserData or context
	zapLogger := getEffectiveLogger(l)

	// Skip if no logger (shouldn't happen with proper fallbacks)
	if zapLogger == nil {
		return 0
	}

	msg := l.CheckString(2)
	var fields []zap.Field
	if l.GetTop() > 2 {
		if tbl := l.CheckTable(3); tbl != nil {
			fields = tableToFields(tbl)
		}
	}

	zapLogger.Debug(msg, fields...)
	return 0
}

func loggerInfo(l *lua.LState) int {
	// Get the effective logger - either from UserData or context
	zapLogger := getEffectiveLogger(l)

	// Skip if no logger (shouldn't happen with proper fallbacks)
	if zapLogger == nil {
		return 0
	}

	msg := l.CheckString(2)
	var fields []zap.Field
	if l.GetTop() > 2 {
		if tbl := l.CheckTable(3); tbl != nil {
			fields = tableToFields(tbl)
		}
	}

	zapLogger.Info(msg, fields...)
	return 0
}

func loggerWarn(l *lua.LState) int {
	// Get the effective logger - either from UserData or context
	zapLogger := getEffectiveLogger(l)

	// Skip if no logger (shouldn't happen with proper fallbacks)
	if zapLogger == nil {
		return 0
	}

	msg := l.CheckString(2)
	var fields []zap.Field
	if l.GetTop() > 2 {
		if tbl := l.CheckTable(3); tbl != nil {
			fields = tableToFields(tbl)
		}
	}

	zapLogger.Warn(msg, fields...)
	return 0
}

func loggerError(l *lua.LState) int {
	// Get the effective logger - either from UserData or context
	zapLogger := getEffectiveLogger(l)

	// Skip if no logger (shouldn't happen with proper fallbacks)
	if zapLogger == nil {
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

	zapLogger.Error(msg, fields...)
	return 0
}

func loggerWith(l *lua.LState) int {
	// Get the effective logger - either from UserData or context
	zapLogger := getEffectiveLogger(l)

	// Skip if no logger (shouldn't happen with proper fallbacks)
	if zapLogger == nil {
		return 0
	}

	fields := l.CheckTable(2)
	if fields == nil {
		l.ArgError(2, "table expected")
		return 0
	}

	// Create new logger with fields
	newLogger := zapLogger.With(tableToFields(fields)...)

	// Spawn new userdata
	newUd := l.NewUserData()
	newUd.Value = &Logger{logger: newLogger}
	newUd.Metatable = value.GetTypeMetatable(l, "logger.Logger")
	l.Push(newUd)
	return 1
}

func loggerNamed(l *lua.LState) int {
	// Get the effective logger - either from UserData or context
	zapLogger := getEffectiveLogger(l)

	// Skip if no logger (shouldn't happen with proper fallbacks)
	if zapLogger == nil {
		return 0
	}

	name := l.CheckString(2)
	if name == "" {
		l.ArgError(2, "name cannot be empty")
		return 0
	}

	// Create new named logger
	newLogger := zapLogger.Named(name)

	// Spawn new userdata
	newUd := l.NewUserData()
	newUd.Value = &Logger{logger: newLogger}
	newUd.Metatable = value.GetTypeMetatable(l, "logger.Logger")
	l.Push(newUd)
	return 1
}
