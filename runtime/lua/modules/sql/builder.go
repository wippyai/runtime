package sql

import (
	"github.com/Masterminds/squirrel"
	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
)

const (
	selectBuilderTypeName = "sql.SelectBuilder"
	insertBuilderTypeName = "sql.InsertBuilder"
	updateBuilderTypeName = "sql.UpdateBuilder"
	deleteBuilderTypeName = "sql.DeleteBuilder"
	sqlizerTypeName       = "sql.Sqlizer"
)

func init() {
	value.RegisterTypeMethods(nil, selectBuilderTypeName, selectBuilderMetamethods, selectBuilderMethods)
	value.RegisterTypeMethods(nil, insertBuilderTypeName, insertBuilderMetamethods, insertBuilderMethods)
	value.RegisterTypeMethods(nil, updateBuilderTypeName, updateBuilderMetamethods, updateBuilderMethods)
	value.RegisterTypeMethods(nil, deleteBuilderTypeName, deleteBuilderMetamethods, deleteBuilderMethods)
	value.RegisterTypeMethods(nil, sqlizerTypeName, sqlizerMetamethods, sqlizerMethods)
}

func registerBuilderSubmodule(mod *lua.LTable) {
	builder := lua.CreateTable(0, 20)

	builder.RawSetString("select", lua.LGoFunc(builderSelect))
	builder.RawSetString("insert", lua.LGoFunc(builderInsert))
	builder.RawSetString("update", lua.LGoFunc(builderUpdate))
	builder.RawSetString("delete", lua.LGoFunc(builderDelete))

	registerPlaceholderFormats(builder)
	registerExpressionBuilders(builder)

	builder.Immutable = true
	mod.RawSetString("builder", builder)
}

func registerPlaceholderFormats(mod *lua.LTable) {
	questionUD := &lua.LUserData{Value: squirrel.Question}
	mod.RawSetString("question", questionUD)

	dollarUD := &lua.LUserData{Value: squirrel.Dollar}
	mod.RawSetString("dollar", dollarUD)

	atUD := &lua.LUserData{Value: squirrel.AtP}
	mod.RawSetString("at", atUD)

	colonUD := &lua.LUserData{Value: squirrel.Colon}
	mod.RawSetString("colon", colonUD)

	mod.RawSetString("default_placeholder", questionUD)
}

func registerExpressionBuilders(mod *lua.LTable) {
	mod.RawSetString("expr", lua.LGoFunc(builderExpr))
	mod.RawSetString("eq", lua.LGoFunc(builderEq))
	mod.RawSetString("not_eq", lua.LGoFunc(builderNotEq))
	mod.RawSetString("lt", lua.LGoFunc(builderLt))
	mod.RawSetString("lte", lua.LGoFunc(builderLtOrEq))
	mod.RawSetString("gt", lua.LGoFunc(builderGt))
	mod.RawSetString("gte", lua.LGoFunc(builderGtOrEq))
	mod.RawSetString("like", lua.LGoFunc(builderLike))
	mod.RawSetString("not_like", lua.LGoFunc(builderNotLike))
	mod.RawSetString("and_", lua.LGoFunc(builderAnd))
	mod.RawSetString("or_", lua.LGoFunc(builderOr))
}

func luaTableToMap(table *lua.LTable) map[string]any {
	result := make(map[string]any)
	table.ForEach(func(key, val lua.LValue) {
		if key.Type() != lua.LTString {
			return
		}
		keyStr := string(key.(lua.LString))

		if val.Type() == lua.LTUserData {
			if ud, ok := val.(*lua.LUserData); ok {
				if marker, ok := ud.Value.(string); ok && marker == "SQL_NULL" {
					result[keyStr] = nil
					return
				}
				if typed, ok := ud.Value.(*TypedValue); ok {
					result[keyStr] = typed.Value
					return
				}
			}
		}

		result[keyStr] = toGoValue(val)
	})
	return result
}
