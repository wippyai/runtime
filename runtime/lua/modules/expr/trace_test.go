package expr

import (
	"testing"
)

// pcall that succeeds
func TestPcall_Success(t *testing.T) {
	l := setupState()
	defer l.Close()
	err := l.DoString(`
		local success = pcall(function()
			return 42
		end)
	`)
	if err != nil {
		t.Fatalf("failed: %v", err)
	}
}

// pcall that fails
func TestPcall_Failure(t *testing.T) {
	l := setupState()
	defer l.Close()
	err := l.DoString(`
		local success = pcall(function()
			error("fail")
		end)
	`)
	if err != nil {
		t.Fatalf("failed: %v", err)
	}
}

func TestEvalAfterSuccess(t *testing.T) {
	l := setupState()
	defer l.Close()
	err := l.DoString(`
		local r, e = expr.eval("nil")
		print("Success: result=", r, "type=", type(r))
		if r ~= nil then error("expected nil") end
	`)
	if err != nil {
		t.Fatalf("failed: %v", err)
	}
}

func TestEvalAfterFailure(t *testing.T) {
	l := setupState()
	defer l.Close()
	err := l.DoString(`
		local r, e = expr.eval("nil")
		print("Failure: result=", r, "type=", type(r))
		if r ~= nil then error("expected nil") end
	`)
	if err != nil {
		t.Fatalf("failed: %v", err)
	}
}
