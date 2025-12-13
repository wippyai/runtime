# os

Lua os.time, os.date, os.clock, os.difftime functions. Time, nondeterministic.

## Loading

Global. No require needed.

```lua
os.time()  -- direct access
```

## Constants

```lua
os.platform  -- "wippy"
```

## Functions

### time(spec?: table) → integer

Returns Unix timestamp (seconds since Jan 1, 1970 UTC).

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| spec | table | no | nil | Date/time specification table |

**spec fields:**

| Field | Type | Default | Notes |
|-------|------|---------|-------|
| year | integer | current year | Full year (e.g., 2024) |
| month | integer | current month | 1-12 |
| day | integer | current day | 1-31 |
| hour | integer | 0 | 0-23 |
| min | integer | 0 | 0-59 |
| sec | integer | 0 | 0-59 |

**Returns:**
- No arguments: `integer` - Current Unix timestamp
- With table: `integer` - Unix timestamp for specified date/time in local timezone

**Notes:**
- Without arguments, returns current time as Unix timestamp
- With table, converts date/time to Unix timestamp using local timezone
- Missing time fields (hour, min, sec) default to 0
- Missing date fields default to current date values

### date(format?: string, timestamp?: integer) → string | table

Formats timestamp as string or returns date table.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| format | string | no | "%c" | Format string or "*t" for table |
| timestamp | integer | no | current time | Unix timestamp to format |

**Returns:**
- Format string: `string` - Formatted date/time string
- Format "*t": `table` - Date components table

**Format specifiers:**

| Code | Meaning | Example |
|------|---------|---------|
| %a | Abbreviated weekday | "Mon" |
| %A | Full weekday | "Monday" |
| %b | Abbreviated month | "Jan" |
| %B | Full month | "January" |
| %c | Date and time | "Mon Jan 2 15:04:05 2006" |
| %d | Day of month (01-31) | "15" |
| %H | Hour (00-23) | "14" |
| %I | Hour (01-12) | "02" |
| %j | Day of year (001-366) | "015" |
| %m | Month (01-12) | "06" |
| %M | Minute (00-59) | "30" |
| %p | AM/PM | "PM" |
| %S | Second (00-59) | "45" |
| %U | Week number (ISO) | "24" |
| %w | Weekday (0-6, Sunday=0) | "6" |
| %W | Week number (ISO) | "24" |
| %x | Date only | "01/02/06" |
| %X | Time only | "15:04:05" |
| %y | 2-digit year | "24" |
| %Y | 4-digit year | "2024" |
| %z | Timezone offset | "-0700" |
| %Z | Timezone name | "MST" |
| %% | Literal % | "%" |

**Date table fields (when format is "*t"):**

| Field | Type | Notes |
|-------|------|-------|
| year | integer | Full year (e.g., 2024) |
| month | integer | 1-12 |
| day | integer | 1-31 |
| hour | integer | 0-23 |
| min | integer | 0-59 |
| sec | integer | 0-59 |
| wday | integer | 1-7 (Sunday=1) |
| yday | integer | 1-366 (day of year) |
| isdst | boolean | Daylight saving time active |

**Notes:**
- Default format "%c" returns full date and time string
- Prefix format with "!" for UTC instead of local time (e.g., "!%Y-%m-%d")
- Unknown format specifiers are returned as-is (e.g., "%Q" returns "%Q")
- Format "*t" returns table with date components
- Uses local timezone unless "!" prefix is used

### clock() → number

Returns elapsed CPU time in seconds since module load.

**Returns:** `number` - Seconds elapsed since module initialization

**Notes:**
- Returns floating-point seconds
- Measures time since the Lua runtime started
- Not the same as wall-clock time
- Always non-negative

### difftime(t2: integer, t1: integer) → number

Returns difference between two timestamps in seconds.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| t2 | integer | yes | - | Later timestamp |
| t1 | integer | yes | - | Earlier timestamp |

**Returns:** `number` - Difference in seconds (t2 - t1)

**Notes:**
- Returns positive if t2 > t1, negative if t2 < t1
- Simply performs t2 - t1
- Arguments are typically Unix timestamps from os.time()

## Example

```lua
-- Get current timestamp
local now = os.time()
print(now)  -- 1718462445

-- Convert date to timestamp
local timestamp = os.time({year=2024, month=6, day=15, hour=12, min=30, sec=45})

-- Format timestamp as string
local formatted = os.date("%Y-%m-%d %H:%M:%S", timestamp)
print(formatted)  -- "2024-06-15 12:30:45"

-- Get date components as table
local tbl = os.date("*t", timestamp)
print(tbl.year, tbl.month, tbl.day)  -- 2024  6  15

-- UTC formatting
local utc = os.date("!%Y-%m-%d %H:%M:%S", timestamp)

-- Time difference
local t1 = os.time({year=2024, month=1, day=1, hour=0, min=0, sec=0})
local t2 = os.time({year=2024, month=1, day=2, hour=0, min=0, sec=0})
local diff = os.difftime(t2, t1)
print(diff)  -- 86400 (one day in seconds)

-- Measure elapsed time
local start = os.clock()
-- ... do work ...
local elapsed = os.clock() - start
print(elapsed)  -- 0.123

-- Platform identifier
print(os.platform)  -- "wippy"
```
