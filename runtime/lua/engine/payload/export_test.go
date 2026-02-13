package payload

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/payload"
)

// Helper function to create nested tables for testing
func createNestedTable(l *lua.LState, depth int) *lua.LTable {
	if depth <= 0 {
		return nil
	}

	tbl := l.NewTable()
	l.SetTable(tbl, lua.LString("value"), lua.LNumber(depth))

	if depth > 1 {
		nested := createNestedTable(l, depth-1)
		l.SetTable(tbl, lua.LString("nested"), nested)
	}

	return tbl
}

func TestExportLuaValue(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	t.Run("Basic types export", func(t *testing.T) {
		tests := []struct {
			input    lua.LValue
			expected lua.LValue
			name     string
		}{
			{lua.LNil, lua.LNil, "Nil"},
			{lua.LBool(true), lua.LBool(true), "Boolean true"},
			{lua.LBool(false), lua.LBool(false), "Boolean false"},
			{lua.LNumber(42.5), lua.LNumber(42.5), "Number"},
			{lua.LString("hello"), lua.LString("hello"), "String"},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				result := ExportPayload(tt.input)
				assert.Equal(t, payload.Lua, result.Format())

				resultValue, ok := result.Data().(lua.LValue)
				assert.True(t, ok, "Result data is not a lua.LValue")
				assert.Equal(t, tt.expected, resultValue)
			})
		}
	})

	t.Run("Table export", func(t *testing.T) {
		// Create a test table with both array and hash parts
		tbl := l.NewTable()
		// Array part
		l.SetTable(tbl, lua.LNumber(1), lua.LString("one"))
		l.SetTable(tbl, lua.LNumber(2), lua.LNumber(2))
		l.SetTable(tbl, lua.LNumber(3), lua.LBool(true))
		// Hash part
		l.SetTable(tbl, lua.LString("name"), lua.LString("test"))
		l.SetTable(tbl, lua.LString("nested"), func() *lua.LTable {
			nested := l.NewTable()
			l.SetTable(nested, lua.LString("key"), lua.LString("value"))
			return nested
		}())

		result := ExportPayload(tbl)
		assert.Equal(t, payload.Lua, result.Format())

		resultTable, ok := result.Data().(*lua.LTable)
		assert.True(t, ok, "Result data is not a *lua.LTable")

		// Verify that the table is mutable (deep copy creates new mutable table)
		assert.False(t, resultTable.Immutable, "Deep copied table should be mutable")

		// Original table should remain unchanged and mutable
		assert.False(t, tbl.Immutable, "Original table should remain mutable")

		// Result should be a different object (deep copy)
		assert.NotSame(t, tbl, resultTable, "Result should be a deep copy, not the same object")

		// Verify array part
		assert.Equal(t, lua.LString("one"), resultTable.RawGetInt(1))
		assert.Equal(t, lua.LNumber(2), resultTable.RawGetInt(2))
		assert.Equal(t, lua.LBool(true), resultTable.RawGetInt(3))

		// Verify hash part
		assert.Equal(t, lua.LString("test"), resultTable.RawGetString("name"))

		// Verify nested table is also mutable
		nestedResult, ok := resultTable.RawGetString("nested").(*lua.LTable)
		assert.True(t, ok, "Nested table should exist")
		assert.False(t, nestedResult.Immutable, "Nested table should be mutable")
		assert.Equal(t, lua.LString("value"), nestedResult.RawGetString("key"))
	})

	t.Run("Mixed table with sparse array", func(t *testing.T) {
		// Create a sparse array table
		tbl := l.NewTable()
		l.SetTable(tbl, lua.LNumber(1), lua.LString("one"))
		// Skip index 2
		l.SetTable(tbl, lua.LNumber(3), lua.LString("three"))
		l.SetTable(tbl, lua.LNumber(10), lua.LString("ten")) // Will be treated as hash part
		l.SetTable(tbl, lua.LString("key"), lua.LString("value"))

		result := ExportPayload(tbl)
		resultTable, _ := result.Data().(*lua.LTable)

		// Check values
		assert.Equal(t, lua.LString("one"), resultTable.RawGetInt(1))
		assert.Equal(t, lua.LNil, resultTable.RawGetInt(2), "Sparse index should remain nil")
		assert.Equal(t, lua.LString("three"), resultTable.RawGetInt(3))
		assert.Equal(t, lua.LString("ten"), resultTable.RawGetInt(10))
		assert.Equal(t, lua.LString("value"), resultTable.RawGetString("key"))

		// Verify mutability of the copy
		assert.False(t, resultTable.Immutable, "Deep copied table should be mutable")

		// Original should remain mutable
		assert.False(t, tbl.Immutable, "Original table should remain mutable")
	})

	t.Run("Deeply nested tables", func(t *testing.T) {
		original := createNestedTable(l, 5)
		result := ExportPayload(original)
		resultTable, _ := result.Data().(*lua.LTable)

		// Check the deep nesting and mutability
		current := resultTable
		for depth := 5; depth > 0; depth-- {
			assert.False(t, current.Immutable, "Deep copied table at depth %d should be mutable", depth)
			assert.Equal(t, lua.LNumber(depth), current.RawGetString("value"))

			if depth > 1 {
				next, ok := current.RawGetString("nested").(*lua.LTable)
				assert.True(t, ok, "Expected nested table at depth %d", depth)
				current = next
			}
		}

		// Original should remain mutable
		assert.False(t, original.Immutable, "Original table should remain mutable")
	})

	t.Run("Userdata handling", func(t *testing.T) {
		// Create userdata
		ud := l.NewUserData()
		ud.Value = "some go value" // Any Go value

		result := ExportPayload(ud)
		resultValue, _ := result.Data().(lua.LValue)

		// Userdata should be replaced with nil
		assert.Equal(t, lua.LNil, resultValue)
	})

	t.Run("Table with userdata", func(t *testing.T) {
		// Create a table with a mix of regular values and userdata
		tbl := l.NewTable()
		l.SetTable(tbl, lua.LString("regular"), lua.LString("value"))
		l.SetTable(tbl, lua.LString("userdata"), l.NewUserData())

		result := ExportPayload(tbl)
		resultTable, _ := result.Data().(*lua.LTable)

		// Regular value should be preserved
		assert.Equal(t, lua.LString("value"), resultTable.RawGetString("regular"))

		// Userdata should be replaced with nil
		assert.Equal(t, lua.LNil, resultTable.RawGetString("userdata"))

		// Deep copied table should be mutable
		assert.False(t, resultTable.Immutable, "Deep copied table should be mutable")
	})

	t.Run("Large table performance", func(t *testing.T) {
		// Create a large table to test performance impact of optimizations
		largeTable := l.NewTable()

		// Add a large array part (1000 elements)
		for i := 1; i <= 1000; i++ {
			l.SetTable(largeTable, lua.LNumber(i), lua.LNumber(i))
		}

		// Add a large hash part (1000 string keys)
		for i := 1; i <= 1000; i++ {
			key := lua.LString(fmt.Sprintf("key_%d", i))
			l.SetTable(largeTable, key, lua.LNumber(i))
		}

		result := ExportPayload(largeTable)
		resultTable, _ := result.Data().(*lua.LTable)

		// Check a few values to ensure correctness
		assert.Equal(t, lua.LNumber(500), resultTable.RawGetInt(500))
		assert.Equal(t, lua.LNumber(1000), resultTable.RawGetInt(1000))
		assert.Equal(t, lua.LNumber(750), resultTable.RawGetString("key_750"))

		// Verify mutability
		assert.False(t, resultTable.Immutable, "Deep copied large table should be mutable")
	})

	t.Run("Immutability enforcement", func(t *testing.T) {
		// Create a table and export it
		tbl := l.NewTable()
		l.SetTable(tbl, lua.LString("key"), lua.LString("value"))

		result := ExportPayload(tbl)
		resultTable, _ := result.Data().(*lua.LTable)

		// Verify the table is mutable (deep copy)
		assert.False(t, resultTable.Immutable, "Deep copied table should be mutable")

		// Verify that we can modify the deep copied table
		success := resultTable.RawSetString("new_key", lua.LString("new_value"))
		assert.True(t, success, "Setting new value on mutable table should succeed")

		// New value should be accessible
		assert.Equal(t, lua.LString("new_value"), resultTable.RawGetString("new_key"))

		// Original value should be unchanged
		assert.Equal(t, lua.LString("value"), resultTable.RawGetString("key"))
	})

	t.Run("Nested userdata cleanup", func(t *testing.T) {
		// Create a deeply nested structure with userdata at various levels
		tbl := l.NewTable()
		l.SetTable(tbl, lua.LString("normal"), lua.LString("value"))
		l.SetTable(tbl, lua.LString("userdata"), l.NewUserData())

		nested := l.NewTable()
		l.SetTable(nested, lua.LString("nested_normal"), lua.LNumber(42))
		l.SetTable(nested, lua.LString("nested_userdata"), l.NewUserData())
		l.SetTable(tbl, lua.LString("nested"), nested)

		result := ExportPayload(tbl)
		resultTable, _ := result.Data().(*lua.LTable)

		// Check that userdata was cleared at all levels
		assert.Equal(t, lua.LString("value"), resultTable.RawGetString("normal"))
		assert.Equal(t, lua.LNil, resultTable.RawGetString("userdata"))

		nestedResult, _ := resultTable.RawGetString("nested").(*lua.LTable)
		assert.Equal(t, lua.LNumber(42), nestedResult.RawGetString("nested_normal"))
		assert.Equal(t, lua.LNil, nestedResult.RawGetString("nested_userdata"))

		// All deep copied tables should be mutable
		assert.False(t, resultTable.Immutable)
		assert.False(t, nestedResult.Immutable)
	})

	// --- NEW TEST CASES TO PROVE THE RECURSION BUG ---

	t.Run("Circular reference (self)", func(t *testing.T) {
		// This test will hang and crash on the original buggy code.
		// It will pass on the fixed code.
		tbl := l.NewTable()
		l.SetTable(tbl, lua.LString("name"), lua.LString("self_referential"))
		l.SetTable(tbl, lua.LString("self"), tbl) // Circular reference

		// The ExportPayload function should not panic or hang
		var result payload.Payload
		assert.NotPanics(t, func() {
			result = ExportPayload(tbl)
		}, "Exporting a self-referential table should not panic")

		// Verify the structure of the copied table
		resultTable, ok := result.Data().(*lua.LTable)
		assert.True(t, ok, "Result should be a table")
		assert.NotSame(t, tbl, resultTable, "The copy should be a new object")

		assert.Equal(t, lua.LString("self_referential"), resultTable.RawGetString("name"))

		// Check that the circular reference was correctly recreated in the copy
		selfRef, ok := resultTable.RawGetString("self").(*lua.LTable)
		assert.True(t, ok, "The 'self' reference in the copy should be a table")
		assert.Same(t, resultTable, selfRef, "The 'self' reference should point to the new copied table, not the original")
	})

	t.Run("Circular reference (mutual)", func(t *testing.T) {
		// This test will also hang and crash on the original buggy code.
		// It will pass on the fixed code.
		tableA := l.NewTable()
		tableB := l.NewTable()

		l.SetTable(tableA, lua.LString("name"), lua.LString("A"))
		l.SetTable(tableA, lua.LString("link"), tableB)

		l.SetTable(tableB, lua.LString("name"), lua.LString("B"))
		l.SetTable(tableB, lua.LString("link"), tableA)

		// The ExportPayload function should not panic or hang
		var resultA payload.Payload
		assert.NotPanics(t, func() {
			resultA = ExportPayload(tableA)
		}, "Exporting a mutually-referential table should not panic")

		// Verify the structure of the copied tables
		copiedA, ok := resultA.Data().(*lua.LTable)
		assert.True(t, ok, "Result should be a table")
		assert.NotSame(t, tableA, copiedA, "Copied table A should be a new object")
		assert.Equal(t, lua.LString("A"), copiedA.RawGetString("name"))

		copiedB, ok := copiedA.RawGetString("link").(*lua.LTable)
		assert.True(t, ok, "Link from copied A should point to a table")
		assert.NotSame(t, tableB, copiedB, "Copied table B should be a new object")
		assert.Equal(t, lua.LString("B"), copiedB.RawGetString("name"))

		// Check that the cycle is correctly closed in the new objects
		linkBackToA, ok := copiedB.RawGetString("link").(*lua.LTable)
		assert.True(t, ok, "Link from copied B should point to a table")
		assert.Same(t, copiedA, linkBackToA, "The link from B should point back to the new copied A")
	})
}

// Test the internal makeTableImmutableRecursive function directly
func TestMakeTableImmutableRecursive(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	t.Run("Basic table immutability", func(t *testing.T) {
		tbl := l.NewTable()
		l.SetTable(tbl, lua.LString("key"), lua.LString("value"))
		l.SetTable(tbl, lua.LNumber(1), lua.LNumber(42))

		visited := make(map[*lua.LTable]bool)
		result := makeTableImmutableRecursive(tbl, visited)

		resultTbl, ok := result.(*lua.LTable)
		assert.True(t, ok)
		assert.True(t, resultTbl.Immutable)
		assert.Same(t, tbl, resultTbl)
	})

	t.Run("Nested table immutability", func(t *testing.T) {
		outer := l.NewTable()
		inner := l.NewTable()
		l.SetTable(inner, lua.LString("inner_key"), lua.LString("inner_value"))
		l.SetTable(outer, lua.LString("nested"), inner)

		visited := make(map[*lua.LTable]bool)
		result := makeTableImmutableRecursive(outer, visited)

		outerTbl := result.(*lua.LTable)
		assert.True(t, outerTbl.Immutable)

		innerTbl := outerTbl.RawGetString("nested").(*lua.LTable)
		assert.True(t, innerTbl.Immutable)
	})

	t.Run("Circular reference handling", func(t *testing.T) {
		tbl := l.NewTable()
		l.SetTable(tbl, lua.LString("self"), tbl)

		visited := make(map[*lua.LTable]bool)
		assert.NotPanics(t, func() {
			makeTableImmutableRecursive(tbl, visited)
		})
		assert.True(t, tbl.Immutable)
	})

	t.Run("Non-table values pass through", func(t *testing.T) {
		visited := make(map[*lua.LTable]bool)

		assert.Equal(t, lua.LNil, makeTableImmutableRecursive(lua.LNil, visited))
		assert.Equal(t, lua.LNumber(42), makeTableImmutableRecursive(lua.LNumber(42), visited))
		assert.Equal(t, lua.LString("test"), makeTableImmutableRecursive(lua.LString("test"), visited))
		assert.Equal(t, lua.LBool(true), makeTableImmutableRecursive(lua.LBool(true), visited))
	})

	t.Run("Userdata with nil value", func(t *testing.T) {
		ud := l.NewUserData()
		ud.Value = nil

		visited := make(map[*lua.LTable]bool)
		result := makeTableImmutableRecursive(ud, visited)
		assert.Equal(t, lua.LNil, result)
	})

	t.Run("Userdata with error value", func(t *testing.T) {
		ud := l.NewUserData()
		ud.Value = fmt.Errorf("test error")

		visited := make(map[*lua.LTable]bool)
		result := makeTableImmutableRecursive(ud, visited)
		luaErr, ok := result.(*lua.Error)
		assert.True(t, ok, "expected structured error")
		assert.Contains(t, luaErr.Error(), "test error")
	})

	t.Run("Userdata with non-error value", func(t *testing.T) {
		ud := l.NewUserData()
		ud.Value = "some value"

		visited := make(map[*lua.LTable]bool)
		result := makeTableImmutableRecursive(ud, visited)
		assert.Equal(t, lua.LNil, result)
	})
}

// Test the processAndImmutabilize function directly
func TestProcessAndImmutabilize(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	t.Run("Basic value", func(t *testing.T) {
		result := processAndImmutabilize(lua.LNumber(42))
		assert.Equal(t, lua.LNumber(42), result)
	})

	t.Run("Table becomes immutable", func(t *testing.T) {
		tbl := l.NewTable()
		l.SetTable(tbl, lua.LString("key"), lua.LString("value"))

		result := processAndImmutabilize(tbl)
		resultTbl := result.(*lua.LTable)
		assert.True(t, resultTbl.Immutable)
	})

	t.Run("Nested tables become immutable", func(t *testing.T) {
		outer := l.NewTable()
		inner := l.NewTable()
		l.SetTable(inner, lua.LString("data"), lua.LNumber(123))
		l.SetTable(outer, lua.LString("child"), inner)

		result := processAndImmutabilize(outer)
		outerTbl := result.(*lua.LTable)
		innerTbl := outerTbl.RawGetString("child").(*lua.LTable)

		assert.True(t, outerTbl.Immutable)
		assert.True(t, innerTbl.Immutable)
	})
}
