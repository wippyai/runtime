// SPDX-License-Identifier: MPL-2.0

package sql

import (
	"testing"

	lua "github.com/wippyai/go-lua"
)

func TestModuleLoads(t *testing.T) {
	mod, yields := Module.Build()

	if mod == nil {
		t.Fatal("expected module table to be non-nil")
	}

	if len(yields) != 13 {
		t.Errorf("expected 13 yield types, got %d", len(yields))
	}
}

func TestModuleReuse(t *testing.T) {
	mod1, _ := Module.Build()
	mod2, _ := Module.Build()

	if mod1 != mod2 {
		t.Error("expected module tables to be identical on reuse")
	}
}

func TestModuleImmutable(t *testing.T) {
	mod, _ := Module.Build()

	if !mod.Immutable {
		t.Error("expected module to be immutable")
	}
}

func TestModuleHasGet(t *testing.T) {
	mod, _ := Module.Build()

	getFunc := mod.RawGetString("get")
	if getFunc == lua.LNil {
		t.Error("expected module to have 'get' function")
	}
}

func TestModuleHasNULL(t *testing.T) {
	mod, _ := Module.Build()

	nullVal := mod.RawGetString("NULL")
	if nullVal == lua.LNil {
		t.Error("expected module to have 'NULL' constant")
	}

	if ud, ok := nullVal.(*lua.LUserData); ok {
		if ud.Value != "SQL_NULL" {
			t.Errorf("expected NULL value to be 'SQL_NULL', got %v", ud.Value)
		}
	} else {
		t.Errorf("expected NULL to be userdata, got %T", nullVal)
	}
}

func TestModuleHasTypes(t *testing.T) {
	mod, _ := Module.Build()

	types := mod.RawGetString("type")
	if types == lua.LNil {
		t.Error("expected module to have 'type' table")
	}

	table, ok := types.(*lua.LTable)
	if !ok {
		t.Fatalf("expected types to be a table, got %T", types)
	}

	expectedTypes := []string{"POSTGRES", "MYSQL", "SQLITE", "UNKNOWN"}
	for _, name := range expectedTypes {
		if table.RawGetString(name) == lua.LNil {
			t.Errorf("expected type table to have '%s'", name)
		}
	}
}

func TestModuleHasIsolation(t *testing.T) {
	mod, _ := Module.Build()

	isolation := mod.RawGetString("isolation")
	if isolation == lua.LNil {
		t.Error("expected module to have 'isolation' table")
	}

	table, ok := isolation.(*lua.LTable)
	if !ok {
		t.Fatalf("expected isolation to be a table, got %T", isolation)
	}

	expectedLevels := []string{"DEFAULT", "READ_UNCOMMITTED", "READ_COMMITTED", "WRITE_COMMITTED", "REPEATABLE_READ", "SERIALIZABLE"}
	for _, name := range expectedLevels {
		if table.RawGetString(name) == lua.LNil {
			t.Errorf("expected isolation table to have '%s'", name)
		}
	}
}

func TestModuleHasAs(t *testing.T) {
	mod, _ := Module.Build()

	as := mod.RawGetString("as")
	if as == lua.LNil {
		t.Error("expected module to have 'as' submodule")
	}

	table, ok := as.(*lua.LTable)
	if !ok {
		t.Fatalf("expected as to be a table, got %T", as)
	}

	expectedFuncs := []string{"int", "float", "text", "binary", "null"}
	for _, name := range expectedFuncs {
		if table.RawGetString(name) == lua.LNil {
			t.Errorf("expected as table to have '%s' function", name)
		}
	}
}

func TestModuleHasBuilder(t *testing.T) {
	mod, _ := Module.Build()

	builder := mod.RawGetString("builder")
	if builder == lua.LNil {
		t.Error("expected module to have 'builder' submodule")
	}

	table, ok := builder.(*lua.LTable)
	if !ok {
		t.Fatalf("expected builder to be a table, got %T", builder)
	}

	expectedFuncs := []string{"select", "insert", "update", "delete", "expr", "eq", "not_eq", "lt", "lte", "gt", "gte", "like", "not_like", "and_", "or_"}
	for _, name := range expectedFuncs {
		if table.RawGetString(name) == lua.LNil {
			t.Errorf("expected builder table to have '%s' function", name)
		}
	}

	expectedPlaceholders := []string{"question", "dollar", "at", "colon", "default_placeholder"}
	for _, name := range expectedPlaceholders {
		if table.RawGetString(name) == lua.LNil {
			t.Errorf("expected builder table to have '%s' placeholder", name)
		}
	}
}

func TestAsInt(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	l.Push(lua.LNumber(42))
	asInt(l)

	result := l.Get(-1)
	ud, ok := result.(*lua.LUserData)
	if !ok {
		t.Fatalf("expected userdata, got %T", result)
	}

	typed, ok := ud.Value.(*TypedValue)
	if !ok {
		t.Fatalf("expected TypedValue, got %T", ud.Value)
	}

	if typed.Type != "int" {
		t.Errorf("expected type 'int', got %s", typed.Type)
	}

	if typed.Value != int64(42) {
		t.Errorf("expected value 42, got %v", typed.Value)
	}
}

func TestAsFloat(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	l.Push(lua.LNumber(3.14))
	asFloat(l)

	result := l.Get(-1)
	ud, ok := result.(*lua.LUserData)
	if !ok {
		t.Fatalf("expected userdata, got %T", result)
	}

	typed, ok := ud.Value.(*TypedValue)
	if !ok {
		t.Fatalf("expected TypedValue, got %T", ud.Value)
	}

	if typed.Type != "float" {
		t.Errorf("expected type 'float', got %s", typed.Type)
	}

	if typed.Value != 3.14 {
		t.Errorf("expected value 3.14, got %v", typed.Value)
	}
}

func TestAsText(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	l.Push(lua.LString("hello"))
	asText(l)

	result := l.Get(-1)
	ud, ok := result.(*lua.LUserData)
	if !ok {
		t.Fatalf("expected userdata, got %T", result)
	}

	typed, ok := ud.Value.(*TypedValue)
	if !ok {
		t.Fatalf("expected TypedValue, got %T", ud.Value)
	}

	if typed.Type != "text" {
		t.Errorf("expected type 'text', got %s", typed.Type)
	}

	if typed.Value != "hello" {
		t.Errorf("expected value 'hello', got %v", typed.Value)
	}
}

func TestAsBinary(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	l.Push(lua.LString("binary data"))
	asBinary(l)

	result := l.Get(-1)
	ud, ok := result.(*lua.LUserData)
	if !ok {
		t.Fatalf("expected userdata, got %T", result)
	}

	typed, ok := ud.Value.(*TypedValue)
	if !ok {
		t.Fatalf("expected TypedValue, got %T", ud.Value)
	}

	if typed.Type != "binary" {
		t.Errorf("expected type 'binary', got %s", typed.Type)
	}

	bytes, ok := typed.Value.([]byte)
	if !ok {
		t.Fatalf("expected []byte, got %T", typed.Value)
	}

	if string(bytes) != "binary data" {
		t.Errorf("expected value 'binary data', got %s", string(bytes))
	}
}

func TestAsNull(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	asNull(l)

	result := l.Get(-1)
	ud, ok := result.(*lua.LUserData)
	if !ok {
		t.Fatalf("expected userdata, got %T", result)
	}

	if ud.Value != "SQL_NULL" {
		t.Errorf("expected 'SQL_NULL', got %v", ud.Value)
	}
}

func TestCheckParamsWithTypedValues(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	tbl := l.CreateTable(3, 0)
	tbl.RawSetInt(1, lua.LNumber(42))

	intUD := l.NewUserData()
	intUD.Value = &TypedValue{Type: "int", Value: int64(100)}
	tbl.RawSetInt(2, intUD)

	nullUD := l.NewUserData()
	nullUD.Value = "SQL_NULL"
	tbl.RawSetInt(3, nullUD)

	l.Push(tbl)

	params, err := checkParams(l, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(params) != 3 {
		t.Fatalf("expected 3 params, got %d", len(params))
	}

	if params[0] != float64(42) {
		t.Errorf("expected first param to be 42.0, got %v", params[0])
	}

	if params[1] != int64(100) {
		t.Errorf("expected second param to be int64(100), got %v (%T)", params[1], params[1])
	}

	if params[2] != nil {
		t.Errorf("expected third param to be nil, got %v", params[2])
	}
}

func TestLuaTableToMap(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	tbl := l.CreateTable(0, 3)
	tbl.RawSetString("name", lua.LString("John"))
	tbl.RawSetString("age", lua.LNumber(30))

	nullUD := l.NewUserData()
	nullUD.Value = "SQL_NULL"
	tbl.RawSetString("deleted_at", nullUD)

	result := luaTableToMap(tbl)

	if result["name"] != "John" {
		t.Errorf("expected name to be 'John', got %v", result["name"])
	}

	if result["age"] != float64(30) {
		t.Errorf("expected age to be 30.0, got %v", result["age"])
	}

	if result["deleted_at"] != nil {
		t.Errorf("expected deleted_at to be nil, got %v", result["deleted_at"])
	}
}

func TestModuleConstants(t *testing.T) {
	tests := []struct {
		name     string
		constant string
		expected string
	}{
		{"TypePostgres", TypePostgres, "postgres"},
		{"TypeMySQL", TypeMySQL, "mysql"},
		{"TypeSQLite", TypeSQLite, "sqlite"},
		{"TypeUnknown", TypeUnknown, "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.constant != tt.expected {
				t.Errorf("%s = %s, want %s", tt.name, tt.constant, tt.expected)
			}
		})
	}
}

func TestIsolationConstants(t *testing.T) {
	tests := []struct {
		name     string
		constant string
		expected string
	}{
		{"IsolationDefault", IsolationDefault, "default"},
		{"IsolationReadUncommitted", IsolationReadUncommitted, "read_uncommitted"},
		{"IsolationReadCommitted", IsolationReadCommitted, "read_committed"},
		{"IsolationWriteCommitted", IsolationWriteCommitted, "write_committed"},
		{"IsolationRepeatableRead", IsolationRepeatableRead, "repeatable_read"},
		{"IsolationSerializable", IsolationSerializable, "serializable"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.constant != tt.expected {
				t.Errorf("%s = %s, want %s", tt.name, tt.constant, tt.expected)
			}
		})
	}
}

func TestModuleTypeNames(t *testing.T) {
	tests := []struct {
		name     string
		constant string
		expected string
	}{
		{"dbTypeName", dbTypeName, "sql.DB"},
		{"statementTypeName", statementTypeName, "sql.Statement"},
		{"transactionTypeName", transactionTypeName, "sql.Transaction"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.constant != tt.expected {
				t.Errorf("%s = %s, want %s", tt.name, tt.constant, tt.expected)
			}
		})
	}
}

func TestCheckParamsWithMixedValues(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	tbl := l.CreateTable(3, 0)
	tbl.RawSetInt(1, lua.LString("text"))
	tbl.RawSetInt(2, lua.LNumber(3.14))
	tbl.RawSetInt(3, lua.LTrue)

	l.Push(tbl)

	params, err := checkParams(l, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(params) != 3 {
		t.Fatalf("expected 3 params, got %d", len(params))
	}

	if params[0] != "text" {
		t.Errorf("expected 'text', got %v", params[0])
	}

	if params[1] != 3.14 {
		t.Errorf("expected 3.14, got %v", params[1])
	}

	if params[2] != true {
		t.Errorf("expected true, got %v", params[2])
	}
}

func TestLuaTableToMapWithSQLNull(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	tbl := l.CreateTable(0, 1)

	nullUD := l.NewUserData()
	nullUD.Value = "SQL_NULL"
	tbl.RawSetString("nullable", nullUD)

	result := luaTableToMap(tbl)

	if result["nullable"] != nil {
		t.Errorf("expected nil for SQL_NULL, got %v", result["nullable"])
	}
}

func TestLuaTableToMapWithNonTypedUserData(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	tbl := l.CreateTable(0, 1)

	ud := l.NewUserData()
	ud.Value = "some other value"
	tbl.RawSetString("userdata", ud)

	result := luaTableToMap(tbl)

	if val, exists := result["userdata"]; !exists || val != nil {
		t.Errorf("expected nil for non-typed userdata, got %v", val)
	}
}
