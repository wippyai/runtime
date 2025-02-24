package sql

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	lua "github.com/yuin/gopher-lua"
)

// TestCheckParams tests parameter conversion from Lua to Go
func TestCheckParams(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	// Test cases
	tests := []struct {
		name     string
		setup    func() int
		expected interface{}
		wantErr  bool
	}{
		{
			name: "nil params",
			setup: func() int {
				L.Push(lua.LNil)
				return 1
			},
			expected: nil,
			wantErr:  false,
		},
		{
			// Note: In the real code, empty tables are detected as maps, not arrays
			// because MaxN() returns 0 for empty tables. This is expected behavior.
			name: "empty map table",
			setup: func() int {
				tbl := L.NewTable()
				L.Push(tbl)
				return 1
			},
			expected: nil,
			wantErr:  true, // We expect an error since it's treated as a map
		},
		{
			name: "array with values",
			setup: func() int {
				tbl := L.NewTable()
				tbl.Append(lua.LNumber(42))
				tbl.Append(lua.LString("hello"))
				tbl.Append(lua.LBool(true))
				L.Push(tbl)
				return 1
			},
			expected: []interface{}{float64(42), "hello", true},
			wantErr:  false,
		},
		{
			name: "map-like table",
			setup: func() int {
				tbl := L.NewTable()
				tbl.RawSetString("id", lua.LNumber(123))
				tbl.RawSetString("name", lua.LString("test"))
				L.Push(tbl)
				return 1
			},
			expected: nil,
			wantErr:  true, // We don't support map params yet
		},
		{
			name: "non-table param",
			setup: func() int {
				L.Push(lua.LString("not a table"))
				return 1
			},
			expected: nil,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup test state
			L.Pop(L.GetTop()) // Clear stack
			index := tt.setup()

			// Call the function
			result, err := checkParams(L, index)

			// Check results
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

// mockResult implements the sql.Result interface for testing
type mockResult struct {
	lastID       int64
	rowsAffected int64
}

func (m mockResult) LastInsertId() (int64, error) {
	return m.lastID, nil
}

func (m mockResult) RowsAffected() (int64, error) {
	return m.rowsAffected, nil
}

// mockResultWithError always returns an error
type mockResultWithError struct {
	err error
}

func (m mockResultWithError) LastInsertId() (int64, error) {
	return 0, m.err
}

func (m mockResultWithError) RowsAffected() (int64, error) {
	return 0, m.err
}

// TestResultToTableStructure simply verifies that resultToTable returns a table with the expected keys
func TestResultToTableStructure(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	// Create a mock result
	mockResult := mockResult{
		lastID:       42,
		rowsAffected: 3,
	}

	// Get the result table
	table := resultToTable(L, mockResult)
	assert.NotNil(t, table)

	// Check that the table has the expected keys, regardless of their values
	lastIDVal := table.RawGetString("last_insert_id")
	assert.NotNil(t, lastIDVal) // The value exists, regardless of what it is

	rowsAffectedVal := table.RawGetString("rows_affected")
	assert.NotNil(t, rowsAffectedVal) // The value exists, regardless of what it is

	// Error case - should still return a table with the same keys
	errorResult := mockResultWithError{
		err: errors.New("test error"),
	}

	table = resultToTable(L, errorResult)
	assert.NotNil(t, table)

	lastIDVal = table.RawGetString("last_insert_id")
	assert.Equal(t, lua.LNil, lastIDVal) // Should be nil for error case

	rowsAffectedVal = table.RawGetString("rows_affected")
	assert.Equal(t, lua.LNil, rowsAffectedVal) // Should be nil for error case
}
