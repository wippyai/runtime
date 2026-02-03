package logger

import (
	"errors"
	"fmt"

	"github.com/wippyai/runtime/api/logs"
	runtimeapi "github.com/wippyai/runtime/api/runtime"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/engine/value"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/go-lua/inspect"
	"go.uber.org/zap"
)

const typeLoggerName = "logger.Logger"

var loggerMetatable *lua.LTable

func init() {
	loggerMetatable = value.RegisterTypeMethods(nil, typeLoggerName, nil, map[string]lua.LGoFunc{
		"debug": loggerDebug,
		"info":  loggerInfo,
		"warn":  loggerWarn,
		"error": loggerError,
		"with":  loggerWith,
		"named": loggerNamed,
	})
}

// Logger wraps zap.Logger for Lua.
// If logger is nil, it gets the logger from context at call time.
type Logger struct {
	logger *zap.Logger
}

// Module is the logger ModuleDef.
var Module = &luaapi.ModuleDef{
	Name:        "logger",
	Description: "Structured logging",
	Class:       []string{luaapi.ClassIO},
	BuildValue:  buildModule,
	Types:       ModuleTypes,
}

func buildModule() (lua.LValue, []luaapi.YieldType) {
	// Return shared userdata for V1 API compatibility
	// V1: local log = require("logger"); log:info("msg")
	// Logger.logger is nil - will be fetched from context at call time
	ud := &lua.LUserData{
		Value:     &Logger{},
		Metatable: loggerMetatable,
	}
	return ud, nil
}

// getLogger returns the zap logger, either from the Logger struct or from context
func (lg *Logger) getLogger(l *lua.LState) *zap.Logger {
	if lg.logger != nil {
		return lg.logger
	}
	// Get logger from Lua context
	ctx := l.Context()
	if ctx != nil {
		return logs.GetLogger(ctx)
	}
	return zap.NewNop()
}

// Instance methods for Logger userdata

func checkLogger(l *lua.LState, _ int) *Logger {
	ud := l.CheckUserData(1)
	if ud == nil {
		l.ArgError(1, "expected logger.Logger")
		return nil
	}
	if logger, ok := ud.Value.(*Logger); ok {
		return logger
	}
	l.ArgError(1, "expected logger.Logger")
	return nil
}

func loggerDebug(l *lua.LState) int {
	logger := checkLogger(l, 1)
	if logger == nil {
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
	logger.getLogger(l).Debug(msg, allFields...)
	return 0
}

func loggerInfo(l *lua.LState) int {
	logger := checkLogger(l, 1)
	if logger == nil {
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
	logger.getLogger(l).Info(msg, allFields...)
	return 0
}

func loggerWarn(l *lua.LState) int {
	logger := checkLogger(l, 1)
	if logger == nil {
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
	logger.getLogger(l).Warn(msg, allFields...)
	return 0
}

func loggerError(l *lua.LState) int {
	logger := checkLogger(l, 1)
	if logger == nil {
		return 0
	}

	msg := l.CheckString(2)
	var fields []zap.Field

	if l.GetTop() > 2 {
		if tbl := l.CheckTable(3); tbl != nil {
			if errValue := tbl.RawGetString("error"); errValue != lua.LNil {
				fields = append(fields, zap.Error(errors.New(lua.LVAsString(errValue))))
				tbl.RawSetString("error", lua.LNil)
			}
			fields = append(fields, tableToFields(tbl)...)
		}
	}

	contextFields := getContextFields(l)
	allFields := contextFields
	allFields = append(allFields, fields...)
	logger.getLogger(l).Error(msg, allFields...)
	return 0
}

func loggerWith(l *lua.LState) int {
	logger := checkLogger(l, 1)
	if logger == nil {
		return 0
	}

	fields := l.CheckTable(2)
	if fields == nil {
		l.ArgError(2, "table expected")
		return 0
	}

	newLogger := logger.getLogger(l).With(tableToFields(fields)...)
	value.PushTypedUserData(l, &Logger{logger: newLogger}, typeLoggerName)
	return 1
}

func loggerNamed(l *lua.LState) int {
	logger := checkLogger(l, 1)
	if logger == nil {
		return 0
	}

	name := l.CheckString(2)
	if name == "" {
		l.ArgError(2, "name cannot be empty")
		return 0
	}

	newLogger := logger.getLogger(l).Named(name)
	value.PushTypedUserData(l, &Logger{logger: newLogger}, typeLoggerName)
	return 1
}

// tableToFields converts Lua table to zap fields.
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
		case lua.LTInteger:
			fields = append(fields, zap.Int64(string(key), int64(v.(lua.LInteger))))
		case lua.LTBool:
			fields = append(fields, zap.Bool(string(key), bool(v.(lua.LBool))))
		case lua.LTUserData:
			if ud, ok := v.(*lua.LUserData); ok {
				if err, ok := ud.Value.(error); ok && err != nil {
					var luaErr *lua.Error
					if errors.As(err, &luaErr) {
						fields = append(fields, zap.String(string(key), luaErr.Error()))
					} else {
						fields = append(fields, zap.String(string(key), err.Error()))
					}
				} else {
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

// getContextFields extracts PID and location from the Lua context.
func getContextFields(l *lua.LState) []zap.Field {
	ctx := l.Context()
	if ctx == nil {
		return nil
	}

	fields := make([]zap.Field, 0, 2)

	if pid, ok := runtimeapi.GetFramePID(ctx); ok {
		fields = append(fields, zap.String("pid", pid.String()))
	}

	if id, ok := runtimeapi.GetFrameID(ctx); ok {
		if line, ok := inspect.GetCallerLine(l, 2); ok {
			location := fmt.Sprintf("%s:%d", id.String(), line)
			fields = append(fields, zap.String("location", location))
		}
	}

	return fields
}
