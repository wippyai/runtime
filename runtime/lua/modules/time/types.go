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
			"nanoseconds":  types.NewFunction([]types.Type{types.Self}, []types.Type{types.Number}),
			"microseconds": types.NewFunction([]types.Type{types.Self}, []types.Type{types.Number}),
			"milliseconds": types.NewFunction([]types.Type{types.Self}, []types.Type{types.Number}),
			"seconds":      types.NewFunction([]types.Type{types.Self}, []types.Type{types.Number}),
			"minutes":      types.NewFunction([]types.Type{types.Self}, []types.Type{types.Number}),
			"hours":        types.NewFunction([]types.Type{types.Self}, []types.Type{types.Number}),
		},
	}

	// Location type
	locationType = &types.InterfaceType{
		Name: "time.Location",
		Methods: map[string]*types.FunctionType{
			"string": types.NewFunction([]types.Type{types.Self}, []types.Type{types.String}),
		},
	}

	// Time type (self-referential and references duration/location)
	timeType = &types.InterfaceType{
		Name:    "time.Time",
		Methods: map[string]*types.FunctionType{},
	}
	timeType.Methods["add"] = types.NewFunction([]types.Type{types.Self, types.Any}, []types.Type{timeType})
	timeType.Methods["sub"] = types.NewFunction([]types.Type{types.Self, timeType}, []types.Type{durationType})
	timeType.Methods["add_date"] = types.NewFunction([]types.Type{types.Self, types.Number, types.Number, types.Number}, []types.Type{timeType})
	timeType.Methods["after"] = types.NewFunction([]types.Type{types.Self, timeType}, []types.Type{types.Boolean})
	timeType.Methods["before"] = types.NewFunction([]types.Type{types.Self, timeType}, []types.Type{types.Boolean})
	timeType.Methods["equal"] = types.NewFunction([]types.Type{types.Self, timeType}, []types.Type{types.Boolean})
	timeType.Methods["format"] = types.NewFunction([]types.Type{types.Self, types.String}, []types.Type{types.String})
	timeType.Methods["format_rfc3339"] = types.NewFunction([]types.Type{types.Self}, []types.Type{types.String})
	timeType.Methods["unix"] = types.NewFunction([]types.Type{types.Self}, []types.Type{types.Integer})
	timeType.Methods["unix_nano"] = types.NewFunction([]types.Type{types.Self}, []types.Type{types.Integer})
	timeType.Methods["date"] = types.NewFunction([]types.Type{types.Self}, []types.Type{types.Integer, types.Integer, types.Integer})
	timeType.Methods["clock"] = types.NewFunction([]types.Type{types.Self}, []types.Type{types.Integer, types.Integer, types.Integer})
	timeType.Methods["year"] = types.NewFunction([]types.Type{types.Self}, []types.Type{types.Integer})
	timeType.Methods["month"] = types.NewFunction([]types.Type{types.Self}, []types.Type{types.Integer})
	timeType.Methods["day"] = types.NewFunction([]types.Type{types.Self}, []types.Type{types.Integer})
	timeType.Methods["hour"] = types.NewFunction([]types.Type{types.Self}, []types.Type{types.Integer})
	timeType.Methods["minute"] = types.NewFunction([]types.Type{types.Self}, []types.Type{types.Integer})
	timeType.Methods["second"] = types.NewFunction([]types.Type{types.Self}, []types.Type{types.Integer})
	timeType.Methods["nanosecond"] = types.NewFunction([]types.Type{types.Self}, []types.Type{types.Integer})
	timeType.Methods["weekday"] = types.NewFunction([]types.Type{types.Self}, []types.Type{types.Integer})
	timeType.Methods["year_day"] = types.NewFunction([]types.Type{types.Self}, []types.Type{types.Integer})
	timeType.Methods["is_zero"] = types.NewFunction([]types.Type{types.Self}, []types.Type{types.Boolean})
	timeType.Methods["in_location"] = types.NewFunction([]types.Type{types.Self, locationType}, []types.Type{timeType})
	timeType.Methods["location"] = types.NewFunction([]types.Type{types.Self}, []types.Type{locationType})
	timeType.Methods["utc"] = types.NewFunction([]types.Type{types.Self}, []types.Type{timeType})
	timeType.Methods["in_local"] = types.NewFunction([]types.Type{types.Self}, []types.Type{timeType})
	timeType.Methods["round"] = types.NewFunction([]types.Type{types.Self, durationType}, []types.Type{timeType})
	timeType.Methods["truncate"] = types.NewFunction([]types.Type{types.Self, durationType}, []types.Type{timeType})
}

// Ticker type
var tickerType = &types.InterfaceType{
	Name: "time.Ticker",
	Methods: map[string]*types.FunctionType{
		"stop":     types.NewFunction([]types.Type{types.Self}, []types.Type{types.Boolean}),
		"response": types.NewFunction([]types.Type{types.Self}, []types.Type{types.Any}),
		"channel":  types.NewFunction([]types.Type{types.Self}, []types.Type{types.Any}),
	},
}

// Timer type
var timerType = &types.InterfaceType{
	Name: "time.Timer",
	Methods: map[string]*types.FunctionType{
		"stop":     types.NewFunction([]types.Type{types.Self}, []types.Type{types.Boolean}),
		"reset":    types.NewFunction([]types.Type{types.Self, types.Any}, []types.Type{types.Boolean}),
		"response": types.NewFunction([]types.Type{types.Self}, []types.Type{types.Any}),
		"channel":  types.NewFunction([]types.Type{types.Self}, []types.Type{types.Any}),
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
			"NANOSECOND":  types.Integer,
			"MICROSECOND": types.Integer,
			"MILLISECOND": types.Integer,
			"SECOND":      types.Integer,
			"MINUTE":      types.Integer,
			"HOUR":        types.Integer,
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
			"JANUARY":     types.Integer,
			"FEBRUARY":    types.Integer,
			"MARCH":       types.Integer,
			"APRIL":       types.Integer,
			"MAY":         types.Integer,
			"JUNE":        types.Integer,
			"JULY":        types.Integer,
			"AUGUST":      types.Integer,
			"SEPTEMBER":   types.Integer,
			"OCTOBER":     types.Integer,
			"NOVEMBER":    types.Integer,
			"DECEMBER":    types.Integer,
			"SUNDAY":      types.Integer,
			"MONDAY":      types.Integer,
			"TUESDAY":     types.Integer,
			"WEDNESDAY":   types.Integer,
			"THURSDAY":    types.Integer,
			"FRIDAY":      types.Integer,
			"SATURDAY":    types.Integer,
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
