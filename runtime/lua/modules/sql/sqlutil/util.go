package sqlutil

import (
	"database/sql"
	"fmt"
	"time"

	luaconv "github.com/wippyai/runtime/system/payload/lua"

	lua "github.com/yuin/gopher-lua"
)

func CheckParam(l *lua.LState, index int) (interface{}, error) {
	param := l.Get(index)

	// We expect a table with parameters
	if param.Type() != lua.LTUserData {
		return luaconv.ToGoAny(param), nil
	}

	if ud, ok := param.(*lua.LUserData); ok {
		// Check for SQL NULL
		if ud.Value == "SQL_NULL" {
			return nil, nil
		}

		// Check for typed values
		if typedValue, ok := ud.Value.(*TypedValue); ok {
			// Use the pre-converted value with the right type
			return typedValue.Value, nil
		}
	}

	return nil, fmt.Errorf("parameter type not supported supported")
}

// CheckParams extracts and converts parameters from Lua to Go
func CheckParams(l *lua.LState, index int) (interface{}, error) {
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
	maxn := tbl.MaxN()

	// If MaxN() returns 0, it's not a sequential array
	if maxn == 0 {
		isArray = false
	}

	if isArray {
		// Create a slice with explicit length to preserve nil values
		result := make([]interface{}, maxn)

		// Fill the slice, preserving nil values
		for i := 1; i <= maxn; i++ {
			v := tbl.RawGetInt(i)

			// Check for special NULL value
			if v.Type() == lua.LTUserData {
				if ud, ok := v.(*lua.LUserData); ok {
					// Check for SQL NULL
					if ud.Value == "SQL_NULL" {
						result[i-1] = nil
						continue
					}

					// Check for typed values
					if typedValue, ok := ud.Value.(*TypedValue); ok {
						// Use the pre-converted value with the right type
						result[i-1] = typedValue.Value
						continue
					}
				}
			}

			// Handle normal values
			if v == lua.LNil {
				result[i-1] = nil
			} else {
				// Convert using existing mechanism for a single value
				result[i-1] = luaconv.ToGoAny(v)
			}
		}

		return result, nil
	}

	// For now, we only support positional parameters
	return nil, fmt.Errorf("only positional parameters (array-like tables) are supported")
}

// RowsToTable converts SQL rows to a Lua table
func RowsToTable(l *lua.LState, rows *sql.Rows) (*lua.LTable, error) {
	// Get column information
	columns, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("failed to get column info: %w", err)
	}

	if len(columns) == 0 {
		return nil, fmt.Errorf("query returned no columns")
	}

	// Prepare result table
	resultTable := l.CreateTable(4, 0)
	rowIndex := 1

	// Prepare value containers
	values := make([]any, len(columns))
	valuePtrs := make([]any, len(columns))

	for i := range columns {
		valuePtrs[i] = &values[i]
	}

	// Iterate through rows
	for rows.Next() {
		err := rows.Scan(valuePtrs...)
		if err != nil {
			return nil, fmt.Errorf("failed to scan row %d: %w", rowIndex, err)
		}

		// Create table for this row using map for consistent field names
		rowMap := make(map[string]any)

		// Add values to row map
		for i, col := range columns {
			// Handle nil values
			val := values[i]

			// Convert SQL types to appropriate Go types
			switch v := val.(type) {
			case []byte:
				// Byte arrays are typically strings in SQL results
				rowMap[col] = string(v)
			case time.Time:
				// Convert time to string in ISO format
				rowMap[col] = v.Format(time.RFC3339)
			default:
				rowMap[col] = v
			}
		}

		// Convert the row map to a Lua table
		luaValue, err := luaconv.GoToLua(rowMap) // todo: run directly
		if err != nil {
			return nil, fmt.Errorf("failed to convert row %d to Lua: %w", rowIndex, err)
		}

		// Add row to result table
		resultTable.RawSetInt(rowIndex, luaValue)
		rowIndex++
	}

	// Check for any errors during iteration
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error during row iteration: %w", err)
	}

	return resultTable, nil
}

// ResultToTable converts a SQL result (from exec) to a Lua table
func ResultToTable(l *lua.LState, result sql.Result) *lua.LTable {
	// Create a Go map and then convert to Lua table
	resultMap := make(map[string]interface{})

	// Get last insert Source
	if lastInsertID, err := result.LastInsertId(); err == nil {
		resultMap["last_insert_id"] = lastInsertID
	} else {
		// Some drivers may not support this
		resultMap["last_insert_id"] = nil
	}

	// Get rows affected
	if rowsAffected, err := result.RowsAffected(); err == nil {
		resultMap["rows_affected"] = rowsAffected
	} else {
		// Some drivers may not support this
		resultMap["rows_affected"] = nil
	}

	// Convert using the existing conversion function
	luaTable, err := luaconv.GoToLua(resultMap)
	if err != nil {
		// Fall back to manual table creation if conversion fails
		table := l.CreateTable(0, 2)
		table.RawSetString("last_insert_id", lua.LNil)
		table.RawSetString("rows_affected", lua.LNil)
		return table
	}

	return luaTable.(*lua.LTable)
}
