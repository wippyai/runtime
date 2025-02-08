package engine

import (
	"errors"
	"fmt"
	"log"
	"runtime"
	"testing"

	lua "github.com/yuin/gopher-lua"
)

// Test helpers
func runLua(t *testing.T, script string) (*lua.LState, error) {
	L := lua.NewState()
	defer L.Close()

	// Register our error type
	RegisterErrorType(L)

	// Register test functions
	L.SetGlobal("fail_immediate", L.NewFunction(testFailImmediate))
	L.SetGlobal("fail_return", L.NewFunction(testFailReturn))
	L.SetGlobal("get_error_type", L.NewFunction(testGetErrorType))

	return L, L.DoString(script)
}

// Test function that immediately raises error
func testFailImmediate(L *lua.LState) int {
	err := errors.New("immediate failure")
	RaiseError(L, err)
	return 0
}

// Test function that returns nil, error
func testFailReturn(L *lua.LState) int {
	err := errors.New("returned failure")
	L.Push(lua.LNil)
	L.Push(WrapError(L, err))
	return 2
}

// Helper to check error type
func testGetErrorType(L *lua.LState) int {
	err := GetGoError(L, 1)
	if err != nil {
		L.Push(lua.LString("is_go_error"))
	} else {
		L.Push(lua.LString("not_go_error"))
	}
	return 1
}

func TestErrorWrapper(t *testing.T) {
	// Test 1: Immediate error raising
	t.Run("immediate error", func(t *testing.T) {
		script := `
			local ok, err = pcall(fail_immediate)
			assert(not ok)  -- Should be false
			assert(type(err) == "userdata")  -- Our error wrapper
			assert(tostring(err) == "immediate failure")  -- String conversion
			assert(get_error_type(err) == "is_go_error")  -- Can get Go error
		`
		_, err := runLua(t, script)
		if err != nil {
			t.Errorf("test failed: %v", err)
		}
	})

	// Test 2: Returned error
	t.Run("returned error", func(t *testing.T) {
		script := `
			local ok, err = fail_return()
			assert(not ok)  -- Should be nil
			assert(type(err) == "userdata")
			assert(tostring(err) == "returned failure")
			assert(get_error_type(err) == "is_go_error")
		`
		_, err := runLua(t, script)
		if err != nil {
			t.Errorf("test failed: %v", err)
		}
	})

	// Test 3: Error propagation
	t.Run("error propagation", func(t *testing.T) {
		script := `
			local function wrap_error()
				local ok, err = fail_return()
				if not ok then
					error(err)  -- Propagate our error
				end
			end

			local ok, err = pcall(wrap_error)
			assert(not ok)
			assert(type(err) == "userdata")
			assert(tostring(err) == "returned failure")
			assert(get_error_type(err) == "is_go_error")
		`
		_, err := runLua(t, script)
		if err != nil {
			t.Errorf("test failed: %v", err)
		}
	})

	// Test 4: Error in pcall handler
	t.Run("error handler", func(t *testing.T) {
		script := `
			local function error_handler(err)
				-- Handler receives our error object
				assert(type(err) == "userdata")
				assert(get_error_type(err) == "is_go_error")
				return "handled: " .. tostring(err)
			end

			local ok, result = xpcall(fail_immediate, error_handler)
			assert(not ok)
			assert(result == "handled: immediate failure")
		`
		_, err := runLua(t, script)
		if err != nil {
			t.Errorf("test failed: %v", err)
		}
	})

	// Test 5: Normal Lua errors still work
	t.Run("normal lua error", func(t *testing.T) {
		script := `
			local ok, err = pcall(function()
				error("normal lua error")
			end)
			assert(not ok)
			assert(type(err) == "string")
			assert(get_error_type(err) == "not_go_error")
		`
		_, err := runLua(t, script)
		if err != nil {
			t.Errorf("test failed: %v", err)
		}
	})
}

// Custom error type to store additional info
type validationError struct {
	Field   string
	Message string
	Stack   string
}

func (e *validationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

// Example validation function that we'll bind to Lua
func validateUser(L *lua.LState) int {
	username := L.CheckString(1)
	age := L.CheckNumber(2)

	// Helper to get stack info
	getStack := func() string {
		if d, ok := L.GetStack(0); ok {
			//return d.String()
			log.Printf("stack: %+v", d)
			return fmt.Sprintf("%s:%d", d.What, d.CurrentLine)
		}
		return "no stack available"
	}

	if len(username) < 3 {
		err := &validationError{
			Field:   "username",
			Message: "too short",
			Stack:   getStack(),
		}
		L.Push(lua.LNil)
		L.Push(WrapError(L, err))
		return 2
	}

	if age < 18 {
		err := &validationError{
			Field:   "age",
			Message: "must be 18 or older",
			Stack:   getStack(),
		}
		RaiseError(L, err)
		return 0
	}

	L.Push(lua.LTrue)
	return 1
}

func TestErrorPropagation(t *testing.T) {
	t.Run("error retrieval", func(t *testing.T) {
		L := lua.NewState()
		defer L.Close()

		RegisterErrorType(L)
		L.SetGlobal("validate_user", L.NewFunction(validateUser))

		// Create test script
		script := `
			function create_user(name, age)
				local ok, err = validate_user(name, age)
				if not ok then
					error(err)
				end
				return true
			end
			
			-- Call with invalid username
			return create_user("x", 20)
		`

		// Run script and expect error
		err := L.DoString(script)
		if err == nil {
			t.Fatal("expected error, got nil")
		}

		// Try to extract our validation error
		var valErr *validationError
		if apiErr, ok := err.(*lua.ApiError); ok {
			if ud, ok := apiErr.Object.(*lua.LUserData); ok {
				if ve, ok := ud.Value.(*ErrorWrapper).Err.(*validationError); ok {
					valErr = ve
					log.Printf("stack: %s", ve.Error())
				}
			}
		}

		// Verify we got our error with all data
		if valErr == nil {
			t.Fatal("failed to retrieve validationError")
		}

		// Check error fields
		if valErr.Field != "username" {
			t.Errorf("expected field 'username', got '%s'", valErr.Field)
		}
		if valErr.Message != "too short" {
			t.Errorf("expected message 'too short', got '%s'", valErr.Message)
		}
		if valErr.Stack == "" {
			t.Error("expected non-empty stack trace")
		}
	})

	t.Run("immediate error", func(t *testing.T) {
		L := lua.NewState()
		defer L.Close()

		RegisterErrorType(L)
		L.SetGlobal("validate_user", L.NewFunction(validateUser))

		// Test immediate error (age < 18)
		script := `return validate_user("john", 15)`

		err := L.DoString(script)
		if err == nil {
			t.Fatal("expected error, got nil")
		}

		var valErr *validationError
		if apiErr, ok := err.(*lua.ApiError); ok {
			if ud, ok := apiErr.Object.(*lua.LUserData); ok {
				if ve, ok := ud.Value.(*ErrorWrapper).Err.(*validationError); ok {
					valErr = ve
				}
			}
		}

		if valErr == nil {
			t.Fatal("failed to retrieve validationError")
		}

		if valErr.Field != "age" {
			t.Errorf("expected field 'age', got '%s'", valErr.Field)
		}
	})

	t.Run("success case", func(t *testing.T) {
		L := lua.NewState()
		defer L.Close()

		RegisterErrorType(L)
		L.SetGlobal("validate_user", L.NewFunction(validateUser))

		// Test valid input
		script := `return validate_user("john", 20)`

		err := L.DoString(script)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
}

// Enhanced error type that includes Go stack trace
type TraceableError struct {
	Msg      string
	GoStack  string
	LuaStack string
}

func (e *TraceableError) Error() string {
	return e.Msg
}

// Helper to capture Go stack when creating error
func newTraceableError(msg string) *TraceableError {
	// Capture Go stack
	buf := make([]byte, 4096)
	n := runtime.Stack(buf, false)

	return &TraceableError{
		Msg:     msg,
		GoStack: string(buf[:n]),
	}
}

// Function that generates error deep in Go call stack
func deepFunction() *TraceableError {
	return newTraceableError("deep error")
}

func middleFunction() *TraceableError {
	if err := deepFunction(); err != nil {
		// Note: not wrapping with fmt.Errorf as it breaks type assertion
		return err
	}
	return nil
}

// Lua-bound function that generates error
func errorGeneratingFunction(L *lua.LState) int {
	err := middleFunction()
	if err != nil {
		// Add Lua stack info before raising
		if d, ok := L.GetStack(0); ok {
			err.LuaStack = fmt.Sprintf("%s:%d", d.What, d.CurrentLine)
		}

		// Important: We use our error wrapper before raising
		errObj := WrapError(L, err)
		L.Error(errObj, 1)
		return 0
	}
	return 0
}

func TestErrorTracing(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	RegisterErrorType(L)
	L.SetGlobal("generate_error", L.NewFunction(errorGeneratingFunction))

	// Create some Lua call stack
	script := `
		function deep_lua()
			return generate_error()
		end

		function middle_lua()
			return deep_lua()
		end

		return middle_lua()
	`

	err := L.DoString(script)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	// Extract our traceable error
	var traceErr *TraceableError
	if apiErr, ok := err.(*lua.ApiError); ok {
		// Debug print to see what we're getting
		t.Logf("ApiError object type: %T\n", apiErr.Object)

		if ud, ok := apiErr.Object.(*lua.LUserData); ok {
			if wrapper, ok := ud.Value.(*ErrorWrapper); ok {
				if te, ok := wrapper.Err.(*TraceableError); ok {
					traceErr = te
				} else {
					t.Fatalf("wrapper.Err is of type %T, not TraceableError", wrapper.Err)
				}
			} else {
				t.Fatalf("userdata value is of type %T, not *ErrorWrapper", ud.Value)
			}
		} else {
			t.Fatalf("apiErr.Object is of type %T, not *lua.LUserData", apiErr.Object)
		}
	} else {
		t.Fatalf("error is of type %T, not *lua.ApiError", err)
	}

	if traceErr == nil {
		t.Fatal("failed to retrieve TraceableError")
	}

	// Verify we have both stacks
	t.Logf("\nError occurred with:\nGo Stack:\n%s\nLua Stack:\n%s",
		traceErr.GoStack,
		traceErr.LuaStack)

	// Verify presence of key functions in stack trace
	if traceErr.GoStack == "" {
		t.Error("Go stack trace is empty")
	}

	if traceErr.LuaStack == "" {
		t.Error("Lua stack trace is empty")
	}
}
