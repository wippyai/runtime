package logger

import (
	"errors"
	"fmt"
	"sync"

	runtimeapi "github.com/wippyai/runtime/api/runtime"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/engine/value"

	lua "github.com/yuin/gopher-lua"
	"github.com/yuin/gopher-lua/inspect"
	"go.uber.org/zap"
)

// todo: can it work via dot notation?

// Module represents a logger Lua module
type LoggerModule struct {
	baseLogger *zap.Logger
}

// Logger represents a Lua userdata object wrapping zap.Logger
type Logger struct {
	logger *zap.Logger
}

// NewLoggerModule creates a new logger module
func NewLoggerModule(log *zap.Logger) *LoggerModule {
	return &LoggerModule{
		baseLogger: log,
	}
}

func (m *LoggerModule) Info() luaapi.ModuleInfo {
	return luaapi.ModuleInfo{
		Name:        "logger",
		Description: "Structured logging",
		Class:       []string{luaapi.ClassIO},
	}
}

// Global logger methods (registered once)
var loggerMethodsOnce sync.Once

func initLoggerMethods() {
	loggerMethodsOnce.Do(func() {
		value.RegisterMethods(nil, "logger.Logger", map[string]lua.LGFunction{
			"debug": loggerDebug,
			"info":  loggerInfo,
			"warn":  loggerWarn,
			"error": loggerError,
			"with":  loggerWith,
			"named": loggerNamed,
		})
	})
}

// Loader is the entry point for loading the plugin
func (m *LoggerModule) Loader(l *lua.LState) int {
	initLoggerMethods()

	// Base logger is our module entry
	ud := l.NewUserData()
	ud.Value = &Logger{logger: m.baseLogger}
	ud.Metatable = value.GetTypeMetatable(nil, "logger.Logger")

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
		case lua.LTUserData:
			// Check if userdata contains a Go error type
			if ud, ok := v.(*lua.LUserData); ok {
				// Try lua.Error first for richer error information
				if luaErr, ok := ud.Value.(*lua.Error); ok {
					fields = append(fields, zap.String(string(key), luaErr.Error()))
				} else if goErr, ok := ud.Value.(error); ok && goErr != nil {
					// Fallback to generic error interface
					fields = append(fields, zap.String(string(key), goErr.Error()))
				} else {
					// For non-error userdata, use __tostring metamethod
					strValue := lua.LVAsString(v)
					if strValue != "" {
						fields = append(fields, zap.String(string(key), strValue))
					}
				}
			}
		case lua.LTNil, lua.LTFunction, lua.LTThread, lua.LTTable, lua.LTChannel:
			fallthrough
		default:
			fields = append(fields, zap.Any(string(key), value.ToGoAny(v)))
		}
	})

	return fields
}

// getContextFields extracts PID and location from the Lua context
func getContextFields(l *lua.LState) []zap.Field {
	fields := make([]zap.Field, 0, 2)

	// Get PID
	if pid, ok := runtimeapi.GetFramePID(l.Context()); ok {
		fields = append(fields, zap.String("pid", pid.String()))
	}

	// Get location = id:line
	if id, ok := runtimeapi.GetFrameID(l.Context()); ok {
		// Stack level 2 is the actual Lua caller (0=getContextFields, 1=loggerXXX, 2=Lua caller)
		if line, ok := inspect.GetCallerLine(l, 2); ok {
			location := fmt.Sprintf("%s:%d", id.String(), line)
			fields = append(fields, zap.String("location", location))
		}
	}

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

	contextFields := getContextFields(l)
	allFields := contextFields
	allFields = append(allFields, fields...)
	logger.logger.Debug(msg, allFields...)
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

	contextFields := getContextFields(l)
	allFields := contextFields
	allFields = append(allFields, fields...)
	logger.logger.Info(msg, allFields...)
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

	contextFields := getContextFields(l)
	allFields := contextFields
	allFields = append(allFields, fields...)
	logger.logger.Warn(msg, allFields...)
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
				// Use LVAsString to properly invoke __tostring metamethod for error userdata
				fields = append(fields, zap.Error(errors.New(lua.LVAsString(errValue))))
				tbl.RawSetString("error", lua.LNil) // Done error from table
			}

			fields = append(fields, tableToFields(tbl)...)
		}
	}

	contextFields := getContextFields(l)
	allFields := contextFields
	allFields = append(allFields, fields...)
	logger.logger.Error(msg, allFields...)
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
