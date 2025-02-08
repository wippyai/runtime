package errors

import (
	"errors"
	"fmt"
	lua "github.com/yuin/gopher-lua"
	"strings"
	"testing"
)

func TestWrappedErrorWithStack(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	ConfigureErrorMetatable(L)

	// Nested Go functions with source info
	deepGoFunc := func() error {
		return fmt.Errorf("deep error at errors_test.go:29")
	}

	middleGoFunc := func() error {
		if err := deepGoFunc(); err != nil {
			return fmt.Errorf("middle layer at errors_test.go:35: %w", err)
		}
		return nil
	}

	testFunc := func(L *lua.LState) int {
		err := middleGoFunc()
		if err != nil {
			RaiseError(L, err)
		}
		return 0
	}

	L.SetGlobal("test_error", L.NewFunction(testFunc))

	script := `
        function deep_lua_func()
            return test_error()
        end

        function middle_lua_func()
            local ok, err = pcall(deep_lua_func)
            if not ok then
                error(err, 2)
            end
            return ok
        end

        middle_lua_func()
    `

	err := L.DoString(script)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	wrapped := GetWrappedError(err)
	if wrapped == nil {
		t.Fatal("failed to get wrapped error")
	}

	// Build and print actual error chain for debugging
	var actualChain []string
	current := wrapped.err // Start with the wrapped error's inner error
	for current != nil {
		actualChain = append(actualChain, current.Error())
		current = errors.Unwrap(current)
	}

	// Expected error chain
	expectedChain := []string{
		"middle layer at errors_test.go:35: deep error at errors_test.go:29",
		"deep error at errors_test.go:29",
	}

	if len(actualChain) != len(expectedChain) {
		t.Fatalf("error chain length mismatch: want %d, got %d", len(expectedChain), len(actualChain))
	}

	for i, expected := range expectedChain {
		if actualChain[i] != expected {
			t.Errorf("error %d mismatch:\nwant: %s\ngot:  %s", i, expected, actualChain[i])
		}
	}

	// Validate stack trace contains key elements
	stack := wrapped.Stack()
	requiredElements := []string{
		"Lua Stack:",
		"Thread:",
		"test_error",
		"middle_lua_func",
		"Go Stack:",
		"WrapError",
		"errors_test.go",
	}

	for _, elem := range requiredElements {
		if !strings.Contains(stack, elem) {
			t.Errorf("stack trace missing required element: %s", elem)
		}
	}
}

func TestDirectErrorPropagation(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	ConfigureErrorMetatable(L)

	deepGoFunc := func() error {
		return fmt.Errorf("deep error at errors_test.go:29")
	}

	middleGoFunc := func() error {
		if err := deepGoFunc(); err != nil {
			return fmt.Errorf("middle layer at errors_test.go:35: %w", err)
		}
		return nil
	}

	testFunc := func(L *lua.LState) int {
		err := middleGoFunc()
		if err != nil {
			RaiseError(L, err)
		}
		return 0
	}

	L.SetGlobal("test_error", L.NewFunction(testFunc))

	script := `
        function deep_lua_func()
            return test_error()
        end

        function middle_lua_func()
            local ok, err = deep_lua_func()
            if not ok then
                error(err)
            end
            return ok
        end

        middle_lua_func()
    `

	err := L.DoString(script)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	wrapped := GetWrappedError(err)
	if wrapped == nil {
		t.Fatal("failed to get wrapped error")
	}

	// Validate error preserves both stacks
	stack := wrapped.Stack()
	if !strings.Contains(stack, "Lua Stack:") || !strings.Contains(stack, "Go Stack:") {
		t.Error("stack trace missing either Lua or Go stack")
	}

	// Validate error chain preserved through direct propagation
	if !strings.Contains(wrapped.Error(), "middle layer") {
		t.Error("error chain broken in direct propagation")
	}
}

var (
	ErrTest = fmt.Errorf("test error marker")
)

func TestErrorIdentification(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	ConfigureErrorMetatable(L)

	deepGoFunc := func() error {
		return fmt.Errorf("deep error: %w", ErrTest)
	}

	middleGoFunc := func() error {
		if err := deepGoFunc(); err != nil {
			return fmt.Errorf("middle layer: %w", err)
		}
		return nil
	}

	testFunc := func(L *lua.LState) int {
		err := middleGoFunc()
		if err != nil {
			RaiseError(L, err)
		}
		return 0
	}

	L.SetGlobal("test_error", L.NewFunction(testFunc))

	script := `
        function deep_lua_func()
            return test_error()
        end

        function middle_lua_func()
            local ok, err = pcall(deep_lua_func)
            if not ok then
                error(err)
            end
            return ok
        end

        middle_lua_func()
    `

	err := L.DoString(script)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	wrapped := GetWrappedError(err)
	if wrapped == nil {
		t.Fatal("failed to get wrapped error")
	}

	// Test errors.Is works through our wrapper
	if !errors.Is(wrapped, ErrTest) {
		t.Error("failed to identify original error marker")
	}

	// Build and print actual error chain for debugging
	var actualChain []string
	current := wrapped.err // Start with the wrapped error's inner error
	for current != nil {
		actualChain = append(actualChain, current.Error())
		current = errors.Unwrap(current)
	}

	// Expected error chain in unwrapping order
	expectedChain := []string{
		"middle layer: deep error: test error marker",
		"deep error: test error marker",
		"test error marker",
	}

	if len(actualChain) != len(expectedChain) {
		t.Fatalf("error chain length mismatch: want %d, got %d", len(expectedChain), len(actualChain))
	}

	for i, expected := range expectedChain {
		if actualChain[i] != expected {
			t.Errorf("error %d mismatch:\nwant: %s\ngot:  %s", i, expected, actualChain[i])
		}
	}

	// Validate stacks are present
	stack := wrapped.Stack()
	if !strings.Contains(stack, "Lua Stack:") || !strings.Contains(stack, "Go Stack:") {
		t.Error("missing either Lua or Go stack trace")
	}
}
