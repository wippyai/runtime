package lua

import (
	"fmt"
	"testing"

	"github.com/ponyruntime/pony/api/payload"
	"github.com/stretchr/testify/assert"
	lua "github.com/yuin/gopher-lua"
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
			name     string
			input    lua.LValue
			expected lua.LValue
		}{
			{"Nil", lua.LNil, lua.LNil},
			{"Boolean true", lua.LBool(true), lua.LBool(true)},
			{"Boolean false", lua.LBool(false), lua.LBool(false)},
			{"Number", lua.LNumber(42.5), lua.LNumber(42.5)},
			{"String", lua.LString("hello"), lua.LString("hello")},
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

		// Verify that the table is now immutable (same object, but immutable)
		assert.True(t, resultTable.Immutable, "Table should be immutable after export")
		assert.Same(t, tbl, resultTable, "Table should be the same object, made immutable in-place")

		// Verify array part
		assert.Equal(t, lua.LString("one"), resultTable.RawGetInt(1))
		assert.Equal(t, lua.LNumber(2), resultTable.RawGetInt(2))
		assert.Equal(t, lua.LBool(true), resultTable.RawGetInt(3))

		// Verify hash part
		assert.Equal(t, lua.LString("test"), resultTable.RawGetString("name"))

		// Verify nested table is also immutable
		nestedResult, ok := resultTable.RawGetString("nested").(*lua.LTable)
		assert.True(t, ok, "Nested table should exist")
		assert.True(t, nestedResult.Immutable, "Nested table should be immutable")
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

		// Verify immutability
		assert.True(t, resultTable.Immutable, "Table should be immutable after export")
	})

	t.Run("Deeply nested tables", func(t *testing.T) {
		original := createNestedTable(l, 5)
		result := ExportPayload(original)
		resultTable, _ := result.Data().(*lua.LTable)

		// Check the deep nesting and immutability
		current := resultTable
		for depth := 5; depth > 0; depth-- {
			assert.True(t, current.Immutable, "Table at depth %d should be immutable", depth)
			assert.Equal(t, lua.LNumber(depth), current.RawGetString("value"))

			if depth > 1 {
				next, ok := current.RawGetString("nested").(*lua.LTable)
				assert.True(t, ok, "Expected nested table at depth %d", depth)
				current = next
			}
		}
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

		// Table should be immutable
		assert.True(t, resultTable.Immutable, "Table should be immutable after export")
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

		// Verify immutability
		assert.True(t, resultTable.Immutable, "Large table should be immutable after export")
	})

	t.Run("Immutability enforcement", func(t *testing.T) {
		// Create a table and export it
		tbl := l.NewTable()
		l.SetTable(tbl, lua.LString("key"), lua.LString("value"))

		result := ExportPayload(tbl)
		resultTable, _ := result.Data().(*lua.LTable)

		// Verify the table is immutable
		assert.True(t, resultTable.Immutable, "Table should be immutable")

		// Verify that attempting to modify fails (using direct field access to avoid panic)
		success := resultTable.RawSetString("new_key", lua.LString("new_value"))
		assert.False(t, success, "Setting new value on immutable table should fail")

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

		// All tables should be immutable
		assert.True(t, resultTable.Immutable)
		assert.True(t, nestedResult.Immutable)
	})
}
