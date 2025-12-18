package ostime

import "github.com/yuin/gopher-lua/types"

// DateTable type returned by os.date("*t")
var dateTableType = &types.RecordType{
	Name: "os.DateTable",
	Fields: []types.RecordField{
		{Name: "year", Type: types.Number},
		{Name: "month", Type: types.Number},
		{Name: "day", Type: types.Number},
		{Name: "hour", Type: types.Number},
		{Name: "min", Type: types.Number},
		{Name: "sec", Type: types.Number},
		{Name: "wday", Type: types.Number},
		{Name: "yday", Type: types.Number},
		{Name: "isdst", Type: types.Boolean},
	},
}

// ModuleTypes returns the type manifest for the os module.
func ModuleTypes() *types.TypeManifest {
	m := types.NewManifest("os")

	m.DefineType("DateTable", dateTableType)

	moduleType := &types.InterfaceType{
		Name: "os",
		Fields: map[string]types.Type{
			"platform": types.String,
		},
		Methods: map[string]*types.FunctionType{
			// os.time(table?: table): integer
			"time": types.NewFunction(
				[]types.Type{types.Optional(types.Any)},
				[]types.Type{types.Number},
			),
			// os.date(format?: string, timestamp?: number): string | DateTable
			"date": types.NewFunction(
				[]types.Type{types.Optional(types.String), types.Optional(types.Number)},
				[]types.Type{types.NewUnion(types.String, dateTableType)},
			),
			// os.clock(): number
			"clock": types.NewFunction(nil, []types.Type{types.Number}),
			// os.difftime(t2: number, t1: number): number
			"difftime": types.NewFunction(
				[]types.Type{types.Number, types.Number},
				[]types.Type{types.Number},
			),
		},
	}

	m.SetExport(moduleType)
	return m
}
