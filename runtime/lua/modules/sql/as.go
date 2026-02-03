package sql

import (
	lua "github.com/wippyai/go-lua"
)

type TypedValue struct {
	Value any
	Type  string
}

func registerAsSubmodule(mod *lua.LTable) {
	as := lua.CreateTable(0, 5)

	as.RawSetString("int", lua.LGoFunc(asInt))
	as.RawSetString("float", lua.LGoFunc(asFloat))
	as.RawSetString("text", lua.LGoFunc(asText))
	as.RawSetString("binary", lua.LGoFunc(asBinary))
	as.RawSetString("null", lua.LGoFunc(asNull))

	as.Immutable = true
	mod.RawSetString("as", as)
}

func asInt(l *lua.LState) int {
	value := l.Get(1)

	var intValue int64
	switch v := value.(type) {
	case lua.LNumber:
		intValue = int64(v)
	case lua.LInteger:
		intValue = int64(v)
	default:
		intValue = 0
	}

	ud := l.NewUserData()
	ud.Value = &TypedValue{
		Type:  "int",
		Value: intValue,
	}

	l.Push(ud)
	return 1
}

func asFloat(l *lua.LState) int {
	value := l.Get(1)

	var floatValue float64
	switch v := value.(type) {
	case lua.LNumber:
		floatValue = float64(v)
	case lua.LInteger:
		floatValue = float64(v)
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

func asText(l *lua.LState) int {
	value := l.Get(1)

	var textValue string
	switch v := value.(type) {
	case lua.LString:
		textValue = string(v)
	case lua.LNumber:
		textValue = v.String()
	case lua.LInteger:
		textValue = v.String()
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

func asBinary(l *lua.LState) int {
	value := l.Get(1)

	var binaryValue []byte
	if str, ok := value.(lua.LString); ok {
		binaryValue = []byte(str)
	}

	ud := l.NewUserData()
	ud.Value = &TypedValue{
		Type:  "binary",
		Value: binaryValue,
	}

	l.Push(ud)
	return 1
}

func asNull(l *lua.LState) int {
	ud := l.NewUserData()
	ud.Value = "SQL_NULL"
	l.Push(ud)
	return 1
}
