package sqlutil

import (
	lua "github.com/yuin/gopher-lua"
)

// TypedValue represents a value with an explicit SQL type
type TypedValue struct {
	Type  string
	Value interface{}
}

// RegisterAsModule registers the 'as' submodule in the SQL module
func RegisterAsModule(l *lua.LState, mod *lua.LTable) {
	// Create the 'as' submodule table
	as := l.CreateTable(0, 6) // Initial capacity for functions

	// Register conversion functions
	as.RawSetString("int", l.NewFunction(asInt))
	as.RawSetString("binary", l.NewFunction(asBinary))
	as.RawSetString("float", l.NewFunction(asFloat))
	as.RawSetString("text", l.NewFunction(asText))
	as.RawSetString("null", l.NewFunction(asNull))

	// Can add more types as needed

	// Add the submodule to the main module
	mod.RawSetString("as", as)
}

// asInt converts a Lua value to SQL INTEGER type
func asInt(l *lua.LState) int {
	value := l.Get(1)

	var intValue int64
	switch value.Type() {
	case lua.LTNumber:
		intValue = int64(value.(lua.LNumber))
	case lua.LTString:
		// Could add string-to-int conversion here if needed
		intValue = 0 // Default for now
	case lua.LTNil, lua.LTBool, lua.LTFunction, lua.LTUserData, lua.LTThread, lua.LTTable, lua.LTChannel:
		// FIXME rework on demand
		fallthrough
	default:
		intValue = 0
	}

	// Create a userdata with the typed value
	ud := l.NewUserData()
	ud.Value = &TypedValue{
		Type:  "int",
		Value: intValue,
	}

	l.Push(ud)
	return 1
}

// asBinary converts a Lua value to SQL BINARY/BLOB type
func asBinary(l *lua.LState) int {
	value := l.Get(1)

	var binaryValue []byte
	switch value.Type() {
	case lua.LTString:
		binaryValue = []byte(string(value.(lua.LString)))
	case lua.LTNil, lua.LTBool, lua.LTNumber, lua.LTFunction, lua.LTUserData, lua.LTThread, lua.LTTable, lua.LTChannel:
		// FIXME rework on demand
		fallthrough
	default:
		binaryValue = nil
	}

	ud := l.NewUserData()
	ud.Value = &TypedValue{
		Type:  "binary",
		Value: binaryValue,
	}

	l.Push(ud)
	return 1
}

// asFloat converts a Lua value to SQL FLOAT/REAL type
func asFloat(l *lua.LState) int {
	value := l.Get(1)

	var floatValue float64
	switch value.Type() {
	case lua.LTNumber:
		floatValue = float64(value.(lua.LNumber))
	case lua.LTNil, lua.LTBool, lua.LTString, lua.LTFunction, lua.LTUserData, lua.LTThread, lua.LTTable, lua.LTChannel:
		// FIXME rework on demand
		fallthrough
	default:
		floatValue = 0.0
	}

	ud := l.NewUserData()
	ud.Value = &TypedValue{
		Type:  "float",
		Value: floatValue,
	}

	l.Push(ud)
	return 1
}

// asText converts a Lua value to SQL TEXT type
func asText(l *lua.LState) int {
	value := l.Get(1)

	var textValue string
	switch value.Type() {
	case lua.LTString:
		textValue = string(value.(lua.LString))
	case lua.LTNumber:
		textValue = value.String()
	case lua.LTNil, lua.LTBool, lua.LTFunction, lua.LTUserData, lua.LTThread, lua.LTTable, lua.LTChannel:
		// FIXME rework on demand
		fallthrough
	default:
		textValue = ""
	}

	ud := l.NewUserData()
	ud.Value = &TypedValue{
		Type:  "text",
		Value: textValue,
	}

	l.Push(ud)
	return 1
}

// asNull explicitly returns a SQL NULL value
func asNull(l *lua.LState) int {
	// Create a userdata with the NULL marker
	ud := l.NewUserData()
	ud.Value = "SQL_NULL" // Use the same marker as sql.NULL for consistency

	l.Push(ud)
	return 1
}
