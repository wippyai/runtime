package engine

import (
	"fmt"
	"go.uber.org/zap"
	"strings"
	"testing"
)

func TestVM_InvalidLibraryLoading(t *testing.T) {
	logger := zap.NewNop()

	t.Run("library with syntax error", func(t *testing.T) {
		libSource := `
			local lib = {
				-- Missing closing brace
			return lib
		`
		vm, err := NewVM(logger, WithLibrary("badlib", libSource))
		if err == nil {
			t.Error("expected error for library with syntax error, got nil")
		} else if !strings.Contains(strings.ToLower(err.Error()), "failed to load library") {
			t.Errorf("expected load error message, got: %v", err)
		}
		if vm != nil {
			vm.Close()
		}
	})

	t.Run("library with runtime error", func(t *testing.T) {
		libSource := `
			error("boom")
			local lib = {}
			return lib
		`
		vm, err := NewVM(logger, WithLibrary("badlib", libSource))
		if err == nil {
			t.Error("expected error for library with runtime error, got nil")
		} else if !strings.Contains(strings.ToLower(err.Error()), "failed to initialize library") {
			t.Errorf("expected initialization error message, got: %v", err)
		}
		if vm != nil {
			vm.Close()
		}
	})

	t.Run("library returning non-table", func(t *testing.T) {
		libSource := `
			return "not a table"
		`
		vm, err := NewVM(logger, WithLibrary("badlib", libSource))
		if err == nil {
			t.Error("expected error for library returning non-table, got nil")
		} else if !strings.Contains(err.Error(), "must return a table") {
			t.Errorf("expected 'must return a table' error message, got: %v", err)
		}
		if vm != nil {
			vm.Close()
		}
	})

	t.Run("library with invalid prototype", func(t *testing.T) {
		vm, err := NewVM(logger, WithLibrary("badlib", nil))
		if err == nil {
			t.Error("expected error for nil prototype, got nil")
		} else if !strings.Contains(err.Error(), "invalid source type") {
			t.Errorf("expected 'invalid source type' error message, got: %v", err)
		}
		if vm != nil {
			vm.Close()
		}
	})

	t.Run("library with empty source", func(t *testing.T) {
		vm, err := NewVM(logger, WithLibrary("badlib", ""))
		if err == nil {
			t.Error("expected error for empty source, got nil")
		} else if !strings.Contains(err.Error(), "source cannot be empty") {
			t.Errorf("expected 'source cannot be empty' error message, got: %v", err)
		}
		if vm != nil {
			vm.Close()
		}
	})

	t.Run("library with invalid name", func(t *testing.T) {
		libSource := `
			local lib = {}
			return lib
		`
		invalidNames := []string{
			"",                       // Empty name
			"invalid/name",           // Contains path separator
			"invalid\\name",          // Contains backslash
			"invalid name",           // Contains space
			"invalid.name",           // Contains dot
			"invalid*name",           // Contains wildcard
			strings.Repeat("a", 129), // Too long name
		}

		for _, name := range invalidNames {
			t.Run(fmt.Sprintf("invalid name: %q", name), func(t *testing.T) {
				vm, err := NewVM(logger, WithLibrary(name, libSource))
				if err == nil {
					t.Errorf("expected error for invalid library name %q, got nil", name)
				}
				if vm != nil {
					vm.Close()
				}
			})
		}
	})

	t.Run("library with circular dependency", func(t *testing.T) {
		lib1Source := `
			require("lib2")  -- This will fail because lib2 tries to require lib1
			local lib = {}
			return lib
		`
		lib2Source := `
			require("lib1")  -- This creates circular dependency
			local lib = {}
			return lib
		`
		vm, err := NewVM(logger,
			WithLibrary("lib1", lib1Source),
			WithLibrary("lib2", lib2Source))
		if err == nil {
			t.Error("expected error for circular dependency, got nil")
		} else if !strings.Contains(strings.ToLower(err.Error()), "initialize library") {
			t.Errorf("expected initialization error message, got: %v", err)
		}
		if vm != nil {
			vm.Close()
		}
	})

	t.Run("multiple library errors", func(t *testing.T) {
		vm, err := NewVM(logger,
			WithLibrary("lib1", "return 123"),     // Returns non-table
			WithLibrary("lib2", "invalid{syntax"), // Syntax error
			WithLibrary("lib3", "error('boom')"))  // Runtime error

		if err == nil {
			t.Error("expected error for multiple invalid libraries, got nil")
		} else {
			errStr := err.Error()
			if !strings.Contains(errStr, "must return a table") ||
				!strings.Contains(strings.ToLower(errStr), "failed to load") ||
				!strings.Contains(strings.ToLower(errStr), "failed to initialize") {
				t.Errorf("expected multiple error messages, got: %v", err)
			}
		}
		if vm != nil {
			vm.Close()
		}
	})
}

func TestVM_LibraryStackTrace(t *testing.T) {
	logger := zap.NewNop()

	t.Run("library with runtime error and stack trace", func(t *testing.T) {
		libSource := `
			local lib = {}
			
			function lib.init()
				error("something went wrong")  -- This is line 5
				return true
			end
			
			lib.init()  -- Call the function that errors
			return lib
		`
		vm, err := NewVM(logger, WithLibrary("errorlib", libSource))
		if err == nil {
			t.Error("expected error with stack trace, got nil")
		} else {
			errStr := err.Error()

			// Check basic error info
			if !strings.Contains(errStr, "VM initialization errors: failed to initialize library 'errorlib': <errorlib>:5: something went wrong") {
				t.Error("error should contain initialization error message")
			}

			// Take the first error section (before ; library)
			firstErrorSection := strings.Split(errStr, "; library")[0]
			traces := strings.Split(firstErrorSection, "stack traceback:")

			if len(traces) < 2 {
				t.Error("first error section should contain stack trace")
			} else {
				trace := traces[1]
				traceLines := strings.Split(trace, "\n")

				// Expected sequence of function calls in order
				expectedCalls := []string{
					"[G]: in function 'error'",
					"<errorlib>:5: in function 'init'",
					`<errorlib>:9: in function <<errorlib>:0>`,
					"[G]: in function (anonymous)",
					"[G]: in function 'require'",
					"<string>:1: in main chunk",
				}

				// Check each expected call appears in order
				lastIndex := -1
				for _, expectedCall := range expectedCalls {
					found := false
					for i, line := range traceLines {
						if strings.TrimSpace(line) == expectedCall {
							if i <= lastIndex {
								t.Errorf("call %q found out of order", expectedCall)
							}
							lastIndex = i
							found = true
							break
						}
					}
					if !found {
						t.Errorf("expected call %q not found in trace", expectedCall)
					}
				}
			}
		}
		if vm != nil {
			vm.Close()
		}
	})

	t.Run("library with nested call stack error", func(t *testing.T) {
		libSource := `
			local lib = {}
			
			function lib.deep()
				error("deep error occurred")
			end
			
			function lib.middle()
				lib.deep()
			end
			
			function lib.top()
				lib.middle()
			end
			
			lib.top()  -- Start the chain of calls
			return lib
		`
		vm, err := NewVM(logger, WithLibrary("stacklib", libSource))
		if err == nil {
			t.Error("expected error with nested stack trace, got nil")
		} else {
			errStr := err.Error()

			// Check for functions in the correct order
			if !matchStackOrder(errStr, []string{"deep", "middle", "top"}) {
				t.Error("stack trace should show functions in the correct order (deep -> middle -> top)")
			}

			// Check file:line format
			if !strings.Contains(errStr, "<stacklib>:") {
				t.Error("error should contain file reference '<stacklib>:'")
			}

			if !strings.Contains(errStr, "deep error occurred") {
				t.Error("error should contain the message 'deep error occurred'")
			}
		}
		if vm != nil {
			vm.Close()
		}
	})

	t.Run("library with syntax error showing position", func(t *testing.T) {
		libSource := `
			local lib = {}
			
			function lib.test()
				if true then  -- Missing 'end' for this if
				return lib
		` // Intentionally malformed
		vm, err := NewVM(logger, WithLibrary("syntaxlib", libSource))
		if err == nil {
			t.Error("expected syntax error with position, got nil")
		} else {
			errStr := err.Error()

			if !strings.Contains(strings.ToLower(errStr), "syntax error") {
				t.Error("error should mention 'syntax error'")
			}

			if !strings.Contains(errStr, "<syntaxlib>") {
				t.Error("error should contain file reference '<syntaxlib>'")
			}
		}
		if vm != nil {
			vm.Close()
		}
	})

	t.Run("error in require statement with location", func(t *testing.T) {
		libSource := `
			local lib = {}
			
			function lib.test()
				require('nonexistent_module')  -- This should fail with location info
			end
			
			lib.test()
			return lib
		`
		vm, err := NewVM(logger, WithLibrary("requirelib", libSource))
		if err == nil {
			t.Error("expected require error with location, got nil")
		} else {
			errStr := err.Error()

			if !strings.Contains(errStr, "module nonexistent_module not found") {
				t.Error("error should mention module not found")
			}

			if !strings.Contains(errStr, "<requirelib>:") {
				t.Error("error should contain file reference '<requirelib>:'")
			}

			if !strings.Contains(errStr, "in function 'test'") {
				t.Error("error should show it occurred in test function")
			}
		}
		if vm != nil {
			vm.Close()
		}
	})
}

// Helper function to check if functions appear in the stack trace in the correct order
func matchStackOrder(stackTrace string, functionNames []string) bool {
	lastPos := -1
	for _, fname := range functionNames {
		pos := strings.Index(stackTrace, fmt.Sprintf("in function '%s'", fname))
		if pos == -1 {
			return false
		}
		if pos <= lastPos {
			return false
		}
		lastPos = pos
	}
	return true
}
