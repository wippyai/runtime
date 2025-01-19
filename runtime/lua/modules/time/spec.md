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

### time.sleep

The `time.sleep` function pauses execution for the specified duration. The function handles context cancellation and supports multiple input types for duration.

#### Syntax

```lua
local err = time.sleep(duration)  -- duration can be Duration object or string
```

#### Parameters

- `duration`: Can be one of:
  - Duration object (from time.parse_duration)
  - String (valid duration format, e.g., "100ms")

#### Returns

- `string|nil`: Returns an error message string if the sleep was interrupted (e.g., by context cancellation) or if the duration was invalid. Returns nil on successful completion.

#### Error Cases

Returns error message string in these cases:
- Invalid duration format when using string duration
- Context cancellation during sleep
- Invalid duration type (not Duration or string)

#### Special Behavior

- In coroutine-enabled VMs, sleep uses coroutine.Wrap for non-blocking behavior
- When context is available, uses context-aware timer to support cancellation
- When no context is available, uses basic time.Sleep

#### Examples

Basic usage:
```lua
local time = require("time")

-- Sleep with duration object
local duration = time.parse_duration("500ms")
local err = time.sleep(duration)
if err then
    error("Sleep interrupted: " .. err)
end

-- Sleep with duration string
local err = time.sleep("100ms")
if err then
    error("Sleep interrupted: " .. err)
end
```

Error handling:
```lua
local time = require("time")

-- Invalid duration string
local err = time.sleep("invalid")
assert(err ~= nil) -- err contains "time: invalid duration"

-- Invalid type
local success, err = pcall(function()
    time.sleep(123)  -- Numbers not supported, must be string or Duration
end)
assert(not success) -- err contains "duration or string expected"
```

With context cancellation:
```lua
local time = require("time")

-- If context gets cancelled during sleep
local err = time.sleep("5s")
if err and err:find("context canceled") then
    print("Sleep was interrupted by context cancellation")
end
```

In coroutines:
```lua
local time = require("time")

coroutine.spawn(function()
    local err = time.sleep("100ms")
    if not err then
        print("Slept for 100ms")
    end
end)
```

#### Implementation Notes

- Context cancellation takes precedence over other error conditions
- Differs from time.after by being a blocking operation rather than channel-based
- Does not support direct millisecond numbers unlike time.after - must use string or Duration object

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

### time.after

The `time.after` function creates a one-time channel that will receive a signal after a specified duration. This is
similar to Timer but with a simpler interface focused on one-time wait operations.

#### Syntax

```lua
local ch = time.after(duration)  -- duration can be Duration object, string, or number (ms)
```

#### Parameters

- `duration`: Can be one of:
    - Duration object (from time.parse_duration)
    - String (valid duration format, e.g., "100ms")
    - Number (interpreted as milliseconds)

#### Returns

- `Channel`: A channel that will receive a boolean (true) once after the specified duration

#### Errors

Raises an error in these cases:

- Invalid duration format when using string duration
- Non-positive duration value
- Invalid duration type (not Duration, string, or number)
- Context validation errors

#### Examples

Basic usage:

```lua
local time = require("time")

-- Wait for 100ms
local ch = time.after("100ms")
ch:receive()  -- Blocks for 100ms, then receives true

-- Using with duration object
local duration = time.parse_duration("500ms")
local ch = time.after(duration)
ch:receive()  -- Blocks for 500ms

-- Using milliseconds directly
local ch = time.after(250)  -- 250ms
ch:receive()
```

With channel select:

```lua
local time = require("time")
local channel = require("channel")

local timeout = time.after("1s")
local done = channel.new(0)

local result = channel.select{
    timeout:case_receive(),
    done:case_receive()
}

if result.channel == timeout then
    print("Operation timed out")
else
    print("Operation completed")
end
```

With coroutines:

```lua
local time = require("time")

coroutine.spawn(function()
    local ch = time.after("100ms")
    ch:receive()
    print("100ms have passed")
end)
```

Context cancellation:

```lua
-- If the context is cancelled before duration elapses:
local ch = time.after("1s")
-- Context cancellation will cause receive to return with error
local success, err = pcall(function()
    ch:receive()
end)
```

#### Implementation Notes

- The channel is automatically closed after sending the signal
- The channel has a buffer size of 1
- If context is cancelled before duration elapses, the channel is closed without sending
- Creates a unique channel name using the format "timer_[duration]"
- Unlike Timer, there's no way to stop or reset the after operation

### Timer

A `Timer` represents a single event that will occur after a specified duration.

- **Type:** `userdata`
- **Metatable:** `Timer`

#### Timer Methods

- `stop() -> boolean`: Stops the timer and returns whether it was stopped before being triggered.
- `reset(duration: Duration|string|number) -> boolean`: Resets the timer with a new duration. Returns whether the timer
  was active when reset was called.
    - If duration is a number, it's interpreted as milliseconds.
    - If duration is a string, it must be a valid duration string (e.g., "100ms").
- `channel() -> Channel`: Returns a channel that will receive a Time object when the timer triggers.

#### Timer Creation

```lua
local timer = time.timer(duration)  -- duration can be Duration object, string, or number (ms)
```

#### Timer Example

```lua
local time = require("time")
local timer = time.timer("100ms")  -- Create timer for 100ms
local ch = timer:channel()         -- Get the timer's channel
local t = ch:receive()            -- Wait for timer to trigger, returns Time object

-- With reset
timer:reset("200ms")              -- Reset timer for 200ms
local was_active = timer:stop()   -- Stop the timer
```

### Ticker

A `Ticker` represents a periodic event that occurs at regular intervals.

- **Type:** `userdata`
- **Metatable:** `Ticker`

#### Ticker Methods

- `stop()`: Stops the ticker from sending more events.
- `channel() -> Channel`: Returns a channel that will receive a Time object each time the ticker triggers.

#### Ticker Creation

```lua
local ticker = time.ticker(duration)  -- duration can be Duration object, string, or number (ms)
```

#### Ticker Example

```lua
local time = require("time")
local ticker = time.ticker("100ms")  -- Create ticker for every 100ms
local ch = ticker:channel()          -- Get the ticker's channel

-- Receive first 3 ticks
for i = 1, 3 do
    local t = ch:receive()           -- Each receive gets a Time object
    print("Tick at:", t:format_rfc3339())
end

ticker:stop()                        -- Stop the ticker
```

### Channel Integration

Both Timer and Ticker can be used with channel operations like `select`:

```lua
local time = require("time")
local channel = require("channel")

local timer = time.timer("100ms")
local done = channel.new(0)

local result = channel.select{
    timer:channel():case_receive(),
    done:case_receive()
}

if result.channel == timer:channel() then
    print("Timer triggered")
else
    print("Done channel received")
end
```

### Error Handling

Timer and Ticker construction will raise errors in the following cases:

- Invalid duration format when using string duration
- Non-positive duration value
- Invalid duration type (not Duration, string, or number)
- Context validation errors

Example error handling:

```lua
local time = require("time")

-- These will raise errors
local success, err = pcall(function()
    time.timer(-100)         -- Error: duration must be > 0
    time.timer("invalid")    -- Error: invalid duration format
    time.timer({})          -- Error: invalid type
end)

-- Reset errors
local success, err = pcall(function()
    local t = time.timer("100ms")
    t:reset(-50)           -- Error: duration must be > 0
end)
```

### Context Cancellation

Both Timer and Ticker respect context cancellation:

- If the context is cancelled before a timer triggers, the channel will be closed
- For tickers, no more events will be sent after context cancellation
- Channel operations (receive) will return with context cancellation errors

```lua
-- Timer with context cancellation
local timer = time.timer("1s")
local ch = timer:channel()
local success, err = pcall(function()
    ch:receive()  -- Will error if context is cancelled
end)
```

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