package time

import (
	"github.com/yuin/gopher-lua/types"
)

// Forward declarations for mutually referential types
var (
	timeType     *types.InterfaceType
	durationType *types.InterfaceType
	locationType *types.InterfaceType
)

func init() {
	// Duration type
	durationType = &types.InterfaceType{
		Name: "time.Duration",
		Methods: map[string]*types.FunctionType{
			"nanoseconds":  types.NewFunction(nil, []types.Type{types.Number}),
			"microseconds": types.NewFunction(nil, []types.Type{types.Number}),
			"milliseconds": types.NewFunction(nil, []types.Type{types.Number}),
			"seconds":      types.NewFunction(nil, []types.Type{types.Number}),
			"minutes":      types.NewFunction(nil, []types.Type{types.Number}),
			"hours":        types.NewFunction(nil, []types.Type{types.Number}),
		},
	}

	// Location type
	locationType = &types.InterfaceType{
		Name: "time.Location",
		Methods: map[string]*types.FunctionType{
			"string": types.NewFunction(nil, []types.Type{types.String}),
		},
	}

	// Time type (self-referential and references duration/location)
	timeType = &types.InterfaceType{
		Name:    "time.Time",
		Methods: map[string]*types.FunctionType{},
	}
	timeType.Methods["add"] = types.NewFunction([]types.Type{types.Any}, []types.Type{timeType})
	timeType.Methods["sub"] = types.NewFunction([]types.Type{timeType}, []types.Type{durationType})
	timeType.Methods["add_date"] = types.NewFunction([]types.Type{types.Number, types.Number, types.Number}, []types.Type{timeType})
	timeType.Methods["after"] = types.NewFunction([]types.Type{timeType}, []types.Type{types.Boolean})
	timeType.Methods["before"] = types.NewFunction([]types.Type{timeType}, []types.Type{types.Boolean})
	timeType.Methods["equal"] = types.NewFunction([]types.Type{timeType}, []types.Type{types.Boolean})
	timeType.Methods["format"] = types.NewFunction([]types.Type{types.String}, []types.Type{types.String})
	timeType.Methods["format_rfc3339"] = types.NewFunction(nil, []types.Type{types.String})
	timeType.Methods["unix"] = types.NewFunction(nil, []types.Type{types.Number})
	timeType.Methods["unix_nano"] = types.NewFunction(nil, []types.Type{types.Number})
	timeType.Methods["date"] = types.NewFunction(nil, []types.Type{types.Number, types.Number, types.Number})
	timeType.Methods["clock"] = types.NewFunction(nil, []types.Type{types.Number, types.Number, types.Number})
	timeType.Methods["year"] = types.NewFunction(nil, []types.Type{types.Number})
	timeType.Methods["month"] = types.NewFunction(nil, []types.Type{types.Number})
	timeType.Methods["day"] = types.NewFunction(nil, []types.Type{types.Number})
	timeType.Methods["hour"] = types.NewFunction(nil, []types.Type{types.Number})
	timeType.Methods["minute"] = types.NewFunction(nil, []types.Type{types.Number})
	timeType.Methods["second"] = types.NewFunction(nil, []types.Type{types.Number})
	timeType.Methods["nanosecond"] = types.NewFunction(nil, []types.Type{types.Number})
	timeType.Methods["weekday"] = types.NewFunction(nil, []types.Type{types.Number})
	timeType.Methods["year_day"] = types.NewFunction(nil, []types.Type{types.Number})
	timeType.Methods["is_zero"] = types.NewFunction(nil, []types.Type{types.Boolean})
	timeType.Methods["in_location"] = types.NewFunction([]types.Type{locationType}, []types.Type{timeType})
	timeType.Methods["location"] = types.NewFunction(nil, []types.Type{locationType})
	timeType.Methods["utc"] = types.NewFunction(nil, []types.Type{timeType})
	timeType.Methods["in_local"] = types.NewFunction(nil, []types.Type{timeType})
	timeType.Methods["round"] = types.NewFunction([]types.Type{durationType}, []types.Type{timeType})
	timeType.Methods["truncate"] = types.NewFunction([]types.Type{durationType}, []types.Type{timeType})
}

// Ticker type
var tickerType = &types.InterfaceType{
	Name: "time.Ticker",
	Methods: map[string]*types.FunctionType{
		"stop":     types.NewFunction(nil, []types.Type{types.Boolean}),
		"response": types.NewFunction(nil, []types.Type{types.Any}),
		"channel":  types.NewFunction(nil, []types.Type{types.Any}),
	},
}

// Timer type
var timerType = &types.InterfaceType{
	Name: "time.Timer",
	Methods: map[string]*types.FunctionType{
		"stop":     types.NewFunction(nil, []types.Type{types.Boolean}),
		"reset":    types.NewFunction([]types.Type{types.Any}, []types.Type{types.Boolean}),
		"response": types.NewFunction(nil, []types.Type{types.Any}),
		"channel":  types.NewFunction(nil, []types.Type{types.Any}),
	},
}

// ModuleTypes returns the type manifest for the time module.
func ModuleTypes() *types.TypeManifest {
	m := types.NewManifest("time")

	m.DefineType("Time", timeType)
	m.DefineType("Duration", durationType)
	m.DefineType("Location", locationType)
	m.DefineType("Ticker", tickerType)
	m.DefineType("Timer", timerType)

	moduleType := &types.InterfaceType{
		Name: "time",
		Fields: map[string]types.Type{
			"NANOSECOND":  types.Number,
			"MICROSECOND": types.Number,
			"MILLISECOND": types.Number,
			"SECOND":      types.Number,
			"MINUTE":      types.Number,
			"HOUR":        types.Number,
			"RFC3339":     types.String,
			"RFC3339NANO": types.String,
			"RFC822":      types.String,
			"RFC822Z":     types.String,
			"RFC850":      types.String,
			"RFC1123":     types.String,
			"RFC1123Z":    types.String,
			"KITCHEN":     types.String,
			"STAMP":       types.String,
			"STAMP_MILLI": types.String,
			"STAMP_MICRO": types.String,
			"STAMP_NANO":  types.String,
			"DATE_TIME":   types.String,
			"DATE_ONLY":   types.String,
			"TIME_ONLY":   types.String,
			"JANUARY":     types.Number,
			"FEBRUARY":    types.Number,
			"MARCH":       types.Number,
			"APRIL":       types.Number,
			"MAY":         types.Number,
			"JUNE":        types.Number,
			"JULY":        types.Number,
			"AUGUST":      types.Number,
			"SEPTEMBER":   types.Number,
			"OCTOBER":     types.Number,
			"NOVEMBER":    types.Number,
			"DECEMBER":    types.Number,
			"SUNDAY":      types.Number,
			"MONDAY":      types.Number,
			"TUESDAY":     types.Number,
			"WEDNESDAY":   types.Number,
			"THURSDAY":    types.Number,
			"FRIDAY":      types.Number,
			"SATURDAY":    types.Number,
			"utc":         locationType,
			"localtz":     locationType,
		},
		Methods: map[string]*types.FunctionType{
			"sleep":          types.NewFunction([]types.Type{types.Any}, nil),
			"timer":          types.NewFunction([]types.Type{types.Any}, []types.Type{timerType, types.Optional(types.LuaError)}),
			"after":          types.NewFunction([]types.Type{types.Any}, []types.Type{types.Any}),
			"ticker":         types.NewFunction([]types.Type{types.Any}, []types.Type{tickerType, types.Optional(types.LuaError)}),
			"now":            types.NewFunction(nil, []types.Type{timeType}),
			"date":           types.NewFunction([]types.Type{types.Number, types.Number, types.Number, types.Number, types.Number, types.Number, types.Number, types.Optional(locationType)}, []types.Type{timeType}),
			"unix":           types.NewFunction([]types.Type{types.Number, types.Number}, []types.Type{timeType}),
			"parse":          types.NewFunction([]types.Type{types.String, types.String, types.Optional(locationType)}, []types.Type{timeType, types.Optional(types.LuaError)}),
			"parse_duration": types.NewFunction([]types.Type{types.Any}, []types.Type{durationType, types.Optional(types.LuaError)}),
			"load_location":  types.NewFunction([]types.Type{types.String}, []types.Type{locationType, types.Optional(types.LuaError)}),
			"fixed_zone":     types.NewFunction([]types.Type{types.String, types.Number}, []types.Type{locationType}),
		},
	}

	m.SetExport(moduleType)
	return m
}
