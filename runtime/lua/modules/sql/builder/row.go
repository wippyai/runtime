package builder

import (
	"reflect"
	"time"

	"github.com/Masterminds/squirrel"
	lua "github.com/yuin/gopher-lua"
)

// Row is a wrapper around Squirrel's RowScanner
type Row struct {
	RowScanner squirrel.RowScanner
	err        error
}

// RegisterRowMetatable registers the metatable for Row objects
func RegisterRowMetatable(l *lua.LState) {
	// Create Row metatable
	mt := l.CreateTable(0, 2)

	// Create methods table
	methods := l.CreateTable(0, 1)
	methods.RawSetString("scan", l.NewFunction(rowScan))

	// Set up metatable
	mt.RawSetString("__index", methods)
	mt.RawSetString("__tostring", l.NewFunction(func(l *lua.LState) int {
		l.Push(lua.LString("RowScanner"))
		return 1
	}))

	// Register the metatable
	l.SetField(l.Registry, "RowMetatable", mt)
}

// WrapRow creates a new Row userdata with the proper metatable
func WrapRow(l *lua.LState, row squirrel.RowScanner, err error) lua.LValue {
	// Create a Row object
	rowObj := &Row{RowScanner: row, err: err}

	// Create Lua userdata
	ud := l.NewUserData()
	ud.Value = rowObj

	// Set metatable
	mt := l.GetField(l.Registry, "RowMetatable")
	if mt.Type() != lua.LTTable {
		// This should not happen if RegisterRowMetatable was called
		l.RaiseError("RowMetatable not registered")
		return lua.LNil
	}

	l.SetMetatable(ud, mt.(*lua.LTable))
	return ud
}

// CheckRow checks if the given value is a Row and returns it
func CheckRow(l *lua.LState, idx int) *Row {
	ud := l.CheckUserData(idx)
	if row, ok := ud.Value.(*Row); ok {
		return row
	}
	l.ArgError(idx, "expected Row object")
	return nil
}

// rowScan implements the scan method for Row objects
// Usage: local success, val1, val2, err = row:scan()
func rowScan(l *lua.LState) int {
	// Check Row object
	row := CheckRow(l, 1)
	if row == nil {
		return 0
	}

	// Check if there's a pre-existing error
	if row.err != nil {
		l.Push(lua.LBool(false))
		l.Push(lua.LString(row.err.Error()))
		return 2
	}

	// Create destination slice based on type hints (if provided)
	numDests := l.GetTop() - 1
	if numDests <= 0 {
		// Default behavior: scan all columns
		columns, columnErr := getRowColumns(row.RowScanner)
		if columnErr != nil {
			l.Push(lua.LBool(false))
			l.Push(lua.LString(columnErr.Error()))
			return 2
		}
		numDests = len(columns)
	}

	// Create properly typed destination variables based on hints
	dests := make([]interface{}, numDests)
	for i := 0; i < numDests; i++ {
		// Create a destination variable
		// By default we use a string scanner for text, etc.
		dests[i] = new(interface{})

		// If a type hint was provided, use it
		if l.GetTop() > i+1 {
			hint := l.Get(i + 2)
			if hint.Type() == lua.LTString {
				hintStr := hint.String()
				switch hintStr {
				case "string":
					dests[i] = new(string)
				case "number", "int", "integer":
					dests[i] = new(int64)
				case "float", "real":
					dests[i] = new(float64)
				case "bool", "boolean":
					dests[i] = new(bool)
				case "time", "date", "datetime":
					dests[i] = new(time.Time)
				case "bytes", "blob":
					dests[i] = new([]byte)
				}
			}
		}
	}

	// Scan row into destinations
	err := row.RowScanner.Scan(dests...)
	if err != nil {
		l.Push(lua.LBool(false))
		l.Push(lua.LString(err.Error()))
		return 2
	}

	// Push success flag
	l.Push(lua.LBool(true))

	// Convert and push scanned values onto the stack
	for _, dest := range dests {
		// Get the actual value
		val := reflect.ValueOf(dest).Elem().Interface()
		l.Push(goToLuaValue(l, val))
	}

	// Return success + values
	return numDests + 1
}

// getRowColumns attempts to retrieve column info from a scanner
// This may not work for all scanner implementations
func getRowColumns(scanner squirrel.RowScanner) ([]string, error) {
	// Attempt to access columns through reflection
	// This is a best-effort approach and may not work with all implementations
	scannerVal := reflect.ValueOf(scanner)
	if scannerVal.Kind() == reflect.Ptr {
		scannerVal = scannerVal.Elem()
	}

	// Try to find a "Columns" method
	columnsMethod := reflect.Value{}
	if scannerVal.Kind() == reflect.Struct {
		// Check for sql.Row or sql.Rows
		if rowsField := scannerVal.FieldByName("Rows"); rowsField.IsValid() {
			scannerVal = rowsField
		}

		// Look for a method to get columns
		scannerType := scannerVal.Type()
		for i := 0; i < scannerType.NumMethod(); i++ {
			methodName := scannerType.Method(i).Name
			if methodName == "Columns" {
				columnsMethod = scannerVal.Method(i)
				break
			}
		}
	}

	if columnsMethod.IsValid() {
		results := columnsMethod.Call(nil)
		if len(results) >= 1 {
			columnsVal := results[0]
			if columnsVal.Kind() == reflect.Slice && columnsVal.Type().Elem().Kind() == reflect.String {
				// Convert []string
				length := columnsVal.Len()
				columns := make([]string, length)
				for i := 0; i < length; i++ {
					columns[i] = columnsVal.Index(i).String()
				}
				return columns, nil
			}
		}
	}

	// Fallback to default string columns
	return []string{"column1", "column2", "column3", "column4", "column5"}, nil
}
