package builder

import (
	"fmt"
	"github.com/Masterminds/squirrel"
	"github.com/ponyruntime/pony/runtime/lua/modules/sql"
	lua "github.com/yuin/gopher-lua"
)

// RegisterBuilderModule adds the SQL builder submodule to the main SQL module
func RegisterBuilderModule(l *lua.LState, mod *lua.LTable) {
	// Register Row metatable
	RegisterRowMetatable(l)

	// Create builder submodule table
	builder := l.CreateTable(0, 20) // Reserve space for all functions

	// Register main constructor functions
	builder.RawSetString("select", l.NewFunction(builderSelect))
	builder.RawSetString("insert", l.NewFunction(builderInsert))
	builder.RawSetString("update", l.NewFunction(builderUpdate))
	builder.RawSetString("delete", l.NewFunction(builderDelete))
	builder.RawSetString("case", l.NewFunction(builderCase))

	// Register placeholder formats as userdata
	registerPlaceholderFormats(l, builder)

	// Register expression builders (eq, not_eq, etc.)
	registerExpressionBuilders(l, builder)

	// Add the submodule to the main module
	mod.RawSetString("builder", builder)
}

// registerPlaceholderFormats adds the placeholder format constants to the builder module
func registerPlaceholderFormats(l *lua.LState, mod *lua.LTable) {
	// Create userdata for each placeholder format
	questionFormat := l.NewUserData()
	questionFormat.Value = squirrel.Question
	questionFormat.Metatable = getPlaceholderMetatable(l, "Question")
	mod.RawSetString("question", questionFormat)

	dollarFormat := l.NewUserData()
	dollarFormat.Value = squirrel.Dollar
	dollarFormat.Metatable = getPlaceholderMetatable(l, "Dollar")
	mod.RawSetString("dollar", dollarFormat)

	atFormat := l.NewUserData()
	atFormat.Value = squirrel.AtP
	atFormat.Metatable = getPlaceholderMetatable(l, "At")
	mod.RawSetString("at", atFormat)

	namedFormat := l.NewUserData()
	namedFormat.Value = squirrel.Named
	namedFormat.Metatable = getPlaceholderMetatable(l, "Named")
	mod.RawSetString("named", namedFormat)

	// Set the default placeholder format to use (question by default)
	mod.RawSetString("default_placeholder", questionFormat)
}

// getPlaceholderMetatable returns a metatable for placeholder formats
func getPlaceholderMetatable(l *lua.LState, name string) *lua.LTable {
	mt := l.CreateTable(0, 1)
	mt.RawSetString("__tostring", l.NewFunction(func(l *lua.LState) int {
		l.Push(lua.LString("Placeholder Format: " + name))
		return 1
	}))
	return mt
}

// Common runner types from your existing module
// Add these constants to make it easier to integrate with your module
const (
	RunnerNotSet         = "cannot run; no Runner set (run_with)"
	RunnerNotQueryRunner = "cannot query row; Runner is not a QueryRower"
)

// checkBuilder is a generic type checker for builder objects
// This utility function helps reduce repetitive code
func checkBuilder(l *lua.LState, index int, expected string) interface{} {
	ud := l.CheckUserData(index)
	if ud.Metatable == nil {
		l.ArgError(index, "expected "+expected+" object")
		return nil
	}

	return ud.Value
}

// getBaseRunner extracts a Squirrel BaseRunner from DB/Transaction objects
// Used by run_with methods to integrate with your module
func getBaseRunner(l *lua.LState, value interface{}) (squirrel.BaseRunner, error) {
	switch v := value.(type) {
	case *sql.DB:
		// Use the db field from your existing DB type
		return v.db, nil
	case *sql.Transaction:
		// Use the tx field from your existing Transaction type
		return v.tx, nil
	default:
		return nil, fmt.Errorf("expected database or transaction object, got %T", value)
	}
}
