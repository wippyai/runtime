package sql

import (
	"testing"

	lua "github.com/yuin/gopher-lua"
)

func TestCheckParamsNil(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	l.Push(lua.LNil)

	params, err := checkParams(l, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if params != nil {
		t.Errorf("expected nil params, got %v", params)
	}
}

func TestCheckParamsEmptyTable(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	tbl := l.CreateTable(0, 0)
	l.Push(tbl)

	params, err := checkParams(l, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if params != nil {
		t.Errorf("expected nil params for empty table, got %v", params)
	}
}

func TestCheckParamsInvalidType(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	l.Push(lua.LString("not a table"))

	_, err := checkParams(l, 1)
	if err == nil {
		t.Error("expected error for non-table parameter")
	}
}

func TestToGoValueBool(t *testing.T) {
	result := toGoValue(lua.LTrue)
	if result != true {
		t.Errorf("expected true, got %v", result)
	}

	result = toGoValue(lua.LFalse)
	if result != false {
		t.Errorf("expected false, got %v", result)
	}
}

func TestToGoValueNumber(t *testing.T) {
	result := toGoValue(lua.LNumber(3.14))
	if result != float64(3.14) {
		t.Errorf("expected 3.14, got %v", result)
	}
}

func TestToGoValueInteger(t *testing.T) {
	result := toGoValue(lua.LInteger(42))
	if result != int64(42) {
		t.Errorf("expected 42, got %v", result)
	}
}

func TestToGoValueString(t *testing.T) {
	result := toGoValue(lua.LString("hello"))
	if result != "hello" {
		t.Errorf("expected 'hello', got %v", result)
	}
}

func TestToGoValueNil(t *testing.T) {
	result := toGoValue(lua.LNil)
	if result != nil {
		t.Errorf("expected nil, got %v", result)
	}
}

func TestToGoValueUnknown(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	tbl := l.CreateTable(0, 0)
	result := toGoValue(tbl)
	if result != nil {
		t.Errorf("expected nil for unknown type, got %v", result)
	}
}
