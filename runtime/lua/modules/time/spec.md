# Lua Time Module Specification

## Overview

The `time` module provides a Lua interface for time-related functionality. It allows Lua code to work with durations,
dates, and times, including parsing, formatting, and arithmetic operations. It also supports time zones and provides
constants for common time units and formats.

## Module Interface

### Module Loading

```lua
local time = require("time")
```

### Data Structures

#### Duration

A `Duration` represents the elapsed time between two instants as an int64 nanosecond count.

- **Type:** `userdata`
- **Metatable:** `Duration`

##### Duration Methods

- `nanoseconds() -> number`: Returns the duration as a floating-point number of nanoseconds.
- `microseconds() -> number`: Returns the duration as a floating-point number of microseconds.
- `milliseconds() -> number`: Returns the duration as a floating-point number of milliseconds.
- `seconds() -> number`: Returns the duration as a floating-point number of seconds.
- `minutes() -> number`: Returns the duration as a floating-point number of minutes.
- `hours() -> number`: Returns the duration as a floating-point number of hours.

##### Duration String Representation

- `tostring(duration: Duration) -> string`: Returns a string representation of the duration (e.g., "1h30m").

#### Location

A `Location` represents a time zone.

- **Type:** `userdata`
- **Metatable:** `Location`

##### Location Methods

- `string() -> string`: Returns the name of the location.

##### Location String Representation

- `tostring(location: Location) -> string`: Returns the name of the location.

#### Time

A `Time` represents an instant in time with nanosecond precision.

- **Type:** `userdata`
- **Metatable:** `Time`

##### Time Methods

- `add(duration: Duration) -> Time`: Returns the time resulting from adding the given duration to the time.
- `sub(other: Time) -> Duration`: Returns the duration between the time and another time.
- `add_date(years: number, months: number, days: number) -> Time`: Returns the time resulting from adding the given
  years, months, and days to the time.
- `after(other: Time) -> boolean`: Returns true if the time is after the other time.
- `before(other: Time) -> boolean`: Returns true if the time is before the other time.
- `equal(other: Time) -> boolean`: Returns true if the time is equal to the other time.
- `format(layout: string) -> string`: Returns a string representation of the time formatted according to the given
  layout string.
- `format_rfc3339() -> string`: Returns a string representation of the time formatted according to RFC3339.
- `unix() -> number`: Returns the time as a Unix timestamp (seconds since January 1, 1970 UTC).
- `unix_nano() -> number`: Returns the time as a Unix timestamp in nanoseconds.
- `date() -> year: number, month: number, day: number`: Returns the year, month, and day of the time.
- `clock() -> hour: number, min: number, sec: number`: Returns the hour, minute, and second of the time.
- `year() -> number`: Returns the year of the time.
- `month() -> number`: Returns the month of the time.
- `day() -> number`: Returns the day of the time.
- `hour() -> number`: Returns the hour of the time.
- `minute() -> number`: Returns the minute of the time.
- `second() -> number`: Returns the second of the time.
- `nanosecond() -> number`: Returns the nanosecond of the time.
- `weekday() -> number`: Returns the day of the week of the time (0 = Sunday, 1 = Monday, ...).
- `year_day() -> number`: Returns the day of the year of the time.
- `is_zero() -> boolean`: Returns true if the time represents the zero time instant.
- `in_location(location: Location) -> Time`: Returns a new Time representing the same instant but in the given location.
- `location() -> Location`: Returns the Location associated with the time.
- `utc() -> Time`: Returns a new Time representing the same instant but in the UTC location.
- `in_local() -> Time`: Returns a new Time representing the same instant but in the Local location.
- `round(duration: Duration) -> Time`: Returns the result of rounding the time to the nearest multiple of the given
  duration.
- `truncate(duration: Duration) -> Time`: Returns the result of truncating the time to the nearest multiple of the
  given duration.

##### Time String Representation

- `tostring(time: Time) -> string`: Returns a string representation of the time.

### Global Functions

#### time.parse_duration(str: string) -> Duration, error: string | nil

Parses a duration string.

Parameters:

- `str`: The duration string to parse (e.g., "1h30m").

Returns:

- `duration`: The parsed duration.
- `error`: An error message if parsing fails, or `nil` on success.

#### time.load_location(name: string) -> Location, error: string | nil

Loads a location by name.

Parameters:

- `name`: The name of the location to load (e.g., "America/New_York").

Returns:

- `location`: The loaded location.
- `error`: An error message if loading fails, or `nil` on success.

#### time.fixed_zone(name: string, offset: number) -> Location

Creates a fixed time zone with the given name and offset in seconds east of UTC.

Parameters:

- `name`: The name of the fixed zone.
- `offset`: The offset in seconds east of UTC.

Returns:

- `location`: The created fixed zone location.

#### time.now() -> Time

Returns the current time.

#### time.sleep(duration: Duration) -> error: string | nil

Pauses the current execution for the specified duration.

Parameters:
- `duration`: The duration to sleep. Must be a Duration userdata object.

Returns:
- `error`: An error message string in the following cases, or `nil` on success:
  - "duration expected" if the argument is not a Duration userdata object
  - The context error message if a context is available and cancelled during sleep

Example:
```lua
local time = require("time")
local duration = time.parse_duration("1s")
local err = time.sleep(duration)  -- sleeps for 1 second
if err then
    error("Sleep interrupted: " .. err)
end

-- Error cases
local err = time.sleep("1s")        -- Errors with "duration expected"
local err = time.sleep(123)         -- Errors with "duration expected"
local err = time.sleep({})          -- Errors with "duration expected"

-- With context cancellation
-- If running in a context that gets cancelled:
local err = time.sleep(duration)    -- err will contain context cancellation message
```

Remarks:
- The function requires a proper Duration object created via time.parse_duration() or other time module methods
- Any non-Duration argument will result in an immediate "duration expected" error
- Unlike other time functions that may have lenient type coercion, sleep() strictly enforces Duration type for safety
- Context cancellation errors take precedence over type errors

#### time.date(year: number, month: number, day: number, hour: number, min: number, sec: number, nsec: number, loc: Location | nil) -> Time

Creates a new time with the given components.

Parameters:

- `year`: The year.
- `month`: The month (1-12).
- `day`: The day (1-31).
- `hour`: The hour (0-23).
- `min`: The minute (0-59).
- `sec`: The second (0-59).
- `nsec`: The nanosecond (0-999999999).
- `loc`: The location. If nil, the local location is used.

Returns:

- `time`: The created time.

#### time.unix(sec: number, nsec: number) -> Time

Creates a new time from a Unix timestamp.

Parameters:

- `sec`: The seconds since January 1, 1970 UTC.
- `nsec`: The nanoseconds.

Returns:

- `time`: The created time.

#### time.parse(layout: string, value: string, loc: Location | nil) -> Time, error: string | nil

Parses a time string according to the given layout.

Parameters:

- `layout`: The layout string describing the format of the time string.
- `value`: The time string to parse.
- `loc`: The location to use for parsing. If nil, the local location is used.

Returns:

- `time`: The parsed time.
- `error`: An error message if parsing fails, or `nil` on success.

### Constants

#### Duration Constants

- `time.NANOSECOND`: 1 (number)
- `time.MICROSECOND`: 1000 (number)
- `time.MILLISECOND`: 1000000 (number)
- `time.SECOND`: 1000000000 (number)
- `time.MINUTE`: 60000000000 (number)
- `time.HOUR`: 3600000000000 (number)

#### Location Constants

- `time.utc`: A `Location` representing Coordinated Universal Time (UTC).
- `time.localtz`: A `Location` representing the local time zone.

#### Format Constants

- `time.RFC3339`: "2006-01-02T15:04:05Z07:00" (string)
- `time.RFC3339Nano`: "2006-01-02T15:04:05.999999999Z07:00" (string)
- `time.RFC822`: "02 Jan 06 15:04 MST" (string)
- `time.RFC822Z`: "02 Jan 06 15:04 -0700" (string)
- `time.RFC850`: "Monday, 02-Jan-06 15:04:05 MST" (string)
- `time.RFC1123`: "Mon, 02 Jan 2006 15:04:05 MST" (string)
- `time.RFC1123Z`: "Mon, 02 Jan 2006 15:04:05 -0700" (string)
- `time.Kitchen`: "3:04PM" (string)
- `time.Stamp`: "Jan _2 15:04:05" (string)
- `time.StampMilli`: "Jan _2 15:04:05.000" (string)
- `time.StampMicro`: "Jan _2 15:04:05.000000" (string)
- `time.StampNano`: "Jan _2 15:04:05.000000000" (string)
- `time.DateTime`: "2006-01-02 15:04:05" (string)
- `time.DateOnly`: "2006-01-02" (string)
- `time.TimeOnly`: "15:04:05" (string)

#### Month Constants

- `time.JANUARY`: 1 (number)
- `time.FEBRUARY`: 2 (number)
- `time.MARCH`: 3 (number)
- `time.APRIL`: 4 (number)
- `time.MAY`: 5 (number)
- `time.JUNE`: 6 (number)
- `time.JULY`: 7 (number)
- `time.AUGUST`: 8 (number)
- `time.SEPTEMBER`: 9 (number)
- `time.OCTOBER`: 10 (number)
- `time.NOVEMBER`: 11 (number)
- `time.DECEMBER`: 12 (number)

#### Weekday Constants

- `time.SUNDAY`: 0 (number)
- `time.MONDAY`: 1 (number)
- `time.TUESDAY`: 2 (number)
- `time.WEDNESDAY`: 3 (number)
- `time.THURSDAY`: 4 (number)
- `time.FRIDAY`: 5 (number)
- `time.SATURDAY`: 6 (number)

## Error Handling

- Functions that can fail return an error string as their last return value. On success, the error string is `nil`.
- `time.parse_duration` returns an error if the input string is not a valid duration.
- `time.load_location` returns an error if the location cannot be loaded.
- `time.parse` returns an error if the input string does not match the layout or is invalid.
- `time.sleep` returns an error if called with a context that gets cancelled during the sleep.
- Methods on `Duration`, `Location`, and `Time` objects report errors by raising a Lua error if called on an invalid
  object or with incorrect arguments.

## Thread Safety

- The `time` module is thread-safe.
- `time.sleep` can be safely used with contexts in a multi-threaded environment.

## Best Practices

- Always check for errors returned by functions.
- Use the provided constants for duration units, locations, and time formats.
- Prefer `time.format` with layout strings over manual string concatenation for formatting times.
- Use `time.parse_duration` and `time.parse` for parsing durations and times from strings.
- Use `time.date` and `time.unix` for creating `Time` objects.
- Use the methods on `Time` objects for time arithmetic and manipulation.
- Be mindful of time zones when working with times from different locations.
- When using `time.sleep` in a context-aware environment, handle potential context cancellation errors.

## Example Usage

```lua
local time = require("time")

-- Duration
local duration = time.parse_duration("1h30m")
print(duration:hours()) --> 1.5
print(tostring(duration)) --> 1h30m0s

-- Location
local loc, err = time.load_location("America/New_York")
if err then
    error("Failed to load location: " .. err)
end
print(loc:string()) --> America/New_York

-- Time
local now = time.now()
print("Current time:", now)

local specificTime = time.date(2024, time.DECEMBER, 25, 10, 30, 0, 0, time.utc)
print("Specific time:", specificTime)

local unixTime = time.unix(1672531200, 0)
print("Unix time:", unixTime)

local parsedTime, err = time.parse(time.RFC3339, "2023-01-01T00:00:00Z")
if err then
    error("Failed to parse time: " .. err)
end
print("Parsed time:", parsedTime)

-- Time methods
local later = now:add(duration)
print("Later time:", later)

local diff = later:sub(now)
print("Difference:", diff)

print("Year:", now:year())
print("Month:", now:month())
print("Day:", now:day())

print("Formatted:", now:format("2006-01-02 15:04:05"))
print("RFC3339:", now:format_rfc3339())

-- Sleep
local sleepDuration = time.parse_duration("500ms")
local sleepErr = time.sleep(sleepDuration)
if sleepErr then
	print("Sleep error: ", sleepErr)
end
```
