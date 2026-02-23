<!-- SPDX-License-Identifier: MPL-2.0 -->

# time

Time operations, scheduling, timers, and duration handling. Time, nondeterministic.

## Loading

```lua
local time = require("time")
```

## Constants

```lua
-- Duration constants (nanoseconds)
time.NANOSECOND   -- 1
time.MICROSECOND  -- 1000
time.MILLISECOND  -- 1000000
time.SECOND       -- 1000000000
time.MINUTE       -- 60000000000
time.HOUR         -- 3600000000000

-- Format constants
time.RFC3339       -- "2006-01-02T15:04:05Z07:00"
time.RFC3339NANO   -- "2006-01-02T15:04:05.999999999Z07:00"
time.RFC822        -- "02 Jan 06 15:04 MST"
time.RFC822Z       -- "02 Jan 06 15:04 -0700"
time.RFC850        -- "Monday, 02-Jan-06 15:04:05 MST"
time.RFC1123       -- "Mon, 02 Jan 2006 15:04:05 MST"
time.RFC1123Z      -- "Mon, 02 Jan 2006 15:04:05 -0700"
time.KITCHEN       -- "3:04PM"
time.STAMP         -- "Jan _2 15:04:05"
time.STAMP_MILLI   -- "Jan _2 15:04:05.000"
time.STAMP_MICRO   -- "Jan _2 15:04:05.000000"
time.STAMP_NANO    -- "Jan _2 15:04:05.000000000"
time.DATE_TIME     -- "2006-01-02 15:04:05"
time.DATE_ONLY     -- "2006-01-02"
time.TIME_ONLY     -- "15:04:05"

-- Month constants
time.JANUARY    -- 1
time.FEBRUARY   -- 2
time.MARCH      -- 3
time.APRIL      -- 4
time.MAY        -- 5
time.JUNE       -- 6
time.JULY       -- 7
time.AUGUST     -- 8
time.SEPTEMBER  -- 9
time.OCTOBER    -- 10
time.NOVEMBER   -- 11
time.DECEMBER   -- 12

-- Weekday constants
time.SUNDAY     -- 0
time.MONDAY     -- 1
time.TUESDAY    -- 2
time.WEDNESDAY  -- 3
time.THURSDAY   -- 4
time.FRIDAY     -- 5
time.SATURDAY   -- 6

-- Location constants (userdata)
time.utc        -- UTC timezone Location
time.localtz    -- Local timezone Location
```

## Dependencies

### Channel (from engine)

Ticker and Timer types use channels for receiving ticks/fires. After returns a channel directly.

| Method | Signature | Returns |
|--------|-----------|---------|
| receive | () | value, ok: boolean |
| case_receive | () | case table for channel.select |

See: runtime/lua/engine/spec.md

## Functions

### sleep(duration: integer|string|Duration) → -

Pauses execution for the specified duration.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| duration | integer\|string\|Duration | yes | - | Nanoseconds, Go duration string ("5s"), or Duration object |

**Yields:** until duration elapsed

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| invalid duration type | errors.INVALID | no |
| duration parse error | errors.INVALID | no |

```lua
time.sleep("5s")
time.sleep(5 * time.SECOND)
```

### now() → Time

Returns current time.

**Returns:** Time object representing current moment

```lua
local t = time.now()
```

### date(year: integer, month: integer, day: integer, hour: integer, minute: integer, second: integer, nanosecond: integer, location?: Location) → Time

Constructs a Time from date/time components.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| year | integer | yes | - | Year |
| month | integer | yes | - | Month (1-12, use time.JANUARY etc) |
| day | integer | yes | - | Day of month (1-31) |
| hour | integer | yes | - | Hour (0-23) |
| minute | integer | yes | - | Minute (0-59) |
| second | integer | yes | - | Second (0-59) |
| nanosecond | integer | yes | - | Nanosecond (0-999999999) |
| location | Location | no | time.localtz | Timezone |

**Returns:** Time object

```lua
local t = time.date(2024, 12, 29, 15, 4, 5, 0, time.utc)
```

### unix(sec: integer, nsec: integer) → Time

Constructs a Time from Unix timestamp.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| sec | integer | yes | - | Unix seconds since epoch |
| nsec | integer | yes | - | Nanoseconds offset |

**Returns:** Time object

```lua
local t = time.unix(1735484645, 0)
```

### parse(layout: string, value: string, location?: Location) → Time, error

Parses a time string using a layout format.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| layout | string | yes | - | Go time format layout |
| value | string | yes | - | Time string to parse |
| location | Location | no | time.localtz | Default timezone if not in string |

**Returns:**
- Success: `Time` object
- Error: `nil, error` - structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| parse failed | errors.INTERNAL | no |

```lua
local t, err = time.parse("2006-01-02 15:04:05", "2024-12-29 15:04:05")
local t2, err = time.parse(time.RFC3339, "2024-12-29T15:04:05Z")
```

### parse_duration(value: integer|string|Duration) → Duration, error

Parses a duration from string, number, or Duration object.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| value | integer\|string\|Duration | yes | - | Nanoseconds, Go duration ("1h30m"), or Duration |

**Returns:**
- Success: `Duration` object
- Error: `nil, error` - structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| invalid type | errors.INVALID | no |
| parse failed | errors.INTERNAL | no |

```lua
local d, err = time.parse_duration("1h30m")
local d2, err = time.parse_duration(time.SECOND)
```

### load_location(name: string) → Location, error

Loads a timezone by IANA name.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| name | string | yes | - | IANA timezone name (e.g., "America/New_York") |

**Returns:**
- Success: `Location` object
- Error: `nil, error` - structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| empty name | errors.INVALID | no |
| location not found | errors.INTERNAL | no |

```lua
local loc, err = time.load_location("America/New_York")
```

### fixed_zone(name: string, offset: integer) → Location

Creates a Location with fixed UTC offset.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| name | string | yes | - | Timezone name |
| offset | integer | yes | - | Offset from UTC in seconds |

**Returns:** Location object

```lua
local loc = time.fixed_zone("EST", -5*3600)
```

### ticker(duration: integer|string|Duration) → Ticker, error

Creates a ticker that fires at regular intervals.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| duration | integer\|string\|Duration | yes | - | Tick interval (must be > 0) |

**Returns:**
- Success: `Ticker` object
- Error: `nil, error` - structured error

**Yields:** until ticker created (does not wait for first tick)

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| invalid duration | errors.INVALID | no |
| duration <= 0 | errors.INVALID | no |
| no context | errors.INVALID | no |
| no PID | errors.INVALID | no |
| ticker start failed | errors.INTERNAL | no |

```lua
local ticker = time.ticker("5ms")
local tick = ticker:response():receive()
ticker:stop()
```

### timer(duration: integer|string|Duration) → Timer, error

Creates a timer that fires once after duration.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| duration | integer\|string\|Duration | yes | - | Time until fire (must be > 0) |

**Returns:**
- Success: `Timer` object
- Error: `nil, error` - structured error

**Yields:** until timer created (does not wait for fire)

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| invalid duration | errors.INVALID | no |
| duration <= 0 | errors.INVALID | no |
| no context | errors.INVALID | no |
| no PID | errors.INVALID | no |
| timer start failed | errors.INTERNAL | no |

```lua
local timer = time.timer("10ms")
local fire_time = timer:response():receive()
```

### after(duration: integer|string|Duration) → Channel, error

Returns a channel that receives once after duration. Convenience wrapper for timer.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| duration | integer\|string\|Duration | yes | - | Time until send (must be > 0) |

**Returns:**
- Success: `Channel` that receives Time when duration elapses
- Error: `nil, error` - structured error

**Yields:** until channel created (does not wait for send)

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| invalid duration | errors.INVALID | no |
| duration <= 0 | errors.INVALID | no |
| no context | errors.INVALID | no |
| no PID | errors.INVALID | no |

```lua
local ch = time.after("10ms")
local fire_time = ch:receive()

-- Use with channel.select
local result = channel.select{
    time.after("5ms"):case_receive(),
    some_other_channel:case_receive()
}
```

## Types

### Time

Represents a point in time.

| Method | Signature | Returns | Notes |
|--------|-----------|---------|-------|
| add | (duration: integer\|string\|Duration) | Time | Add duration |
| sub | (other: Time) | Duration | Subtract times, get duration |
| add_date | (years: integer, months: integer, days: integer) | Time | Add calendar units |
| after | (other: Time) | boolean | Check if after other |
| before | (other: Time) | boolean | Check if before other |
| equal | (other: Time) | boolean | Check if equal (ignores location) |
| format | (layout: string) | string | Format using Go layout |
| format_rfc3339 | () | string | Format as RFC3339 |
| unix | () | integer | Unix seconds since epoch |
| unix_nano | () | integer | Unix nanoseconds since epoch |
| date | () | year: integer, month: integer, day: integer | Get date components |
| clock | () | hour: integer, minute: integer, second: integer | Get time components |
| year | () | integer | Get year |
| month | () | integer | Get month (1-12) |
| day | () | integer | Get day of month |
| hour | () | integer | Get hour (0-23) |
| minute | () | integer | Get minute (0-59) |
| second | () | integer | Get second (0-59) |
| nanosecond | () | integer | Get nanosecond (0-999999999) |
| weekday | () | integer | Get weekday (0=Sunday, 6=Saturday) |
| year_day | () | integer | Get day of year (1-366) |
| is_zero | () | boolean | Check if zero time |
| in_location | (loc: Location) | Time | Convert to location |
| location | () | Location | Get current location |
| utc | () | Time | Convert to UTC |
| in_local | () | Time | Convert to local timezone |
| round | (duration: Duration) | Time | Round to nearest duration |
| truncate | (duration: Duration) | Time | Truncate to duration |

### Duration

Represents a time duration.

| Method | Signature | Returns | Notes |
|--------|-----------|---------|-------|
| nanoseconds | () | integer | Duration in nanoseconds |
| microseconds | () | integer | Duration in microseconds |
| milliseconds | () | integer | Duration in milliseconds |
| seconds | () | number | Duration in seconds (float) |
| minutes | () | number | Duration in minutes (float) |
| hours | () | number | Duration in hours (float) |

### Location

Represents a timezone.

| Method | Signature | Returns | Notes |
|--------|-----------|---------|-------|
| string | () | string | Location name |

### Ticker

Periodic timer that sends ticks on a channel.

| Method | Signature | Returns | Notes |
|--------|-----------|---------|-------|
| response | () | Channel | Get channel for receiving ticks (Time values) |
| channel | () | Channel | Alias for response (backwards compatibility) |
| stop | () | - | Stop ticker (yields) |

**Yields:** `stop()` yields until ticker stopped

**Notes:**
- First call to `response()` or `channel()` subscribes to ticker
- Subsequent calls return same channel
- Channel receives Time objects on each tick
- Stop ticker when done to prevent resource leaks
- Automatic cleanup on process exit

### Timer

One-shot timer that fires once after duration.

| Method | Signature | Returns | Notes |
|--------|-----------|---------|-------|
| response | () | Channel | Get channel for receiving fire (Time value) |
| channel | () | Channel | Alias for response (backwards compatibility) |
| stop | () | boolean | Stop timer before it fires (yields, returns true if stopped) |
| reset | (duration: integer\|string\|Duration) | - | Reset with new duration (yields) |

**Yields:**
- `stop()` yields until timer stopped
- `reset()` yields until timer reset

**Notes:**
- First call to `response()` or `channel()` subscribes to timer
- Subsequent calls return same channel
- Channel receives Time object when timer fires
- `stop()` returns true if timer was stopped before firing, false if already fired
- Automatic cleanup on process exit

## Errors

This module returns structured errors. Check kind with `errors.*` constants:

```lua
local timer, err = time.timer("5ms")
if err then
    if err:kind() == errors.INVALID then
        -- bad duration or parameters
    elseif err:kind() == errors.INTERNAL then
        -- timer creation failed
    end
end
```

**Possible kinds:** `errors.INVALID`, `errors.INTERNAL`

## Example

```lua
local time = require("time")

-- Sleep
time.sleep("1s")

-- Current time and formatting
local now = time.now()
print(now:format_rfc3339())

-- Date construction
local t = time.date(2024, 12, 29, 15, 4, 5, 0, time.utc)

-- Time arithmetic
local later = t:add("1h30m")
local diff = later:sub(t)
print(diff:minutes())  -- 90

-- Parse
local parsed, err = time.parse(time.RFC3339, "2024-12-29T15:04:05Z")
if err then error(err) end

-- Ticker - periodic ticks
local ticker = time.ticker("100ms")
for i = 1, 5 do
    local tick_time = ticker:response():receive()
    print("Tick:", tick_time:format_rfc3339())
end
ticker:stop()

-- Timer - one-shot
local timer = time.timer("500ms")
local fire_time = timer:response():receive()
print("Timer fired at:", fire_time:format_rfc3339())

-- After - simple delay with channel
local ch = time.after("1s")
local result_time = ch:receive()
print("Fired:", result_time:format_rfc3339())

-- Select with timeout
local result = channel.select{
    some_operation_channel:case_receive(),
    time.after("5s"):case_receive()  -- timeout case
}
if result.channel == timeout_ch then
    print("Operation timed out")
end
```
