package sql

import (
	"github.com/yuin/gopher-lua/types"
)

// DB Stats record
var dbStatsType = &types.RecordType{
	Name: "sql.DBStats",
	Fields: []types.RecordField{
		{Name: "max_open_connections", Type: types.Number},
		{Name: "open_connections", Type: types.Number},
		{Name: "in_use", Type: types.Number},
		{Name: "idle", Type: types.Number},
		{Name: "wait_count", Type: types.Number},
		{Name: "wait_duration", Type: types.String},
		{Name: "max_idle_closed", Type: types.Number},
		{Name: "max_idle_time_closed", Type: types.Number},
		{Name: "max_lifetime_closed", Type: types.Number},
	},
}

// Execute result record
var executeResultType = &types.RecordType{
	Name: "sql.ExecuteResult",
	Fields: []types.RecordField{
		{Name: "rows_affected", Type: types.Number},
		{Name: "last_insert_id", Type: types.Number},
	},
}

// Forward declarations
var (
	dbInterfaceType *types.InterfaceType
	statementType   *types.InterfaceType
	transactionType *types.InterfaceType
)

func init() {
	// Statement type
	statementType = &types.InterfaceType{
		Name: "sql.Statement",
		Methods: map[string]*types.FunctionType{
			"query":   {Params: nil, Variadic: types.Any, Returns: []types.Type{types.NewArray(types.Any, false), types.Optional(types.LuaError)}},
			"execute": {Params: nil, Variadic: types.Any, Returns: []types.Type{executeResultType, types.Optional(types.LuaError)}},
			"close":   types.NewFunction(nil, []types.Type{types.Boolean, types.Optional(types.LuaError)}),
		},
	}

	// Transaction type
	transactionType = &types.InterfaceType{
		Name: "sql.Transaction",
		Methods: map[string]*types.FunctionType{
			"query":     {Params: []types.Type{types.String}, Variadic: types.Any, Returns: []types.Type{types.NewArray(types.Any, false), types.Optional(types.LuaError)}},
			"execute":   {Params: []types.Type{types.String}, Variadic: types.Any, Returns: []types.Type{executeResultType, types.Optional(types.LuaError)}},
			"prepare":   types.NewFunction([]types.Type{types.String}, []types.Type{statementType, types.Optional(types.LuaError)}),
			"savepoint": types.NewFunction([]types.Type{types.String}, []types.Type{types.Boolean, types.Optional(types.LuaError)}),
			"commit":    types.NewFunction(nil, []types.Type{types.Boolean, types.Optional(types.LuaError)}),
			"rollback":  types.NewFunction([]types.Type{types.Optional(types.String)}, []types.Type{types.Boolean, types.Optional(types.LuaError)}),
		},
	}

	// DB type
	dbInterfaceType = &types.InterfaceType{
		Name: "sql.DB",
		Methods: map[string]*types.FunctionType{
			"type":    types.NewFunction(nil, []types.Type{types.String, types.Optional(types.LuaError)}),
			"query":   {Params: []types.Type{types.String}, Variadic: types.Any, Returns: []types.Type{types.NewArray(types.Any, false), types.Optional(types.LuaError)}},
			"execute": {Params: []types.Type{types.String}, Variadic: types.Any, Returns: []types.Type{executeResultType, types.Optional(types.LuaError)}},
			"prepare": types.NewFunction([]types.Type{types.String}, []types.Type{statementType, types.Optional(types.LuaError)}),
			"begin":   types.NewFunction([]types.Type{types.Optional(types.Any)}, []types.Type{transactionType, types.Optional(types.LuaError)}),
			"release": types.NewFunction(nil, []types.Type{types.Boolean, types.Optional(types.LuaError)}),
			"stats":   types.NewFunction(nil, []types.Type{dbStatsType, types.Optional(types.LuaError)}),
		},
	}
}

// Type constants
var sqlTypeConstType = &types.InterfaceType{
	Name: "sql.type",
	Fields: map[string]types.Type{
		"POSTGRES": types.String,
		"MYSQL":    types.String,
		"SQLITE":   types.String,
		"MSSQL":    types.String,
		"ORACLE":   types.String,
		"UNKNOWN":  types.String,
	},
}

// Isolation constants
var isolationConstType = &types.InterfaceType{
	Name: "sql.isolation",
	Fields: map[string]types.Type{
		"DEFAULT":          types.String,
		"READ_UNCOMMITTED": types.String,
		"READ_COMMITTED":   types.String,
		"WRITE_COMMITTED":  types.String,
		"REPEATABLE_READ":  types.String,
		"SERIALIZABLE":     types.String,
	},
}

// Placeholder format type
var placeholderFormatType = types.Any

// Sqlizer type for SQL expression builders
var sqlizerType = &types.InterfaceType{
	Name: "sql.Sqlizer",
	Methods: map[string]*types.FunctionType{
		"to_sql": types.NewFunction(nil, []types.Type{types.String, types.NewArray(types.Any, false), types.Optional(types.LuaError)}),
	},
}

// Query executor returned by builder.run_with
var queryExecutorType = &types.InterfaceType{
	Name: "sql.QueryExecutor",
	Methods: map[string]*types.FunctionType{
		"query":  types.NewFunction(nil, []types.Type{types.NewArray(types.Any, false), types.Optional(types.LuaError)}),
		"exec":   types.NewFunction(nil, []types.Type{executeResultType, types.Optional(types.LuaError)}),
		"to_sql": types.NewFunction(nil, []types.Type{types.String, types.NewArray(types.Any, false), types.Optional(types.LuaError)}),
	},
}

// SelectBuilder type
var selectBuilderType = &types.InterfaceType{
	Name: "sql.SelectBuilder",
	Methods: map[string]*types.FunctionType{
		"from":               {Params: []types.Type{types.String}, Variadic: types.Any, Returns: []types.Type{nil}},
		"join":               {Params: []types.Type{types.String}, Variadic: types.Any, Returns: []types.Type{nil}},
		"left_join":          {Params: []types.Type{types.String}, Variadic: types.Any, Returns: []types.Type{nil}},
		"right_join":         {Params: []types.Type{types.String}, Variadic: types.Any, Returns: []types.Type{nil}},
		"inner_join":         {Params: []types.Type{types.String}, Variadic: types.Any, Returns: []types.Type{nil}},
		"where":              {Params: []types.Type{types.Any}, Variadic: types.Any, Returns: []types.Type{nil}},
		"order_by":           {Params: nil, Variadic: types.String, Returns: []types.Type{nil}},
		"group_by":           {Params: nil, Variadic: types.String, Returns: []types.Type{nil}},
		"having":             {Params: []types.Type{types.Any}, Variadic: types.Any, Returns: []types.Type{nil}},
		"limit":              types.NewFunction([]types.Type{types.Number}, nil),
		"offset":             types.NewFunction([]types.Type{types.Number}, nil),
		"columns":            {Params: nil, Variadic: types.String, Returns: []types.Type{nil}},
		"distinct":           types.NewFunction(nil, nil),
		"suffix":             {Params: []types.Type{types.String}, Variadic: types.Any, Returns: []types.Type{nil}},
		"placeholder_format": types.NewFunction([]types.Type{placeholderFormatType}, nil),
		"to_sql":             types.NewFunction(nil, []types.Type{types.String, types.NewArray(types.Any, false), types.Optional(types.LuaError)}),
		"run_with":           types.NewFunction([]types.Type{dbInterfaceType}, []types.Type{queryExecutorType}),
	},
}

// InsertBuilder type
var insertBuilderType = &types.InterfaceType{
	Name: "sql.InsertBuilder",
	Methods: map[string]*types.FunctionType{
		"into":               types.NewFunction([]types.Type{types.String}, nil),
		"columns":            {Params: nil, Variadic: types.String, Returns: []types.Type{nil}},
		"values":             {Params: nil, Variadic: types.Any, Returns: []types.Type{nil}},
		"set_map":            types.NewFunction([]types.Type{types.Any}, nil),
		"select":             types.NewFunction([]types.Type{selectBuilderType}, nil),
		"prefix":             {Params: []types.Type{types.String}, Variadic: types.Any, Returns: []types.Type{nil}},
		"suffix":             {Params: []types.Type{types.String}, Variadic: types.Any, Returns: []types.Type{nil}},
		"options":            {Params: nil, Variadic: types.String, Returns: []types.Type{nil}},
		"placeholder_format": types.NewFunction([]types.Type{placeholderFormatType}, nil),
		"to_sql":             types.NewFunction(nil, []types.Type{types.String, types.NewArray(types.Any, false), types.Optional(types.LuaError)}),
		"run_with":           types.NewFunction([]types.Type{dbInterfaceType}, []types.Type{queryExecutorType}),
	},
}

// UpdateBuilder type
var updateBuilderType = &types.InterfaceType{
	Name: "sql.UpdateBuilder",
	Methods: map[string]*types.FunctionType{
		"table":              types.NewFunction([]types.Type{types.String}, nil),
		"set":                types.NewFunction([]types.Type{types.String, types.Any}, nil),
		"set_map":            types.NewFunction([]types.Type{types.Any}, nil),
		"where":              {Params: []types.Type{types.Any}, Variadic: types.Any, Returns: []types.Type{nil}},
		"order_by":           {Params: nil, Variadic: types.String, Returns: []types.Type{nil}},
		"limit":              types.NewFunction([]types.Type{types.Number}, nil),
		"offset":             types.NewFunction([]types.Type{types.Number}, nil),
		"from":               types.NewFunction([]types.Type{types.String}, nil),
		"from_select":        types.NewFunction([]types.Type{selectBuilderType, types.String}, nil),
		"suffix":             {Params: []types.Type{types.String}, Variadic: types.Any, Returns: []types.Type{nil}},
		"placeholder_format": types.NewFunction([]types.Type{placeholderFormatType}, nil),
		"to_sql":             types.NewFunction(nil, []types.Type{types.String, types.NewArray(types.Any, false), types.Optional(types.LuaError)}),
		"run_with":           types.NewFunction([]types.Type{dbInterfaceType}, []types.Type{queryExecutorType}),
	},
}

// DeleteBuilder type
var deleteBuilderType = &types.InterfaceType{
	Name: "sql.DeleteBuilder",
	Methods: map[string]*types.FunctionType{
		"from":               types.NewFunction([]types.Type{types.String}, nil),
		"where":              {Params: []types.Type{types.Any}, Variadic: types.Any, Returns: []types.Type{nil}},
		"order_by":           {Params: nil, Variadic: types.String, Returns: []types.Type{nil}},
		"limit":              types.NewFunction([]types.Type{types.Number}, nil),
		"offset":             types.NewFunction([]types.Type{types.Number}, nil),
		"suffix":             {Params: []types.Type{types.String}, Variadic: types.Any, Returns: []types.Type{nil}},
		"placeholder_format": types.NewFunction([]types.Type{placeholderFormatType}, nil),
		"to_sql":             types.NewFunction(nil, []types.Type{types.String, types.NewArray(types.Any, false), types.Optional(types.LuaError)}),
		"run_with":           types.NewFunction([]types.Type{dbInterfaceType}, []types.Type{queryExecutorType}),
	},
}

// Builder submodule type
var builderType = &types.InterfaceType{
	Name: "sql.builder",
	Fields: map[string]types.Type{
		"question":            placeholderFormatType,
		"dollar":              placeholderFormatType,
		"at":                  placeholderFormatType,
		"colon":               placeholderFormatType,
		"default_placeholder": placeholderFormatType,
	},
	Methods: map[string]*types.FunctionType{
		"select":   {Params: nil, Variadic: types.String, Returns: []types.Type{selectBuilderType}},
		"insert":   types.NewFunction([]types.Type{types.Optional(types.String)}, []types.Type{insertBuilderType}),
		"update":   types.NewFunction([]types.Type{types.Optional(types.String)}, []types.Type{updateBuilderType}),
		"delete":   types.NewFunction([]types.Type{types.Optional(types.String)}, []types.Type{deleteBuilderType}),
		"expr":     {Params: []types.Type{types.String}, Variadic: types.Any, Returns: []types.Type{sqlizerType}},
		"eq":       types.NewFunction([]types.Type{types.String, types.Any}, []types.Type{sqlizerType}),
		"not_eq":   types.NewFunction([]types.Type{types.String, types.Any}, []types.Type{sqlizerType}),
		"lt":       types.NewFunction([]types.Type{types.String, types.Any}, []types.Type{sqlizerType}),
		"lte":      types.NewFunction([]types.Type{types.String, types.Any}, []types.Type{sqlizerType}),
		"gt":       types.NewFunction([]types.Type{types.String, types.Any}, []types.Type{sqlizerType}),
		"gte":      types.NewFunction([]types.Type{types.String, types.Any}, []types.Type{sqlizerType}),
		"like":     types.NewFunction([]types.Type{types.String, types.Any}, []types.Type{sqlizerType}),
		"not_like": types.NewFunction([]types.Type{types.String, types.Any}, []types.Type{sqlizerType}),
		"and_":     {Params: nil, Variadic: sqlizerType, Returns: []types.Type{sqlizerType}},
		"or_":      {Params: nil, Variadic: sqlizerType, Returns: []types.Type{sqlizerType}},
	},
}

// ModuleTypes returns the type manifest for the sql module.
func ModuleTypes() *types.TypeManifest {
	m := types.NewManifest("sql")

	m.DefineType("DB", dbInterfaceType)
	m.DefineType("Statement", statementType)
	m.DefineType("Transaction", transactionType)
	m.DefineType("DBStats", dbStatsType)
	m.DefineType("ExecuteResult", executeResultType)
	m.DefineType("Sqlizer", sqlizerType)
	m.DefineType("QueryExecutor", queryExecutorType)
	m.DefineType("SelectBuilder", selectBuilderType)
	m.DefineType("InsertBuilder", insertBuilderType)
	m.DefineType("UpdateBuilder", updateBuilderType)
	m.DefineType("DeleteBuilder", deleteBuilderType)

	moduleType := &types.InterfaceType{
		Name: "sql",
		Fields: map[string]types.Type{
			"NULL":      types.Any,
			"type":      sqlTypeConstType,
			"isolation": isolationConstType,
			"builder":   builderType,
		},
		Methods: map[string]*types.FunctionType{
			"get": types.NewFunction([]types.Type{types.String}, []types.Type{dbInterfaceType, types.Optional(types.LuaError)}),
		},
	}

	m.SetExport(moduleType)
	return m
}
