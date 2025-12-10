package sql

import (
	"testing"

	lua "github.com/yuin/gopher-lua"
)

func TestModuleConstants(t *testing.T) {
	tests := []struct {
		name     string
		constant string
		expected string
	}{
		{"TypePostgres", TypePostgres, "postgres"},
		{"TypeMySQL", TypeMySQL, "mysql"},
		{"TypeSQLite", TypeSQLite, "sqlite"},
		{"TypeMSSQL", TypeMSSQL, "mssql"},
		{"TypeOracle", TypeOracle, "oracle"},
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
