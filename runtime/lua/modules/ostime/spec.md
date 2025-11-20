# Lua OS Time Module Specification

## Overview

The `os` module provides time and date functions compatible with standard Lua os.time, os.date, and os.clock functions.

## Module Interface

### Module Loading

```lua
local os = require("os")
```

### Functions

#### os.time(date_table?: table)

Returns the current time as a Unix timestamp, or converts a date table to a timestamp.

Parameters:
- `date_table`: Optional table with fields:
  - `year`: Year (required when table is provided)
  - `month`: Month (1-12, required when table is provided)
  - `day`: Day of month (1-31, required when table is provided)
  - `hour`: Hour (0-23, default: 0)
  - `min`: Minute (0-59, default: 0)
  - `sec`: Second (0-59, default: 0)

Returns:
- `timestamp`: Unix timestamp as a number

#### os.date(format?: string, time?: number)

Formats a time value as a date string.

Parameters:
- `format`: Optional format string (defaults to "%c")
  - Supports standard strftime format specifiers
  - Special format "*t" returns a table with date components
- `time`: Optional Unix timestamp (defaults to current time)

Returns:
- `formatted`: Formatted date string, or table if format is "*t"

#### os.clock()

Returns the elapsed time in seconds since the module was loaded.

Returns:
- `seconds`: Elapsed time as a number

## Example Usage

```lua
local os = require("os")

-- Get current Unix timestamp
local now = os.time()
print("Current timestamp:", now)

-- Convert date table to timestamp
local timestamp = os.time({
  year = 2024,
  month = 1,
  day = 15,
  hour = 14,
  min = 30,
  sec = 0
})
print("Specific date timestamp:", timestamp)

-- Format current time
print("Current date:", os.date())
print("Custom format:", os.date("%Y-%m-%d %H:%M:%S"))
print("ISO 8601:", os.date("%Y-%m-%dT%H:%M:%S"))

-- Get date table
local date_table = os.date("*t")
print("Year:", date_table.year)
print("Month:", date_table.month)
print("Day:", date_table.day)

-- Format specific timestamp
print("Formatted:", os.date("%c", timestamp))

-- Measure elapsed time
local start = os.clock()
-- ... do some work ...
local elapsed = os.clock() - start
print("Elapsed seconds:", elapsed)
```

## Format Specifiers

Common format specifiers for `os.date()`:
- `%Y`: Year (4 digits)
- `%m`: Month (01-12)
- `%d`: Day (01-31)
- `%H`: Hour (00-23)
- `%M`: Minute (00-59)
- `%S`: Second (00-59)
- `%c`: Complete date and time
- `%x`: Date
- `%X`: Time
- `*t`: Returns a table instead of a string

## Notes

- Compatible with standard Lua os library time functions
- `os.clock()` is useful for performance measurements
- Date tables use 1-based months (1 = January, 12 = December)
- All times are in UTC unless otherwise specified
