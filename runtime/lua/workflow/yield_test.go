package workflow

import (
	"errors"
	"testing"

	"github.com/wippyai/runtime/api/runtime/workflow"
	lua "github.com/yuin/gopher-lua"
)

func TestNewYield(t *testing.T) {
	y := NewYield(func() (any, error) {
		return "test-value", nil
	})

	if y == nil {
		t.Fatal("NewYield returned nil")
	}

	if y.Cmd == nil {
		t.Fatal("Cmd is nil")
	}

	if y.Cmd.Fn == nil {
		t.Fatal("Cmd.Fn is nil")
	}

	result, err := y.Cmd.Fn()
	if err != nil {
		t.Fatalf("Fn returned error: %v", err)
	}

	if result != "test-value" {
		t.Errorf("expected 'test-value', got %v", result)
	}
}

func TestYield_LuaValue(t *testing.T) {
	y := NewYield(func() (any, error) {
		return nil, nil
	})

	if y.String() != "<workflow_side_effect>" {
		t.Errorf("unexpected String(): %s", y.String())
	}

	if y.Type() != lua.LTUserData {
		t.Errorf("unexpected Type(): %v", y.Type())
	}
}

func TestYield_Command(t *testing.T) {
	y := NewYield(func() (any, error) {
		return nil, nil
	})

	if y.CmdID() != workflow.SideEffect {
		t.Errorf("unexpected CmdID: %v", y.CmdID())
	}

	cmd := y.ToCommand()
	if cmd == nil {
		t.Fatal("ToCommand returned nil")
	}

	if cmd.CmdID() != workflow.SideEffect {
		t.Errorf("command CmdID mismatch: %v", cmd.CmdID())
	}
}

func TestYield_HandleResult_Success(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	y := NewYield(func() (any, error) {
		return nil, nil
	})

	result := workflow.Result{
		Value: "test-uuid",
		Error: nil,
	}

	values := y.HandleResult(l, result, nil)

	if len(values) != 2 {
		t.Fatalf("expected 2 values, got %d", len(values))
	}

	if values[0].String() != "test-uuid" {
		t.Errorf("expected 'test-uuid', got %s", values[0].String())
	}

	if values[1] != lua.LNil {
		t.Errorf("expected nil error, got %v", values[1])
	}
}

func TestYield_HandleResult_Error(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	y := NewYield(func() (any, error) {
		return nil, nil
	})

	values := y.HandleResult(l, nil, errors.New("test error"))

	if len(values) != 2 {
		t.Fatalf("expected 2 values, got %d", len(values))
	}

	if values[0] != lua.LNil {
		t.Errorf("expected nil value, got %v", values[0])
	}

	if values[1] == lua.LNil {
		t.Error("expected error, got nil")
	}
}

func TestYield_HandleResult_ResultError(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	y := NewYield(func() (any, error) {
		return nil, nil
	})

	result := workflow.Result{
		Value: nil,
		Error: errors.New("result error"),
	}

	values := y.HandleResult(l, result, nil)

	if len(values) != 2 {
		t.Fatalf("expected 2 values, got %d", len(values))
	}

	if values[0] != lua.LNil {
		t.Errorf("expected nil value, got %v", values[0])
	}

	if values[1] == lua.LNil {
		t.Error("expected error, got nil")
	}
}

func TestYield_HandleResult_NilData(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	y := NewYield(func() (any, error) {
		return nil, nil
	})

	values := y.HandleResult(l, nil, nil)

	if len(values) != 2 {
		t.Fatalf("expected 2 values, got %d", len(values))
	}

	if values[0] != lua.LNil {
		t.Errorf("expected nil, got %v", values[0])
	}

	if values[1] != lua.LNil {
		t.Errorf("expected nil error, got %v", values[1])
	}
}

func TestYield_HandleResult_InvalidType(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	y := NewYield(func() (any, error) {
		return nil, nil
	})

	values := y.HandleResult(l, "not a Result type", nil)

	if len(values) != 2 {
		t.Fatalf("expected 2 values, got %d", len(values))
	}

	if values[0] != lua.LNil {
		t.Errorf("expected nil value, got %v", values[0])
	}

	if values[1] == lua.LNil {
		t.Error("expected error for invalid type, got nil")
	}
}
