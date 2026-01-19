package time

import (
	"github.com/wippyai/runtime/runtime/lua/engine"
	"github.com/yuin/gopher-lua/types/io"
	"github.com/yuin/gopher-lua/types/typ"
)

// Forward declarations for mutually referential types
var (
	timeType     typ.Type
	durationType typ.Type
	locationType typ.Type
	tickerType   typ.Type
	timerType    typ.Type
	channelType  typ.Type
	channelGen   *typ.Generic
)

func init() {
	if manifest := engine.ChannelModuleTypes(); manifest != nil {
		if t, ok := manifest.LookupType("Channel"); ok {
			if gen, ok := t.(*typ.Generic); ok {
				channelGen = gen
			} else {
				channelType = t
			}
		}
	}
	if channelType == nil && channelGen == nil {
		channelType = typ.Any
	}

	// Duration type
	durationType = typ.NewInterface("time.Duration", []typ.Method{
		{Name: "nanoseconds", Type: typ.Func().Param("self", typ.Self).Returns(typ.Number).Build()},
		{Name: "microseconds", Type: typ.Func().Param("self", typ.Self).Returns(typ.Number).Build()},
		{Name: "milliseconds", Type: typ.Func().Param("self", typ.Self).Returns(typ.Number).Build()},
		{Name: "seconds", Type: typ.Func().Param("self", typ.Self).Returns(typ.Number).Build()},
		{Name: "minutes", Type: typ.Func().Param("self", typ.Self).Returns(typ.Number).Build()},
		{Name: "hours", Type: typ.Func().Param("self", typ.Self).Returns(typ.Number).Build()},
	})

	// Location type
	locationType = typ.NewInterface("time.Location", []typ.Method{
		{Name: "string", Type: typ.Func().Param("self", typ.Self).Returns(typ.String).Build()},
	})

	// Time type (self-referential and references duration/location)
	timeType = typ.NewInterface("time.Time", []typ.Method{
		{Name: "add", Type: typ.Func().Param("self", typ.Self).Param("d", typ.Any).Returns(typ.Self).Build()},
		{Name: "sub", Type: typ.Func().Param("self", typ.Self).Param("t", typ.Self).Returns(durationType).Build()},
		{Name: "add_date", Type: typ.Func().Param("self", typ.Self).Param("year", typ.Number).Param("month", typ.Number).Param("day", typ.Number).Returns(typ.Self).Build()},
		{Name: "after", Type: typ.Func().Param("self", typ.Self).Param("t", typ.Self).Returns(typ.Boolean).Build()},
		{Name: "before", Type: typ.Func().Param("self", typ.Self).Param("t", typ.Self).Returns(typ.Boolean).Build()},
		{Name: "equal", Type: typ.Func().Param("self", typ.Self).Param("t", typ.Self).Returns(typ.Boolean).Build()},
		{Name: "format", Type: typ.Func().Param("self", typ.Self).Param("format", typ.String).Returns(typ.String).Build()},
		{Name: "format_rfc3339", Type: typ.Func().Param("self", typ.Self).Returns(typ.String).Build()},
		{Name: "unix", Type: typ.Func().Param("self", typ.Self).Returns(typ.Integer).Build()},
		{Name: "unix_nano", Type: typ.Func().Param("self", typ.Self).Returns(typ.Integer).Build()},
		{Name: "date", Type: typ.Func().Param("self", typ.Self).Returns(typ.Integer, typ.Integer, typ.Integer).Build()},
		{Name: "clock", Type: typ.Func().Param("self", typ.Self).Returns(typ.Integer, typ.Integer, typ.Integer).Build()},
		{Name: "year", Type: typ.Func().Param("self", typ.Self).Returns(typ.Integer).Build()},
		{Name: "month", Type: typ.Func().Param("self", typ.Self).Returns(typ.Integer).Build()},
		{Name: "day", Type: typ.Func().Param("self", typ.Self).Returns(typ.Integer).Build()},
		{Name: "hour", Type: typ.Func().Param("self", typ.Self).Returns(typ.Integer).Build()},
		{Name: "minute", Type: typ.Func().Param("self", typ.Self).Returns(typ.Integer).Build()},
		{Name: "second", Type: typ.Func().Param("self", typ.Self).Returns(typ.Integer).Build()},
		{Name: "nanosecond", Type: typ.Func().Param("self", typ.Self).Returns(typ.Integer).Build()},
		{Name: "weekday", Type: typ.Func().Param("self", typ.Self).Returns(typ.Integer).Build()},
		{Name: "year_day", Type: typ.Func().Param("self", typ.Self).Returns(typ.Integer).Build()},
		{Name: "is_zero", Type: typ.Func().Param("self", typ.Self).Returns(typ.Boolean).Build()},
		{Name: "in_location", Type: typ.Func().Param("self", typ.Self).Param("loc", locationType).Returns(typ.Self).Build()},
		{Name: "location", Type: typ.Func().Param("self", typ.Self).Returns(locationType).Build()},
		{Name: "utc", Type: typ.Func().Param("self", typ.Self).Returns(typ.Self).Build()},
		{Name: "in_local", Type: typ.Func().Param("self", typ.Self).Returns(typ.Self).Build()},
		{Name: "round", Type: typ.Func().Param("self", typ.Self).Param("d", durationType).Returns(typ.Self).Build()},
		{Name: "truncate", Type: typ.Func().Param("self", typ.Self).Param("d", durationType).Returns(typ.Self).Build()},
	})

	if channelGen != nil {
		channelType = typ.Instantiate(channelGen, timeType)
	}

	// Ticker type
	tickerType = typ.NewInterface("time.Ticker", []typ.Method{
		{Name: "stop", Type: typ.Func().Param("self", typ.Self).Returns(typ.Boolean).Build()},
		{Name: "response", Type: typ.Func().Param("self", typ.Self).Returns(channelType).Build()},
		{Name: "channel", Type: typ.Func().Param("self", typ.Self).Returns(channelType).Build()},
	})

	// Timer type
	timerType = typ.NewInterface("time.Timer", []typ.Method{
		{Name: "stop", Type: typ.Func().Param("self", typ.Self).Returns(typ.Boolean).Build()},
		{Name: "reset", Type: typ.Func().Param("self", typ.Self).Param("d", typ.Any).Returns(typ.Boolean).Build()},
		{Name: "response", Type: typ.Func().Param("self", typ.Self).Returns(channelType).Build()},
		{Name: "channel", Type: typ.Func().Param("self", typ.Self).Returns(channelType).Build()},
	})
}

// ModuleTypes returns the type manifest for the time module.
func ModuleTypes() *io.Manifest {
	m := io.NewManifest("time")

	m.DefineType("Time", timeType)
	m.DefineType("Duration", durationType)
	m.DefineType("Location", locationType)
	m.DefineType("Ticker", tickerType)
	m.DefineType("Timer", timerType)

	moduleType := typ.NewRecord().
		Field("NANOSECOND", typ.Number).
		Field("MICROSECOND", typ.Number).
		Field("MILLISECOND", typ.Number).
		Field("SECOND", typ.Number).
		Field("MINUTE", typ.Number).
		Field("HOUR", typ.Number).
		Field("RFC3339", typ.String).
		Field("RFC3339NANO", typ.String).
		Field("RFC822", typ.String).
		Field("RFC822Z", typ.String).
		Field("RFC850", typ.String).
		Field("RFC1123", typ.String).
		Field("RFC1123Z", typ.String).
		Field("KITCHEN", typ.String).
		Field("STAMP", typ.String).
		Field("STAMP_MILLI", typ.String).
		Field("STAMP_MICRO", typ.String).
		Field("STAMP_NANO", typ.String).
		Field("DATE_TIME", typ.String).
		Field("DATE_ONLY", typ.String).
		Field("TIME_ONLY", typ.String).
		Field("JANUARY", typ.Number).
		Field("FEBRUARY", typ.Number).
		Field("MARCH", typ.Number).
		Field("APRIL", typ.Number).
		Field("MAY", typ.Number).
		Field("JUNE", typ.Number).
		Field("JULY", typ.Number).
		Field("AUGUST", typ.Number).
		Field("SEPTEMBER", typ.Number).
		Field("OCTOBER", typ.Number).
		Field("NOVEMBER", typ.Number).
		Field("DECEMBER", typ.Number).
		Field("SUNDAY", typ.Number).
		Field("MONDAY", typ.Number).
		Field("TUESDAY", typ.Number).
		Field("WEDNESDAY", typ.Number).
		Field("THURSDAY", typ.Number).
		Field("FRIDAY", typ.Number).
		Field("SATURDAY", typ.Number).
		Field("utc", locationType).
		Field("localtz", locationType).
		Field("sleep", typ.Func().Param("d", typ.Any).Build()).
		Field("timer", typ.Func().Param("d", typ.Any).Returns(timerType, typ.NewOptional(typ.LuaError)).Build()).
		Field("after", typ.Func().Param("d", typ.Any).Returns(channelType).Build()).
		Field("ticker", typ.Func().Param("d", typ.Any).Returns(tickerType, typ.NewOptional(typ.LuaError)).Build()).
		Field("now", typ.Func().Returns(timeType).Build()).
		Field("date", typ.Func().Param("year", typ.Number).Param("month", typ.Number).Param("day", typ.Number).Param("hour", typ.Number).Param("min", typ.Number).Param("sec", typ.Number).Param("nsec", typ.Number).OptParam("loc", locationType).Returns(timeType).Build()).
		Field("unix", typ.Func().Param("sec", typ.Number).Param("nsec", typ.Number).Returns(timeType).Build()).
		Field("parse", typ.Func().Param("layout", typ.String).Param("value", typ.String).OptParam("loc", locationType).Returns(timeType, typ.NewOptional(typ.LuaError)).Build()).
		Field("parse_duration", typ.Func().Param("s", typ.Any).Returns(durationType, typ.NewOptional(typ.LuaError)).Build()).
		Field("load_location", typ.Func().Param("name", typ.String).Returns(locationType, typ.NewOptional(typ.LuaError)).Build()).
		Field("fixed_zone", typ.Func().Param("name", typ.String).Param("offset", typ.Number).Returns(locationType).Build()).
		Build()

	m.SetExport(moduleType)
	return m
}
