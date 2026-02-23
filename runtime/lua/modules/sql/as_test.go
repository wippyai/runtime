// SPDX-License-Identifier: MPL-2.0

package sql

import (
	"testing"

	lua "github.com/wippyai/go-lua"
)

func TestAsIntFromInteger(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	l.Push(lua.LInteger(123))
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

	if typed.Value != int64(123) {
		t.Errorf("expected value 123, got %v", typed.Value)
	}
}

func TestAsFloatFromInteger(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	l.Push(lua.LInteger(50))
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

	if typed.Value != float64(50) {
		t.Errorf("expected value 50.0, got %v", typed.Value)
	}
}

func TestAsTextFromNumber(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	l.Push(lua.LNumber(42.5))
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

	if typed.Value == "" {
		t.Error("expected non-empty string")
	}
}

func TestAsTextFromInteger(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	l.Push(lua.LInteger(100))
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
}

func TestAsTextFromInvalidType(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	l.Push(lua.LTrue)
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

	if typed.Value != "" {
		t.Errorf("expected empty string for invalid type, got %v", typed.Value)
	}
}

func TestAsBinaryFromNonString(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	l.Push(lua.LNumber(42))
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

	if typed.Value == nil {
		return
	}
	if bytes, ok := typed.Value.([]byte); !ok || len(bytes) != 0 {
		t.Errorf("expected empty []byte for non-string, got %v", typed.Value)
	}
}

func TestAsIntFromInvalidType(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	l.Push(lua.LString("not a number"))
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

	if typed.Value != int64(0) {
		t.Errorf("expected 0 for invalid type, got %v", typed.Value)
	}
}

func TestAsFloatFromInvalidType(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	l.Push(lua.LString("not a number"))
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

	if typed.Value != 0.0 {
		t.Errorf("expected 0.0 for invalid type, got %v", typed.Value)
	}
}
