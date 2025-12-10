package sql

import (
	"testing"

	lua "github.com/yuin/gopher-lua"
)

func TestBuilderExprWithAllNilArgs(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	l.Push(lua.LString("SELECT ? WHERE ? = ?"))
	l.Push(lua.LNil)
	l.Push(lua.LNil)
	l.Push(lua.LNil)
	builderExpr(l)

	result := l.Get(-1)
	if result == lua.LNil {
		t.Error("expected non-nil result")
	}
}

func TestBuilderExprWithMixedArgs(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	l.Push(lua.LString("SELECT * FROM users WHERE id = ? AND name = ?"))
	l.Push(lua.LNumber(42))
	l.Push(lua.LString("John"))
	builderExpr(l)

	result := l.Get(-1)
	if result == lua.LNil {
		t.Error("expected non-nil result")
	}
}

func TestBuilderEqWithMultipleFields(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	tbl := l.CreateTable(0, 3)
	tbl.RawSetString("id", lua.LNumber(1))
	tbl.RawSetString("active", lua.LTrue)
	tbl.RawSetString("name", lua.LString("test"))
	l.Push(tbl)
	builderEq(l)

	result := l.Get(-1)
	if result == lua.LNil {
		t.Error("expected non-nil result")
	}
}

func TestBuilderNotEqWithMultipleFields(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	tbl := l.CreateTable(0, 2)
	tbl.RawSetString("status", lua.LString("deleted"))
	tbl.RawSetString("archived", lua.LTrue)
	l.Push(tbl)
	builderNotEq(l)

	result := l.Get(-1)
	if result == lua.LNil {
		t.Error("expected non-nil result")
	}
}

func TestBuilderLtEmpty(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	tbl := l.CreateTable(0, 0)
	l.Push(tbl)
	builderLt(l)

	result := l.Get(-1)
	if result == lua.LNil {
		t.Error("expected non-nil result even for empty table")
	}
}

func TestBuilderGtEmpty(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	tbl := l.CreateTable(0, 0)
	l.Push(tbl)
	builderGt(l)

	result := l.Get(-1)
	if result == lua.LNil {
		t.Error("expected non-nil result even for empty table")
	}
}

func TestBuilderLtOrEqEmpty(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	tbl := l.CreateTable(0, 0)
	l.Push(tbl)
	builderLtOrEq(l)

	result := l.Get(-1)
	if result == lua.LNil {
		t.Error("expected non-nil result even for empty table")
	}
}

func TestBuilderGtOrEqEmpty(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	tbl := l.CreateTable(0, 0)
	l.Push(tbl)
	builderGtOrEq(l)

	result := l.Get(-1)
	if result == lua.LNil {
		t.Error("expected non-nil result even for empty table")
	}
}

func TestBuilderLikeEmpty(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	tbl := l.CreateTable(0, 0)
	l.Push(tbl)
	builderLike(l)

	result := l.Get(-1)
	if result == lua.LNil {
		t.Error("expected non-nil result even for empty table")
	}
}

func TestBuilderNotLikeEmpty(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	tbl := l.CreateTable(0, 0)
	l.Push(tbl)
	builderNotLike(l)

	result := l.Get(-1)
	if result == lua.LNil {
		t.Error("expected non-nil result even for empty table")
	}
}

func TestBuilderAndEmpty(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	arr := l.CreateTable(0, 0)
	l.Push(arr)
	builderAnd(l)

	result := l.Get(-1)
	if result == lua.LNil {
		t.Error("expected non-nil result even for empty array")
	}
}

func TestBuilderOrEmpty(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	arr := l.CreateTable(0, 0)
	l.Push(arr)
	builderOr(l)

	result := l.Get(-1)
	if result == lua.LNil {
		t.Error("expected non-nil result even for empty array")
	}
}

func TestGoValueToLuaAllTypes(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	tests := []struct {
		name  string
		input interface{}
	}{
		{"nil", nil},
		{"true", true},
		{"false", false},
		{"int", int(42)},
		{"int64", int64(100)},
		{"float64", float64(3.14)},
		{"string", "hello"},
		{"bytes", []byte("data")},
		{"unknown", struct{}{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := goValueToLua(l, tt.input)
			if result == nil {
				t.Error("expected non-nil result")
			}
		})
	}
}

func TestGoArgsToLuaTableEmpty(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	result := goArgsToLuaTable(l, []any{})

	if result.Len() != 0 {
		t.Errorf("expected empty table, got length %d", result.Len())
	}
}

func TestGoArgsToLuaTableMixed(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	args := []any{
		"string",
		int64(42),
		float64(3.14),
		true,
		nil,
		[]byte("bytes"),
	}

	result := goArgsToLuaTable(l, args)

	if result.Len() != len(args) {
		t.Errorf("expected table length %d, got %d", len(args), result.Len())
	}
}

func TestBuilderAndWithSqlizer(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	arr := l.CreateTable(1, 0)

	tbl := l.CreateTable(0, 1)
	tbl.RawSetString("id", lua.LNumber(1))
	arr.RawSetInt(1, tbl)

	l.Push(arr)
	builderAnd(l)

	result := l.Get(-1)
	if result == lua.LNil {
		t.Error("expected non-nil result")
	}
}

func TestBuilderOrWithSqlizer(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	arr := l.CreateTable(1, 0)

	tbl := l.CreateTable(0, 1)
	tbl.RawSetString("name", lua.LString("test"))
	arr.RawSetInt(1, tbl)

	l.Push(arr)
	builderOr(l)

	result := l.Get(-1)
	if result == lua.LNil {
		t.Error("expected non-nil result")
	}
}

func TestLuaTableToMapWithIntegerKeys(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	tbl := l.CreateTable(2, 0)
	tbl.RawSetInt(1, lua.LString("first"))
	tbl.RawSetInt(2, lua.LString("second"))

	result := luaTableToMap(tbl)

	if len(result) != 0 {
		t.Errorf("expected 0 string-keyed entries for integer keys, got %d", len(result))
	}
}

func TestBuilderExprNoArgs(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	l.Push(lua.LString("SELECT 1"))
	builderExpr(l)

	result := l.Get(-1)
	ud, ok := result.(*lua.LUserData)
	if !ok {
		t.Fatalf("expected userdata, got %T", result)
	}

	if ud.Value == nil {
		t.Error("expected non-nil sqlizer")
	}
}

func TestCheckParamsWithInteger(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	tbl := l.CreateTable(1, 0)
	tbl.RawSetInt(1, lua.LInteger(123))

	l.Push(tbl)

	params, err := checkParams(l, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(params) != 1 {
		t.Fatalf("expected 1 param, got %d", len(params))
	}

	if params[0] != int64(123) {
		t.Errorf("expected int64(123), got %v (%T)", params[0], params[0])
	}
}
