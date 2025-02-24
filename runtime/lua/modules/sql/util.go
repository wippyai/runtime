package sql

import (
	"database/sql"
	"fmt"
	"time"

	lua "github.com/yuin/gopher-lua"
)

// checkParams extracts and converts parameters from Lua to Go
// Currently only supports positional parameters
func checkParams(l *lua.LState, index int) (interface{}, error) {
	params := l.Get(index)

	// Handle nil case (no parameters)
	if params == lua.LNil {
		return nil, nil
	}

	// We expect a table with parameters
	if params.Type() != lua.LTTable {
		return nil, fmt.Errorf("parameters must be a table, got %s", params.Type().String())
	}

	tbl := params.(*lua.LTable)

	// Check if this is a positional parameter array (array-like table)
	// or a named parameter map (map-like table)
	isArray := true

	tbl.ForEach(func(k, v lua.LValue) {
		if k.Type() != lua.LTNumber {
			isArray = false
		} else if k.Type() == lua.LTNumber {
			// Get both int and float values to check if it's a proper integer
			floatVal := float64(k.(lua.LNumber))
			intVal := int(floatVal)

			// Check if it's a positive integer (no fractional part)
			if intVal <= 0 || float64(intVal) != floatVal {
				isArray = false
			}
		}
	})

	if isArray {
		// Convert to a slice of interface{} for positional params
		return tableToSlice(tbl)
	} else {
		// For now, we only support positional parameters
		return nil, fmt.Errorf("only positional parameters (array-like tables) are supported")
	}
}

// tableToSlice converts a Lua table to a Go slice
// Returns a properly typed slice of interface{} values and any error that occurred
func tableToSlice(tbl *lua.LTable) ([]interface{}, error) {
	var result []interface{}

	// Find the highest index for pre-allocation
	mx := 0
	tbl.ForEach(func(k, _ lua.LValue) {
		if k.Type() == lua.LTNumber {
			idx := int(k.(lua.LNumber))
			if idx > mx {
				mx = idx
			}
		}
	})

	if mx == 0 {
		// Empty table or non-numeric keys
		return nil, fmt.Errorf("parameter table has no numeric indices")
	}

	// Pre-allocate the result slice
	result = make([]interface{}, mx)

	// Track if we have any nil values in the middle
	hasNilValues := false

	// Fill the slice with values, converting each Lua value to its Go equivalent
	for i := 1; i <= mx; i++ {
		v := tbl.RawGetInt(i)
		if v == lua.LNil {
			hasNilValues = true
		}
		// Adjust for 1-based indexing in Lua
		result[i-1] = luaToGo(v)
	}

	if hasNilValues {
		// Log warning or handle nil values as needed
		// For now we'll allow nil values in parameters
	}

	return result, nil
}

// luaToGo converts a Lua value to a Go value
// Special handling for SQL-compatible types
func luaToGo(v lua.LValue) interface{} {
	switch v.Type() {
	case lua.LTNil:
		return nil
	case lua.LTBool:
		return lua.LVAsBool(v)
	case lua.LTNumber:
		num := float64(v.(lua.LNumber))
		// Check if this is actually an integer
		if num == float64(int64(num)) {
			return int64(num)
		}
		return num
	case lua.LTString:
		return string(v.(lua.LString))
	case lua.LTTable:
		tbl := v.(*lua.LTable)
		// Check if the table can represent binary data
		if isBinaryData(tbl) {
			return tableToBinary(tbl)
		}
		// If the table has sequential numeric keys, convert to slice
		isArray := true
		maxn := 0

		tbl.ForEach(func(k, _ lua.LValue) {
			if k.Type() != lua.LTNumber {
				isArray = false
				return
			}
			n := int(k.(lua.LNumber))
			if n > maxn {
				maxn = n
			}
		})

		if isArray && maxn > 0 {
			result := make([]interface{}, maxn)
			for i := 1; i <= maxn; i++ {
				result[i-1] = luaToGo(tbl.RawGetInt(i))
			}
			return result
		}

		// Otherwise convert to map
		result := make(map[string]interface{})
		tbl.ForEach(func(k, v lua.LValue) {
			result[k.String()] = luaToGo(v)
		})
		return result
	default:
		// For any other type, use string representation
		return v.String()
	}
}

// isBinaryData checks if a table can be interpreted as binary data
func isBinaryData(tbl *lua.LTable) bool {
	if tbl.RawGetString("_binary") != lua.LNil {
		return true
	}
	return false
}

// tableToBinary converts a table marked as binary data to a byte slice
func tableToBinary(tbl *lua.LTable) []byte {
	// Check if there's a direct data field
	dataField := tbl.RawGetString("data")
	if dataField.Type() == lua.LTString {
		return []byte(dataField.(lua.LString))
	}

	// Otherwise, try to interpret sequentially indexed values as bytes
	maxn := 0
	tbl.ForEach(func(k, _ lua.LValue) {
		if k.Type() == lua.LTNumber {
			n := int(k.(lua.LNumber))
			if n > maxn {
				maxn = n
			}
		}
	})

	if maxn == 0 {
		return []byte{}
	}

	result := make([]byte, maxn)
	for i := 1; i <= maxn; i++ {
		v := tbl.RawGetInt(i)
		if v.Type() == lua.LTNumber {
			result[i-1] = byte(int(v.(lua.LNumber)))
		}
	}

	return result
}

// rowsToTable converts SQL rows to a Lua table
// Enhanced error handling and type conversions
func rowsToTable(l *lua.LState, rows *sql.Rows) (*lua.LTable, error) {
	// Get column information
	columns, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("failed to get column info: %w", err)
	}

	if len(columns) == 0 {
		return nil, fmt.Errorf("query returned no columns")
	}

	// Get column types if available
	colTypes, err := rows.ColumnTypes()
	if err != nil {
		// Log that column types couldn't be retrieved, but continue
		// This isn't fatal as we can still process the data
	}

	// Prepare result table
	resultTable := l.NewTable()
	rowIndex := 1

	// Prepare value containers
	values := make([]interface{}, len(columns))
	valuePtrs := make([]interface{}, len(columns))

	for i := range columns {
		valuePtrs[i] = &values[i]
	}

	// Iterate through rows
	for rows.Next() {
		err := rows.Scan(valuePtrs...)
		if err != nil {
			return nil, fmt.Errorf("failed to scan row %d: %w", rowIndex, err)
		}

		// Create table for this row
		rowTable := l.NewTable()

		// Add values to row table
		for i, col := range columns {
			// Convert the Go value to Lua value
			lv := goToLua(l, values[i], colTypes, i)

			// Set the column in the row table
			rowTable.RawSetString(col, lv)
		}

		// Add row to result table
		resultTable.RawSetInt(rowIndex, rowTable)
		rowIndex++
	}

	// Check for any errors during iteration
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error during row iteration: %w", err)
	}

	return resultTable, nil
}

// goToLua converts a Go value from SQL to a Lua value
// Uses column type information when available for better type mapping
func goToLua(l *lua.LState, val interface{}, colTypes []*sql.ColumnType, colIndex int) lua.LValue {
	// Handle nil values
	if val == nil {
		return lua.LNil
	}

	// Try to use column type information if available
	var specialType string
	if colTypes != nil && colIndex < len(colTypes) && colTypes[colIndex] != nil {
		specialType = colTypes[colIndex].DatabaseTypeName()
	}

	// Handle specific SQL types
	switch v := val.(type) {
	case []byte:
		// For known binary types, return as is
		if specialType == "BLOB" || specialType == "BINARY" || specialType == "VARBINARY" {
			// Create a binary data representation table
			binTable := l.NewTable()
			binTable.RawSetString("_binary", lua.LTrue)
			binTable.RawSetString("data", lua.LString(string(v)))
			return binTable
		}
		// Otherwise, byte arrays are typically strings in SQL results
		return lua.LString(string(v))
	case string:
		return lua.LString(v)
	case int64:
		return lua.LNumber(v)
	case int32:
		return lua.LNumber(v)
	case int:
		return lua.LNumber(v)
	case int16:
		return lua.LNumber(v)
	case int8:
		return lua.LNumber(v)
	case uint64:
		return lua.LNumber(v)
	case uint32:
		return lua.LNumber(v)
	case uint:
		return lua.LNumber(v)
	case uint16:
		return lua.LNumber(v)
	case uint8:
		return lua.LNumber(v)
	case float64:
		return lua.LNumber(v)
	case float32:
		return lua.LNumber(v)
	case bool:
		return lua.LBool(v)
	case time.Time:
		// Return time as ISO format string by default
		return lua.LString(v.Format(time.RFC3339))
	default:
		// Fall back to string representation for unknown types
		return lua.LString(fmt.Sprintf("%v", v))
	}
}

// resultToTable converts a SQL result (from exec) to a Lua table
func resultToTable(l *lua.LState, result sql.Result) *lua.LTable {
	table := l.NewTable()

	// Get last insert ID
	if lastInsertID, err := result.LastInsertId(); err == nil {
		table.RawSetString("last_insert_id", lua.LNumber(lastInsertID))
	} else {
		// Some drivers may not support this
		table.RawSetString("last_insert_id", lua.LNil)
	}

	// Get rows affected
	if rowsAffected, err := result.RowsAffected(); err == nil {
		table.RawSetString("rows_affected", lua.LNumber(rowsAffected))
	} else {
		// Some drivers may not support this
		table.RawSetString("rows_affected", lua.LNil)
	}

	return table
}
