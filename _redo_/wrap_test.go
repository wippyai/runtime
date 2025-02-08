package _redo_

import (
	"fmt"
	lua "github.com/yuin/gopher-lua"
	"reflect"
	"strings"
	"testing"
)

func TestLuaErrorWrapping(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	// Add debug print function to Lua
	L.SetGlobal("debug_print", L.NewFunction(func(L *lua.LState) int {
		msg := L.ToString(1)
		t.Logf("Lua debug: %s", msg)
		return 0
	}))

	ConfigureErrorMetatable(L)
	RegisterErrorsModule(L)

	script := `
       function deep_func()
           debug_print("entering deep_func")
           local base_err = "base error"
           debug_print("created base_err: " .. tostring(base_err))

           debug_print("wrapping with middle error")
           local wrapped = errors.wrap(base_err, "middle error")
           debug_print("middle wrap result: " .. tostring(wrapped))

           debug_print("wrapping with top error")
           local top_wrapped = errors.wrap(wrapped, "top error")
           debug_print("top wrap result: " .. tostring(top_wrapped))

           debug_print("raising error")
           error(top_wrapped)
       end

       -- Don't return the error, re-raise it
       debug_print("starting main script")
       deep_func()  -- This will raise the error
   `

	t.Log("Running script...")
	err := L.DoString(script)
	t.Logf("Script result - err: %v", err)

	if err == nil {
		t.Fatal("expected error, got nil")
	}

	wrapped := GetWrappedError(err)
	if wrapped == nil {
		t.Fatal("failed to get wrapped error")
	}

	t.Logf("Got wrapped error: %+v", wrapped)
	t.Logf("Error string: %s", wrapped.Error())
	t.Logf("Stack trace: %s", wrapped.Stack())

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
}

func TestLuaErrorWrappingWithPcall(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	RegisterErrorsModule(L)

	debugPrint := func(L *lua.LState) int {
		msg := L.ToString(1)
		t.Logf("Lua debug: %s", msg)
		return 0
	}
	L.SetGlobal("debug_print", L.NewFunction(debugPrint))

	script := `
        function deep_func()
            debug_print("entering deep_func")
            local base_err = "base error"
            debug_print("created base_err: " .. tostring(base_err))
            
            debug_print("wrapping with middle error")
            local wrapped = errors.wrap(base_err, "middle error")
            debug_print("middle wrap result: " .. tostring(wrapped))
            
            debug_print("wrapping with top error")
            local top_wrapped = errors.wrap(wrapped, "top error")
            debug_print("top wrap result: " .. tostring(top_wrapped))
            
            debug_print("raising error")
            error(top_wrapped)
        end

        function safe_call()
            debug_print("entering safe_call")
            local ok, err = pcall(deep_func)
            if not ok then
                debug_print("pcall caught error: " .. tostring(err))
                -- Re-wrap the error from pcall to preserve the error object
                if type(err) == 'userdata' then
                    -- If it's already a wrapped error, re-raise it with level 2
                    -- to point to caller's location
                    error(err, 2)
                else
                    -- If it's a string (like from normal error()), wrap it
                    error(errors.wrap("pcall failed", err))
                end
            end
        end

        -- Call the function that uses pcall
        safe_call()
    `

	t.Log("Running script...")
	err := L.DoString(script)
	t.Logf("Script result - err: %v", err)

	if err == nil {
		t.Fatal("expected error, got nil")
	}

	wrapped := GetWrappedError(err)
	if wrapped == nil {
		t.Fatal("failed to get wrapped error")
	}

	t.Logf("Got wrapped error: %+v", wrapped)
	t.Logf("Error string: %s", wrapped.Error())
	t.Logf("Stack trace: %s", wrapped.Stack())

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
		"<string>:12", // Location of deep_func error creation
		"pcall",       // pcall should be in stack
		"errors.wrap", // Our wrapping function
		"safe_call",   // The function using pcall
		"deep_func",   // The function raising the error
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
		if current.context != "" {
			contexts = append(contexts, current.context)
		}
		current = GetWrappedError(current.Unwrap())
	}

	expectedContexts := []string{"top error", "middle error"}
	if !reflect.DeepEqual(contexts, expectedContexts) {
		t.Errorf("wrong context order:\nwant: %v\ngot:  %v", expectedContexts, contexts)
	}
}

func TestComplexInteropErrorWrapping(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	ConfigureErrorMetatable(L)
	RegisterErrorsModule(L)

	debugPrint := func(L *lua.LState) int {
		msg := L.ToString(1)
		t.Logf("Lua debug: %s", msg)
		return 0
	}
	L.SetGlobal("debug_print", L.NewFunction(debugPrint))

	createNestedLuaState := func(L *lua.LState) int {
		t.Log("Go: Creating nested Lua state")
		nestedL := lua.NewState()
		defer nestedL.Close()

		ConfigureErrorMetatable(nestedL)
		RegisterErrorsModule(nestedL)
		nestedL.SetGlobal("debug_print", nestedL.NewFunction(debugPrint))

		finalGoFunc := func(L *lua.LState) int {
			t.Log("Go (nested): In final Go function")
			err := fmt.Errorf("deep error from final Go func")

			// Create and inspect the wrapped error at its origin
			wrappedErr := WrapError(L, err, "original error")
			t.Logf("Original wrapped error at creation:\n%s", wrappedErr.Stack())

			RaiseError(L, err)
			return 0
		}

		nestedL.SetGlobal("final_go_func", nestedL.NewFunction(finalGoFunc))

		nestedScript := `
            debug_print("Nested Lua: About to call final Go func with pcall")
            local ok, err = pcall(final_go_func)
            if not ok then
                debug_print("Nested Lua: pcall caught error: " .. tostring(err))
                
                -- Inspect error right after pcall
                if type(err) == 'userdata' then
                    debug_print("Nested Lua: Got userdata error, attempting to wrap")
                    local wrapped = errors.wrap(err, "nested lua wrapper")
                    debug_print("Nested Lua: Error after wrapping: " .. tostring(wrapped))
                    error(wrapped)
                else
                    debug_print("Nested Lua: Unexpected error type: " .. type(err))
                    error("unexpected error type from pcall: " .. type(err))
                end
            end
            return true
        `

		err := nestedL.DoString(nestedScript)
		if err != nil {
			t.Logf("Error from nested state: %T: %v", err, err)

			if wrapped := GetWrappedError(err); wrapped != nil {
				t.Logf("Nested state wrapped error stack BEFORE transfer:\n%s", wrapped.Stack())

				// Create new userdata in parent state
				ud := L.NewUserData()
				ud.Value = wrapped
				L.SetMetatable(ud, L.GetTypeMetatable("error"))

				// Log stack trace right before raising in parent
				t.Logf("Stack trace before raising in parent:\n%s", wrapped.Stack())
				L.Error(ud, 0)
			} else {
				t.Log("Failed to get wrapped error from nested state")
				RaiseError(L, fmt.Errorf("error from nested state (unwrapped): %v", err))
			}
		}
		return 0
	}

	L.SetGlobal("create_nested_state", L.NewFunction(createNestedLuaState))

	script := `
        debug_print("Main Lua: Starting")
        local ok, err = pcall(function()
            debug_print("Main Lua: Calling create_nested_state")
            create_nested_state()
        end)
        
        if not ok then
            debug_print("Main Lua: Caught error: " .. tostring(err))
            if type(err) == 'userdata' then
                debug_print("Main Lua: Wrapping caught error")
                local wrapped = errors.wrap(err, "top level wrapper")
                debug_print("Main Lua: Final error: " .. tostring(wrapped))
                error(wrapped)
            else
                error("unexpected error type: " .. type(err))
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

	// Final trace inspection
	t.Logf("Final error stack trace:\n%s", wrapped.Stack())

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
		"Error layer: top level wrapper",
		"Error layer: nested lua wrapper",
		"Error layer: original error",
		"Lua Stack:",
		"Go Stack:",
		"final_go_func",
		"create_nested_state",
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
		if current.context != "" {
			contexts = append(contexts, current.context)
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
				t.Errorf("wrong context at position %d: expected %q, got %q", i, expected, contexts[i])
			}
		}
	}
}
