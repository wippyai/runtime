# logger

Structured logging with level-based output and field attachments. IO, nondeterministic.

## Loading

```lua
local logger = require("logger")
```

## Functions

### debug(message: string, fields?: table) → nil

Logs message at debug level.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| message | string | yes | - | Log message text |
| fields | table | no | nil | Key-value pairs to attach |

**fields table:**

| Key | Type | Notes |
|-----|------|-------|
| (any string) | string\|integer\|number\|boolean\|table | Field value, tables converted to JSON |

**Returns:** Nothing

**Notes:**
- Automatically adds `pid` and `location` fields from context
- Non-string keys in fields table are ignored

### info(message: string, fields?: table) → nil

Logs message at info level.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| message | string | yes | - | Log message text |
| fields | table | no | nil | Key-value pairs to attach |

**Returns:** Nothing

### warn(message: string, fields?: table) → nil

Logs message at warn level.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| message | string | yes | - | Log message text |
| fields | table | no | nil | Key-value pairs to attach |

**Returns:** Nothing

### error(message: string, fields?: table) → nil

Logs message at error level.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| message | string | yes | - | Log message text |
| fields | table | no | nil | Key-value pairs to attach |

**Returns:** Nothing

**Notes:**
- If fields table contains `error` key, it's treated specially and formatted as error object

### with(fields: table) → Logger

Creates child logger with permanently attached fields.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| fields | table | yes | - | Key-value pairs to attach to all logs |

**Returns:** `Logger` - new logger instance with fields attached

**Notes:**
- Child logger can be chained with additional `with()` or `named()` calls
- Original logger is unchanged
- Fields are added to all subsequent log calls from child

```lua
local child = logger:with({service = "api"})
child:info("request")  -- includes service="api" field
```

### named(name: string) → Logger

Creates child logger with named prefix.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| name | string | yes | - | Logger name |

**Returns:** `Logger` - new logger instance with name

**Errors:**
- `"name cannot be empty"` - empty string provided

**Notes:**
- Name appears in log output as logger name
- Can be chained with `with()` or additional `named()` calls

```lua
local named = logger:named("handler")
named:info("started")  -- logged with "handler" name
```

## Types

### Logger

Returned by `logger:with()` and `logger:named()`.

| Method | Signature | Returns | Notes |
|--------|-----------|---------|-------|
| debug | (message: string, fields?: table) | - | Log at debug level |
| info | (message: string, fields?: table) | - | Log at info level |
| warn | (message: string, fields?: table) | - | Log at warn level |
| error | (message: string, fields?: table) | - | Log at error level |
| with | (fields: table) | Logger | Create child with fields |
| named | (name: string) | Logger | Create child with name |

## Example

```lua
local logger = require("logger")

-- Basic logging
logger:info("server started", {port = 8080})
logger:debug("processing request")
logger:warn("rate limit approaching", {current = 95, max = 100})
logger:error("connection failed", {error = "timeout"})

-- Create child logger with context fields
local api = logger:with({component = "api", version = "1.0"})
api:info("handling request", {path = "/users"})

-- Create named logger
local auth = logger:named("auth")
auth:info("login attempt", {user = "admin"})

-- Chain logger creation
local handler = api:with({request_id = "abc123"})
handler:info("processing")  -- includes component, version, and request_id
```
