package logger

import (
	"errors"

	"github.com/ponyruntime/pony/runtime/lua/engine/value"
	transcoder "github.com/ponyruntime/pony/system/payload/lua"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

// Module represents a logger Lua module
type Module struct {
	baseLogger *zap.Logger
}

// Op represents a modification to be applied to a logger
type Op struct {
	Type   string      // "with" or "named"
	Fields []zap.Field // for "with" operations
	Name   string      // for "named" operations
}

// Logger represents a Lua userdata object wrapping logger operations
type Logger struct {
	module     *Module // Reference to the module
	operations []Op    // Stack of operations to apply to context logger
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
	// Create UserData with module reference and empty operations list
	ud := l.NewUserData()
	ud.Value = &Logger{
		module:     m,
		operations: make([]Op, 0),
	}
	ud.Metatable = value.RegisterMethods(l, "logger.Logger", methods)
	l.Push(ud)
	return 1
}

// Helper function to apply operations to a logger
func applyOperations(baseLogger *zap.Logger, operations []Op) *zap.Logger {
	logger := baseLogger
	for _, op := range operations {
		switch op.Type {
		case "with":
			logger = logger.With(op.Fields...)
		case "named":
			logger = logger.Named(op.Name)
		}
	}
	return logger
}

// Helper function to get the effective logger
func getEffectiveLogger(l *lua.LState) (*zap.Logger, []Op) {
	// Get operations from UserData if available
	var operations []Op
	var module *Module
	if l.GetTop() >= 1 {
		if ud := l.Get(1); ud.Type() == lua.LTUserData {
			if userdata, ok := ud.(*lua.LUserData); ok {
				if logger, ok := userdata.Value.(*Logger); ok && logger != nil {
					operations = logger.operations
					module = logger.module
				}
			}
		}
	}

	// Use the base logger from the module
	var baseLogger *zap.Logger
	if module != nil {
		baseLogger = module.baseLogger
	}

	return baseLogger, operations
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
	// Get the base logger and operations
	baseLogger, operations := getEffectiveLogger(l)

	// Skip if no base logger
	if baseLogger == nil {
		return 0
	}

	// Apply operations to get the effective logger
	zapLogger := applyOperations(baseLogger, operations)

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
	// Get the base logger and operations
	baseLogger, operations := getEffectiveLogger(l)

	// Skip if no base logger
	if baseLogger == nil {
		return 0
	}

	// Apply operations to get the effective logger
	zapLogger := applyOperations(baseLogger, operations)

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
	// Get the base logger and operations
	baseLogger, operations := getEffectiveLogger(l)

	// Skip if no base logger
	if baseLogger == nil {
		return 0
	}

	// Apply operations to get the effective logger
	zapLogger := applyOperations(baseLogger, operations)

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
	// Get the base logger and operations
	baseLogger, operations := getEffectiveLogger(l)

	// Skip if no base logger
	if baseLogger == nil {
		return 0
	}

	// Apply operations to get the effective logger
	zapLogger := applyOperations(baseLogger, operations)

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
	// Get current operations from UserData
	var currentOps []Op
	var module *Module
	if l.GetTop() >= 1 {
		if ud := l.Get(1); ud.Type() == lua.LTUserData {
			if userdata, ok := ud.(*lua.LUserData); ok {
				if logger, ok := userdata.Value.(*Logger); ok && logger != nil {
					currentOps = logger.operations
					module = logger.module
				}
			}
		}
	}

	fields := l.CheckTable(2)
	if fields == nil {
		l.ArgError(2, "table expected")
		return 0
	}

	// Create new "with" operation
	withOp := Op{
		Type:   "with",
		Fields: tableToFields(fields),
	}

	// Create new operations list with the added "with"
	newOps := make([]Op, len(currentOps)+1)
	copy(newOps, currentOps)
	newOps[len(currentOps)] = withOp

	// Spawn new userdata with the new operations
	newUd := l.NewUserData()
	newUd.Value = &Logger{
		module:     module,
		operations: newOps,
	}
	newUd.Metatable = value.GetTypeMetatable(l, "logger.Logger")
	l.Push(newUd)
	return 1
}

func loggerNamed(l *lua.LState) int {
	// Get current operations from UserData
	var currentOps []Op
	var module *Module
	if l.GetTop() >= 1 {
		if ud := l.Get(1); ud.Type() == lua.LTUserData {
			if userdata, ok := ud.(*lua.LUserData); ok {
				if logger, ok := userdata.Value.(*Logger); ok && logger != nil {
					currentOps = logger.operations
					module = logger.module
				}
			}
		}
	}

	name := l.CheckString(2)
	if name == "" {
		l.ArgError(2, "name cannot be empty")
		return 0
	}

	// Create new "named" operation
	namedOp := Op{
		Type: "named",
		Name: name,
	}

	// Create new operations list with the added "named"
	newOps := make([]Op, len(currentOps)+1)
	copy(newOps, currentOps)
	newOps[len(currentOps)] = namedOp

	// Spawn new userdata with the new operations
	newUd := l.NewUserData()
	newUd.Value = &Logger{
		module:     module,
		operations: newOps,
	}
	newUd.Metatable = value.GetTypeMetatable(l, "logger.Logger")
	l.Push(newUd)
	return 1
}
