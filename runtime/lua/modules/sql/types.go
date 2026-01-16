package sql

import (
	"github.com/yuin/gopher-lua/types"
	"github.com/yuin/gopher-lua/types/contract"
	"github.com/yuin/gopher-lua/types/core/state"
)

// errorValueContract is the standard (value, error) return pattern contract
var errorValueContract = contract.ErrorValueSpec()

// DB Stats record
var dbStatsType = &types.RecordType{
	Name: "sql.DBStats",
	Fields: []types.RecordField{
		{Name: "max_open_connections", Type: types.Integer},
		{Name: "open_connections", Type: types.Integer},
		{Name: "in_use", Type: types.Integer},
		{Name: "idle", Type: types.Integer},
		{Name: "wait_count", Type: types.Integer},
		{Name: "wait_duration", Type: types.String},
		{Name: "max_idle_closed", Type: types.Integer},
		{Name: "max_idle_time_closed", Type: types.Integer},
		{Name: "max_lifetime_closed", Type: types.Integer},
	},
}

// Execute result record
var executeResultType = &types.RecordType{
	Name: "sql.ExecuteResult",
	Fields: []types.RecordField{
		{Name: "rows_affected", Type: types.Integer},
		{Name: "last_insert_id", Type: types.Integer},
	},
}

// State machines for typestate tracking
var (
	dbMachine          *state.StateMachine
	transactionMachine *state.StateMachine
	statementMachine   *state.StateMachine
)

// State type instances for return types
var (
	dbOpenState       *state.StateType
	txActiveState     *state.StateType
	stmtPreparedState *state.StateType
)

// runnerType is a union type that accepts either DB<Open> or Transaction<Active>
// This allows query builders to work with both connections and transactions
var runnerType types.Type

func init() {
	// Statement state machine: Prepared -> Closed
	statementMachine = state.NewMachine("sql.Statement", nil, "Prepared")
	prepared := statementMachine.AddState("Prepared", false)
	prepared.AddMethod("query", "Prepared", &types.FunctionType{
		Params:   []types.Type{types.Self},
		Variadic: types.Any,
		Returns:  []types.Type{types.NewArray(types.Any, false), types.Optional(types.LuaError)},
	})
	prepared.AddMethod("execute", "Prepared", &types.FunctionType{
		Params:   []types.Type{types.Self},
		Variadic: types.Any,
		Returns:  []types.Type{executeResultType, types.Optional(types.LuaError)},
	})
	prepared.AddMethod("close", "Closed", types.NewFunction([]types.Type{types.Self}, []types.Type{types.Boolean, types.Optional(types.LuaError)}))
	statementMachine.AddState("Closed", true)
	stmtPreparedState = statementMachine.InitialState()

	// Transaction state machine: Active -> Committed | RolledBack
	// Typestate tracking ensures correct method sequencing
	transactionMachine = state.NewMachine("sql.Transaction", nil, "Active")
	active := transactionMachine.AddState("Active", false)
	active.AddMethod("query", "Active", &types.FunctionType{
		Params:   []types.Type{types.Self, types.String},
		Variadic: types.Any,
		Returns:  []types.Type{types.NewArray(types.Any, false), types.Optional(types.LuaError)},
	})
	active.AddMethod("execute", "Active", &types.FunctionType{
		Params:   []types.Type{types.Self, types.String},
		Variadic: types.Any,
		Returns:  []types.Type{executeResultType, types.Optional(types.LuaError)},
	})
	active.AddMethod("prepare", "Active", types.NewFunction([]types.Type{types.Self, types.String}, []types.Type{stmtPreparedState, types.Optional(types.LuaError)})).
		WithContract(errorValueContract)
	active.AddMethod("savepoint", "Active", types.NewFunction([]types.Type{types.Self, types.String}, []types.Type{types.Boolean, types.Optional(types.LuaError)}))
	active.AddMethod("rollback_to", "Active", types.NewFunction([]types.Type{types.Self, types.String}, []types.Type{types.Boolean, types.Optional(types.LuaError)}))
	active.AddMethod("release", "Active", types.NewFunction([]types.Type{types.Self, types.String}, []types.Type{types.Boolean, types.Optional(types.LuaError)}))
	active.AddMethod("db_type", "Active", types.NewFunction([]types.Type{types.Self}, []types.Type{types.String, types.Optional(types.LuaError)}))
	active.AddMethod("commit", "Committed", types.NewFunction([]types.Type{types.Self}, []types.Type{types.Boolean, types.Optional(types.LuaError)}))
	active.AddMethod("rollback", "RolledBack", types.NewFunction([]types.Type{types.Self, types.Optional(types.String)}, []types.Type{types.Boolean, types.Optional(types.LuaError)}))
	transactionMachine.AddState("Committed", true)
	transactionMachine.AddState("RolledBack", true)
	txActiveState = transactionMachine.InitialState()

	// DB state machine: Open -> Released
	dbMachine = state.NewMachine("sql.DB", nil, "Open")
	open := dbMachine.AddState("Open", false)
	open.AddMethod("type", "Open", types.NewFunction([]types.Type{types.Self}, []types.Type{types.String, types.Optional(types.LuaError)}))
	open.AddMethod("query", "Open", &types.FunctionType{
		Params:   []types.Type{types.Self, types.String},
		Variadic: types.Any,
		Returns:  []types.Type{types.NewArray(types.Any, false), types.Optional(types.LuaError)},
	})
	open.AddMethod("execute", "Open", &types.FunctionType{
		Params:   []types.Type{types.Self, types.String},
		Variadic: types.Any,
		Returns:  []types.Type{executeResultType, types.Optional(types.LuaError)},
	})
	open.AddMethod("prepare", "Open", types.NewFunction([]types.Type{types.Self, types.String}, []types.Type{stmtPreparedState, types.Optional(types.LuaError)})).
		WithContract(errorValueContract)
	open.AddMethod("begin", "Open", types.NewFunction([]types.Type{types.Self, types.Optional(types.Any)}, []types.Type{txActiveState, types.Optional(types.LuaError)})).
		WithContract(errorValueContract)
	open.AddMethod("release", "Released", types.NewFunction([]types.Type{types.Self}, []types.Type{types.Boolean, types.Optional(types.LuaError)}))
	open.AddMethod("stats", "Open", types.NewFunction([]types.Type{types.Self}, []types.Type{dbStatsType, types.Optional(types.LuaError)}))
	dbMachine.AddState("Released", true)
	dbOpenState = dbMachine.InitialState()

	// Initialize runnerType as union of DB<Open> and Transaction<Active>
	runnerType = types.NewUnion(dbOpenState, txActiveState)

	// SelectBuilder methods - returns Self for chaining
	selectBuilderType.Methods = map[string]*types.FunctionType{
		"from":               {Params: []types.Type{types.Self, types.String}, Variadic: types.Any, Returns: []types.Type{types.Self}},
		"join":               {Params: []types.Type{types.Self, types.String}, Variadic: types.Any, Returns: []types.Type{types.Self}},
		"left_join":          {Params: []types.Type{types.Self, types.String}, Variadic: types.Any, Returns: []types.Type{types.Self}},
		"right_join":         {Params: []types.Type{types.Self, types.String}, Variadic: types.Any, Returns: []types.Type{types.Self}},
		"inner_join":         {Params: []types.Type{types.Self, types.String}, Variadic: types.Any, Returns: []types.Type{types.Self}},
		"where":              {Params: []types.Type{types.Self, types.Any}, Variadic: types.Any, Returns: []types.Type{types.Self}},
		"order_by":           {Params: []types.Type{types.Self}, Variadic: types.String, Returns: []types.Type{types.Self}},
		"group_by":           {Params: []types.Type{types.Self}, Variadic: types.String, Returns: []types.Type{types.Self}},
		"having":             {Params: []types.Type{types.Self, types.Any}, Variadic: types.Any, Returns: []types.Type{types.Self}},
		"limit":              types.NewFunction([]types.Type{types.Self, types.Number}, []types.Type{types.Self}),
		"offset":             types.NewFunction([]types.Type{types.Self, types.Number}, []types.Type{types.Self}),
		"columns":            {Params: []types.Type{types.Self}, Variadic: types.String, Returns: []types.Type{types.Self}},
		"distinct":           types.NewFunction([]types.Type{types.Self}, []types.Type{types.Self}),
		"suffix":             {Params: []types.Type{types.Self, types.String}, Variadic: types.Any, Returns: []types.Type{types.Self}},
		"placeholder_format": types.NewFunction([]types.Type{types.Self, placeholderFormatType}, []types.Type{types.Self}),
		"to_sql":             types.NewFunction([]types.Type{types.Self}, []types.Type{types.String, types.NewArray(types.Any, false), types.Optional(types.LuaError)}),
		"run_with":           types.NewFunction([]types.Type{types.Self, runnerType}, []types.Type{queryExecutorType}),
	}

	// InsertBuilder methods - returns Self for chaining
	insertBuilderType.Methods = map[string]*types.FunctionType{
		"into":               types.NewFunction([]types.Type{types.Self, types.String}, []types.Type{types.Self}),
		"columns":            {Params: []types.Type{types.Self}, Variadic: types.String, Returns: []types.Type{types.Self}},
		"values":             {Params: []types.Type{types.Self}, Variadic: types.Any, Returns: []types.Type{types.Self}},
		"set_map":            types.NewFunction([]types.Type{types.Self, types.Any}, []types.Type{types.Self}),
		"select":             types.NewFunction([]types.Type{types.Self, selectBuilderType}, []types.Type{types.Self}),
		"prefix":             {Params: []types.Type{types.Self, types.String}, Variadic: types.Any, Returns: []types.Type{types.Self}},
		"suffix":             {Params: []types.Type{types.Self, types.String}, Variadic: types.Any, Returns: []types.Type{types.Self}},
		"options":            {Params: []types.Type{types.Self}, Variadic: types.String, Returns: []types.Type{types.Self}},
		"placeholder_format": types.NewFunction([]types.Type{types.Self, placeholderFormatType}, []types.Type{types.Self}),
		"to_sql":             types.NewFunction([]types.Type{types.Self}, []types.Type{types.String, types.NewArray(types.Any, false), types.Optional(types.LuaError)}),
		"run_with":           types.NewFunction([]types.Type{types.Self, runnerType}, []types.Type{queryExecutorType}),
	}

	// UpdateBuilder methods - returns Self for chaining
	updateBuilderType.Methods = map[string]*types.FunctionType{
		"table":              types.NewFunction([]types.Type{types.Self, types.String}, []types.Type{types.Self}),
		"set":                types.NewFunction([]types.Type{types.Self, types.String, types.Any}, []types.Type{types.Self}),
		"set_map":            types.NewFunction([]types.Type{types.Self, types.Any}, []types.Type{types.Self}),
		"where":              {Params: []types.Type{types.Self, types.Any}, Variadic: types.Any, Returns: []types.Type{types.Self}},
		"order_by":           {Params: []types.Type{types.Self}, Variadic: types.String, Returns: []types.Type{types.Self}},
		"limit":              types.NewFunction([]types.Type{types.Self, types.Number}, []types.Type{types.Self}),
		"offset":             types.NewFunction([]types.Type{types.Self, types.Number}, []types.Type{types.Self}),
		"from":               types.NewFunction([]types.Type{types.Self, types.String}, []types.Type{types.Self}),
		"from_select":        types.NewFunction([]types.Type{types.Self, selectBuilderType, types.String}, []types.Type{types.Self}),
		"suffix":             {Params: []types.Type{types.Self, types.String}, Variadic: types.Any, Returns: []types.Type{types.Self}},
		"placeholder_format": types.NewFunction([]types.Type{types.Self, placeholderFormatType}, []types.Type{types.Self}),
		"to_sql":             types.NewFunction([]types.Type{types.Self}, []types.Type{types.String, types.NewArray(types.Any, false), types.Optional(types.LuaError)}),
		"run_with":           types.NewFunction([]types.Type{types.Self, runnerType}, []types.Type{queryExecutorType}),
	}

	// DeleteBuilder methods - returns Self for chaining
	deleteBuilderType.Methods = map[string]*types.FunctionType{
		"from":               types.NewFunction([]types.Type{types.Self, types.String}, []types.Type{types.Self}),
		"where":              {Params: []types.Type{types.Self, types.Any}, Variadic: types.Any, Returns: []types.Type{types.Self}},
		"order_by":           {Params: []types.Type{types.Self}, Variadic: types.String, Returns: []types.Type{types.Self}},
		"limit":              types.NewFunction([]types.Type{types.Self, types.Number}, []types.Type{types.Self}),
		"offset":             types.NewFunction([]types.Type{types.Self, types.Number}, []types.Type{types.Self}),
		"suffix":             {Params: []types.Type{types.Self, types.String}, Variadic: types.Any, Returns: []types.Type{types.Self}},
		"placeholder_format": types.NewFunction([]types.Type{types.Self, placeholderFormatType}, []types.Type{types.Self}),
		"to_sql":             types.NewFunction([]types.Type{types.Self}, []types.Type{types.String, types.NewArray(types.Any, false), types.Optional(types.LuaError)}),
		"run_with":           types.NewFunction([]types.Type{types.Self, runnerType}, []types.Type{queryExecutorType}),
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
		"to_sql": types.NewFunction([]types.Type{types.Self}, []types.Type{types.String, types.NewArray(types.Any, false), types.Optional(types.LuaError)}),
	},
}

// Query executor returned by builder.run_with
var queryExecutorType = &types.InterfaceType{
	Name: "sql.QueryExecutor",
	Methods: map[string]*types.FunctionType{
		"query":  types.NewFunction([]types.Type{types.Self}, []types.Type{types.NewArray(types.Any, false), types.Optional(types.LuaError)}),
		"exec":   types.NewFunction([]types.Type{types.Self}, []types.Type{executeResultType, types.Optional(types.LuaError)}),
		"to_sql": types.NewFunction([]types.Type{types.Self}, []types.Type{types.String, types.NewArray(types.Any, false), types.Optional(types.LuaError)}),
	},
}

// SelectBuilder type - forward declaration, methods set in init
var selectBuilderType = &types.InterfaceType{
	Name:    "sql.SelectBuilder",
	Methods: map[string]*types.FunctionType{},
}

// InsertBuilder type - forward declaration, methods set in init
var insertBuilderType = &types.InterfaceType{
	Name:    "sql.InsertBuilder",
	Methods: map[string]*types.FunctionType{},
}

// UpdateBuilder type - forward declaration, methods set in init
var updateBuilderType = &types.InterfaceType{
	Name:    "sql.UpdateBuilder",
	Methods: map[string]*types.FunctionType{},
}

// DeleteBuilder type - forward declaration, methods set in init
var deleteBuilderType = &types.InterfaceType{
	Name:    "sql.DeleteBuilder",
	Methods: map[string]*types.FunctionType{},
}

// Type casting submodule for explicit type hints
var asType = &types.InterfaceType{
	Name: "sql.as",
	Methods: map[string]*types.FunctionType{
		"int":    types.NewFunction([]types.Type{types.Any}, []types.Type{types.Any}),
		"float":  types.NewFunction([]types.Type{types.Any}, []types.Type{types.Any}),
		"string": types.NewFunction([]types.Type{types.Any}, []types.Type{types.Any}),
		"bool":   types.NewFunction([]types.Type{types.Any}, []types.Type{types.Any}),
		"null":   types.NewFunction([]types.Type{}, []types.Type{types.Any}),
		"json":   types.NewFunction([]types.Type{types.Any}, []types.Type{types.Any}),
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

	m.DefineType("DB", dbOpenState)
	m.DefineType("Statement", stmtPreparedState)
	m.DefineType("Transaction", txActiveState)
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
			"as":        asType,
		},
		Methods: map[string]*types.FunctionType{
			"get": {
				Params:  []types.Type{types.String},
				Returns: []types.Type{dbOpenState, types.Optional(types.LuaError)},
				Refine:  contract.ErrorValueSpec(),
			},
		},
	}

	m.SetExport(moduleType)
	return m
}
