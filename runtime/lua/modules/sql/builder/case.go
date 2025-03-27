package builder

import (
	"fmt"

	"github.com/Masterminds/squirrel"
	lua "github.com/yuin/gopher-lua"
)

// caseBuilderWrapper wraps a Squirrel CaseBuilder
type caseBuilderWrapper struct {
	builder squirrel.CaseBuilder
}

// builderCase creates a new CASE expression
// Usage: sql.builder.case([value]) - value is optional
func builderCase(l *lua.LState) int {
	// Create a new CaseBuilder
	var builder squirrel.CaseBuilder

	// Check if we have a value for the CASE
	if l.GetTop() > 0 {
		value := luaToGoValue(l, l.Get(1))
		builder = squirrel.Case(value)
	} else {
		builder = squirrel.Case()
	}

	// Create wrapper
	wrapper := &caseBuilderWrapper{
		builder: builder,
	}

	// Create userdata and set metatable
	ud := l.NewUserData()
	ud.Value = wrapper
	l.SetMetatable(ud, getCaseBuilderMetatable(l))

	l.Push(ud)
	return 1
}

// getCaseBuilderMetatable returns the metatable for CaseBuilder objects
func getCaseBuilderMetatable(l *lua.LState) *lua.LTable {
	// Create method table
	methods := l.CreateTable(0, 4) // Reserve space for all methods

	// Register all the methods
	methods.RawSetString("when", l.NewFunction(caseWhen))
	methods.RawSetString("else", l.NewFunction(caseElse))
	methods.RawSetString("to_sql", l.NewFunction(caseToSql))

	// Create metatable with __index and __tostring
	mt := l.CreateTable(0, 2)
	mt.RawSetString("__index", methods)
	mt.RawSetString("__tostring", l.NewFunction(func(l *lua.LState) int {
		wrapper := checkCaseBuilder(l)
		if wrapper == nil {
			l.Push(lua.LString("Invalid CaseBuilder"))
			return 1
		}

		// Get SQL for display
		query, args, err := wrapper.builder.ToSql()
		if err != nil {
			l.Push(lua.LString(fmt.Sprintf("CaseBuilder Error: %v", err)))
			return 1
		}

		l.Push(lua.LString(fmt.Sprintf("CaseBuilder: %s [Args: %v]", query, args)))
		return 1
	}))

	return mt
}

// checkCaseBuilder ensures the first argument is a CaseBuilder and returns it
func checkCaseBuilder(l *lua.LState) *caseBuilderWrapper {
	ud := l.CheckUserData(1)
	if wrapper, ok := ud.Value.(*caseBuilderWrapper); ok {
		return wrapper
	}
	l.ArgError(1, "expected CaseBuilder object")
	return nil
}

// Method implementations for CaseBuilder

// caseWhen adds a WHEN condition and result
// Usage: case:when(condition, result)
func caseWhen(l *lua.LState) int {
	wrapper := checkCaseBuilder(l)
	if wrapper == nil {
		return 0
	}

	// Get condition and result
	condition := luaToGoValue(l, l.Get(2))
	result := luaToGoValue(l, l.Get(3))

	wrapper.builder = wrapper.builder.When(condition, result)

	l.Push(l.CheckUserData(1)) // Return self for chaining
	return 1
}

// caseElse adds an ELSE result
// Usage: case:else(result)
func caseElse(l *lua.LState) int {
	wrapper := checkCaseBuilder(l)
	if wrapper == nil {
		return 0
	}

	// Get result
	result := luaToGoValue(l, l.Get(2))

	wrapper.builder = wrapper.builder.Else(result)

	l.Push(l.CheckUserData(1)) // Return self for chaining
	return 1
}

// caseToSql generates the SQL and args
// Usage: sql, args = case:to_sql()
func caseToSql(l *lua.LState) int {
	wrapper := checkCaseBuilder(l)
	if wrapper == nil {
		return 0
	}

	query, args, err := wrapper.builder.ToSql()
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	l.Push(lua.LString(query))

	// Convert args to Lua table
	argsTable := l.CreateTable(len(args), 0)
	for i, arg := range args {
		argsTable.RawSetInt(i+1, goToLuaValue(l, arg))
	}

	l.Push(argsTable)
	return 2
}
