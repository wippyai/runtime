package engine

import (
	"fmt"
	"strings"
	"testing"

	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

func TestVM_InvalidLibraryLoading(t *testing.T) {
	logger := zap.NewNop()

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
}

func TestVM_WithPreloaded(t *testing.T) {
	logger := zap.NewNop()

	t.Run("successful preload", func(t *testing.T) {
		// Spawn a simple loader that returns a table
		loader := func(l *lua.LState) int {
			tab := l.NewTable()
			l.SetField(tab, "test", lua.LString("value"))
			l.Push(tab)
			return 1
		}

		vm, err := NewVM(logger, WithPreloaded("testmod", loader))
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		defer vm.Close()

		// Verify the module is available as a global
		if err := vm.state.DoString(`
			if type(testmod) ~= "table" then
				error("testmod should be a table")
			end
			if testmod.test ~= "value" then
				error("testmod.test should be 'value'")
			end
		`); err != nil {
			t.Errorf("module verification failed: %v", err)
		}
	})

	t.Run("preload with multiple return values", func(t *testing.T) {
		// Loader that returns multiple values
		loader := func(l *lua.LState) int {
			// In actual usage, WithPreloaded only uses the last returned value
			tab := l.NewTable()
			l.SetField(tab, "first", lua.LString("one"))
			l.Push(tab)

			tab2 := l.NewTable()
			l.SetField(tab2, "second", lua.LString("two"))
			l.Push(tab2)
			return 2
		}

		vm, err := NewVM(logger, WithPreloaded("multimod", loader))
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		defer vm.Close()

		// The last value pushed should be set as the global
		if err := vm.state.DoString(`
			if type(multimod) ~= "table" or multimod.second ~= "two" then
				error("last return value not set correctly")
			end
		`); err != nil {
			t.Errorf("module verification failed: %v", err)
		}
	})

	t.Run("preload with no return value", func(t *testing.T) {
		// Loader that returns nothing
		loader := func(_ *lua.LState) int {
			return 0
		}

		vm, err := NewVM(logger, WithPreloaded("emptymod", loader))
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		defer vm.Close()

		// Verify no global was set
		if err := vm.state.DoString(`
			if emptymod ~= nil then
				error("emptymod should not be defined")
			end
		`); err != nil {
			t.Errorf("module verification failed: %v", err)
		}
	})

	t.Run("preload with error", func(t *testing.T) {
		// Loader that raises an error
		loader := func(l *lua.LState) int {
			l.RaiseError("intentional loader error")
			return 0
		}

		vm, err := NewVM(logger, WithPreloaded("errormod", loader))
		if err == nil {
			t.Error("expected error, got nil")
			vm.Close()
			return
		}

		// Verify error message
		if !strings.Contains(err.Error(), "preload errormod failed") ||
			!strings.Contains(err.Error(), "intentional loader error") {
			t.Errorf("unexpected error message: %v", err)
		}

		if vm != nil {
			vm.Close()
		}
	})

	t.Run("multiple preloaded modules", func(t *testing.T) {
		loader1 := func(l *lua.LState) int {
			l.Push(lua.LNumber(42))
			return 1
		}

		loader2 := func(l *lua.LState) int {
			l.Push(lua.LString("hello"))
			return 1
		}

		vm, err := NewVM(logger,
			WithPreloaded("mod1", loader1),
			WithPreloaded("mod2", loader2))
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		defer vm.Close()

		// Verify both modules are available
		if err := vm.state.DoString(`
			if mod1 ~= 42 then
				error("mod1 should be 42")
			end
			if mod2 ~= "hello" then
				error("mod2 should be 'hello'")
			end
		`); err != nil {
			t.Errorf("module verification failed: %v", err)
		}
	})

	t.Run("preload with state modification", func(t *testing.T) {
		// Loader that modifies state beyond just returning values
		loader := func(l *lua.LState) int {
			// Set global directly
			l.SetGlobal("direct_global", lua.LString("direct"))

			// Return a value to be set as module
			l.Push(lua.LString("return_value"))
			return 1
		}

		vm, err := NewVM(logger, WithPreloaded("modifymod", loader))
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		defer vm.Close()

		// Verify both the direct global and returned module value
		if err := vm.state.DoString(`
			if direct_global ~= "direct" then
				error("direct_global not set correctly")
			end
			if modifymod ~= "return_value" then
				error("modifymod not set correctly")
			end
		`); err != nil {
			t.Errorf("state verification failed: %v", err)
		}
	})
}

// Helper function to check if functions appear in the stack trace in the correct order
//
//nolint:unused
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
