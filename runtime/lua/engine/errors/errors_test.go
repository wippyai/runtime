package errors

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	lua "github.com/yuin/gopher-lua"
)

func TestWrappedErrorWithStack(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	RegisterErrorsModule(L) // This now configures error metatable too

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
            local ok, Err = pcall(deep_lua_func)
            if not ok then
                error(Err, 2)
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
	//nolint:prealloc // ok for now
	var actualChain []string
	var current error
	if wrapped != nil {
		current = wrapped.Err // Launch with the wrapped error's inner error
	}
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
		"RaiseError",
		"middle_lua_func",
		"test_error",
		"TestWrappedErrorWithStack",
	}

	for _, elem := range requiredElements {
		if !strings.Contains(stack, elem) {
			t.Errorf("stack trace missing required element: %q", elem)
		}
	}
}

func TestDirectErrorPropagation(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	RegisterErrorsModule(L)

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
            local ok, Err = deep_lua_func()
            if not ok then
                error(Err)
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

	RegisterErrorsModule(L)

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
            local ok, Err = pcall(deep_lua_func)
            if not ok then
                error(Err)
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
	//nolint:prealloc // ok for now
	var actualChain []string
	var current error
	if wrapped != nil {
		current = wrapped.Err // Launch with the wrapped error's inner error
	}
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
}
func TestWrappedErrorReturnAndLuaError(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	RegisterErrorsModule(L)

	// Return wrapped error directly without raising
	testFunc := func(L *lua.LState) int {
		err := fmt.Errorf("test error from Go")
		wrapped := WrapError(L, err, "") // Empty Context for direct error

		// Spawn userdata and return it (don't raise)
		ud := L.NewUserData()
		ud.Value = wrapped
		L.SetMetatable(ud, L.GetTypeMetatable("error"))
		L.Push(ud)
		return 1
	}

	L.SetGlobal("get_wrapped_error", L.NewFunction(testFunc))

	script := `
        local Err = get_wrapped_error()
        error(Err) -- Use Lua's error() on the wrapped error
    `

	err := L.DoString(script)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	wrapped := GetWrappedError(err)
	if wrapped == nil {
		t.Fatal("failed to get wrapped error")
	}

	// Verify Go stack trace is preserved
	stack := wrapped.Stack()
	if !strings.Contains(stack, ".go") {
		t.Error("missing Go stack trace after Lua error()")
	}
}

func TestErrorToString(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	RegisterErrorsModule(L)

	testFunc := func(L *lua.LState) int {
		err := fmt.Errorf("test error message")
		wrapped := WrapError(L, err, "") // Empty Context for simple error

		ud := L.NewUserData()
		ud.Value = wrapped
		L.SetMetatable(ud, L.GetTypeMetatable("error"))
		L.Push(ud)
		return 1
	}

	L.SetGlobal("get_error", L.NewFunction(testFunc))

	script := `
        local Err = get_error()
        local str = tostring(Err)
        if str ~= "test error message" then
            error("Expected 'test error message', got: " .. str)
        end
    `

	if err := L.DoString(script); err != nil {
		t.Fatalf("Failed to convert error to string: %v", err)
	}
}

func TestLuaErrorWrapping(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	RegisterErrorsModule(L)

	script := `
       function deep_func()
           local base_err = "base error"
           local wrapped = errors.wrap(base_err, "middle error")
           local top_wrapped = errors.wrap(wrapped, "top error")
           error(top_wrapped)
       end

       -- Don't return the error, re-raise it
       deep_func()  -- This will raise the error
   `

	err := L.DoString(script)

	if err == nil {
		t.Fatal("expected error, got nil")
	}

	wrapped := GetWrappedError(err)
	if wrapped == nil {
		t.Fatal("failed to get wrapped error")
	}

	// Verify error chain
	errStr := wrapped.Error()
	if !strings.Contains(errStr, "top error") ||
		!strings.Contains(errStr, "middle error") ||
		!strings.Contains(errStr, "base error") {
		t.Errorf("error chain incomplete, got: %s", errStr)
	}

	// Verify stack trace captures Lua frames
	stack := wrapped.Stack()
	if !strings.Contains(stack, "deep_func") {
		t.Error("stack trace missing Lua frames")
	}

	if !strings.Contains(stack, "<string>:4") {
		t.Error("stack trace missing Lua frames")
	}
}

func TestLuaErrorWrappingWithPcall(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	RegisterErrorsModule(L)

	script := `
        function deep_func()
            local base_err = "base error"
            local wrapped = errors.wrap(base_err, "middle error")
            local top_wrapped = errors.wrap(wrapped, "top error")
            error(top_wrapped)
        end

        function safe_call()
            local ok, Err = pcall(deep_func) -- call line 10 (in lua terms)
            if not ok then
                if type(Err) == 'userdata' then
                    -- If it's already a wrapped error, re-raise it with level 2 to point to caller's location
                    error(Err, 2)
                else
                    -- If it's a string (like from normal error()), wrap it
                    error(errors.wrap("pcall failed", Err))
                end
            end
        end

        -- Call the function that uses pcall
        safe_call()
    `

	err := L.DoString(script)

	if err == nil {
		t.Fatal("expected error, got nil")
	}

	wrapped := GetWrappedError(err)
	if wrapped == nil {
		t.Fatal("failed to get wrapped error")
	}

	// Verify error chain
	errStr := wrapped.Error()
	if !strings.Contains(errStr, "top error") ||
		!strings.Contains(errStr, "middle error") ||
		!strings.Contains(errStr, "base error") {
		t.Errorf("error chain incomplete, got: %s", errStr)
	}

	// Verify stack trace contains key components
	stack := wrapped.Stack()
	requiredStackElements := []string{
		"<string>:10", // Location of error creation (instead of function name)
		"pcall",       // pcall should be in stack
		"errors.wrap", // Our wrapping function
		"safe_call",   // The function using pcall
	}

	for _, elem := range requiredStackElements {
		if !strings.Contains(stack, elem) {
			t.Errorf("stack trace missing required element: %q", elem)
		}
	}

	// Verify stack preserves error chain order
	var contexts []string
	current := wrapped
	for current != nil {
		if current.Context != "" {
			contexts = append(contexts, current.Context)
		}
		current = GetWrappedError(current.Unwrap())
	}

	expectedContexts := []string{"top error", "middle error"}
	if !reflect.DeepEqual(contexts, expectedContexts) {
		t.Errorf("wrong Context order:\nwant: %v\ngot:  %v", expectedContexts, contexts)
	}
}

func TestComplexInteropErrorWrapping(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	RegisterErrorsModule(L)

	createNestedLuaState := func(L *lua.LState) int {
		nestedL := lua.NewState()
		defer nestedL.Close()

		RegisterErrorsModule(nestedL)

		finalGoFunc := func(L *lua.LState) int {
			err := fmt.Errorf("deep error from final Go func")

			// Spawn and inspect the wrapped error at its origin
			wrappedErr := WrapError(L, err, "original error")

			RaiseError(L, wrappedErr)
			return 0
		}

		nestedL.SetGlobal("final_go_func", nestedL.NewFunction(finalGoFunc))

		nestedScript := `
           local ok, Err = pcall(function() 
				local result = final_go_func()
				return result
			end)
			if not ok then
				if type(Err) == 'userdata' then
					local wrapped = errors.wrap(Err, "nested lua wrapper")
					error(wrapped)
				else
					error("unexpected error type: " .. type(Err))
				end

                -- Inspect error right after pcall
                if type(Err) == 'userdata' then
                    local wrapped = errors.wrap(Err, "nested lua wrapper")
                    error(wrapped)
                else
                    error("unexpected error type from pcall: " .. type(Err))
                end
            end
            return true
        `

		err := nestedL.DoString(nestedScript)
		if err != nil {
			if wrapped := GetWrappedError(err); wrapped != nil {
				// Spawn new userdata in parent state
				ud := L.NewUserData()
				ud.Value = wrapped
				L.SetMetatable(ud, L.GetTypeMetatable("error"))

				L.Error(ud, 0)
			} else {
				RaiseError(L, fmt.Errorf("error from nested state (unwrapped): %w", err))
			}
		}
		return 0
	}

	L.SetGlobal("create_nested_state", L.NewFunction(createNestedLuaState))

	script := `
        local ok, Err = pcall(function()
            create_nested_state()
        end)
        
        if not ok then
            if type(Err) == 'userdata' then
                local wrapped = errors.wrap(Err, "top level wrapper")
                error(wrapped)
            else
                error("unexpected error type: " .. type(Err))
            end
        end
    `

	err := L.DoString(script)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	wrapped := GetWrappedError(err)
	if wrapped == nil {
		t.Fatal("failed to get wrapped error")
	}

	// Verify error chain components
	errStr := wrapped.Error()
	expectedComponents := []string{
		"top level wrapper",
		"nested lua wrapper",
		"deep error from final Go func",
	}

	for _, component := range expectedComponents {
		if !strings.Contains(errStr, component) {
			t.Errorf("error chain missing expected component: %q", component)
		}
	}

	// Verify stack trace contains key elements
	stack := wrapped.Stack()
	requiredElements := []string{
		"top level wrapper",
		"nested lua wrapper",
		"original error",
		"final_go_func",
		"<string>:2", // func call
	}

	for _, elem := range requiredElements {
		if !strings.Contains(stack, elem) {
			t.Errorf("stack trace missing required element: %q", elem)
		}
	}

	// Verify stack traces are preserved in correct order
	var contexts []string
	var current = wrapped
	for current != nil {
		if current.Context != "" {
			contexts = append(contexts, current.Context)
		}
		if next := GetWrappedError(current.Unwrap()); next != nil {
			current = next
		} else {
			break
		}
	}

	expectedOrder := []string{
		"top level wrapper",
		"nested lua wrapper",
		"original error",
	}

	if len(contexts) != len(expectedOrder) {
		t.Errorf("expected %d error contexts, got %d", len(expectedOrder), len(contexts))
	} else {
		for i, expected := range expectedOrder {
			if contexts[i] != expected {
				t.Errorf("wrong Context at position %d: expected %q, got %q", i, expected, contexts[i])
			}
		}
	}
}
