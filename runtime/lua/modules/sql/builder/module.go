package builder

import (
	"github.com/Masterminds/squirrel"
	lua "github.com/yuin/gopher-lua"
)

// RegisterBuilderModule adds the SQL builder submodule to the main SQL module
func RegisterBuilderModule(l *lua.LState, mod *lua.LTable) {
	// Register all builder types
	registerSelectBuilderType(l)
	registerInsertBuilderType(l)
	registerUpdateBuilderType(l)
	registerDeleteBuilderType(l)
	registerCaseBuilderType(l)

	// Register the query executor
	RegisterQueryExecutorMetatable(l)

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

	colonFormat := l.NewUserData()
	colonFormat.Value = squirrel.Colon
	colonFormat.Metatable = getPlaceholderMetatable(l, "Colon")
	mod.RawSetString("colon", colonFormat)

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
