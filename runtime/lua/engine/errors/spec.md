# Lua Errors Module Specification

## Overview

The `errors` module provides structured error handling capabilities with metadata support, error chaining, and stack trace inspection. It's automatically available as a global `errors` table in Lua.

## Module Interface

The errors module is pre-loaded and available globally:

```lua
-- No require needed, errors is global
local err = errors.new("something went wrong")
```

### Error Kind Constants

Error kind constants categorize errors for structured error handling:

- `errors.NOT_FOUND` - Resource not found
- `errors.ALREADY_EXISTS` - Resource already exists
- `errors.INVALID` - Invalid input or state
- `errors.PERMISSION_DENIED` - Access denied
- `errors.UNAVAILABLE` - Service unavailable
- `errors.INTERNAL` - Internal error
- `errors.CANCELED` - Operation canceled
- `errors.CONFLICT` - Resource conflict
- `errors.TIMEOUT` - Operation timed out
- `errors.RATE_LIMITED` - Rate limit exceeded
- `errors.UNKNOWN` - Unknown error (default)

### Functions

#### errors.new(message_or_table)

Creates a new error with optional metadata.

**Simple form (string message):**

Parameters:
- `message`: Error message string

Returns:
- `error`: Error object

**Structured form (table with metadata):**

Parameters:
- `table`: Table with fields:
  - `message`: Error message (required)
  - `kind`: Error kind constant (optional, defaults to `errors.UNKNOWN`)
  - `retryable`: Boolean indicating if operation should be retried (optional, nil = unknown)
  - `details`: Table of additional structured metadata (optional)

Returns:
- `error`: Error object with metadata

#### errors.wrap(parent_error, new_error_or_message)

Wraps an error with additional context, preserving the error chain and metadata.

Parameters:
- `parent_error`: Existing error object to wrap
- `new_error_or_message`: Either a string message or a new error object

Returns:
- `error`: Wrapped error object

Notes:
- Preserves metadata (kind, retryable, details) from parent error
- Adds new stack trace at wrap point
- Useful for adding context as errors propagate up the call stack

#### errors.call_stack(error_object)

Extracts the complete stack trace from an error, including both Lua and Go frames.

Parameters:
- `error_object`: Error object

Returns:
- `stack`: Table containing structured stack frames

### Error Object Methods

Error objects returned by `errors.new()` and `errors.wrap()` have the following methods:

#### err:message()

Returns the error message string.

Returns:
- `message`: Error message as string

#### err:kind()

Returns the error kind constant.

Returns:
- `kind`: Error kind string (e.g., "not_found", "invalid")

#### err:retryable()

Returns whether the operation should be retried.

Returns:
- `retryable`: Boolean `true` or `false`, or `nil` if unknown

Notes:
- `true` - Safe to retry the operation
- `false` - Do not retry (e.g., invalid input)
- `nil` - Retry behavior unknown

#### err:details()

Returns structured metadata about the error.

Returns:
- `details`: Table containing error metadata, or empty table if none

## Example Usage

```lua
-- Simple error
local err = errors.new("file not found")
print(err:message())  -- Output: file not found
print(err:kind())     -- Output: unknown

-- Structured error with metadata
local err = errors.new({
  message = "user not found",
  kind = errors.NOT_FOUND,
  retryable = false,
  details = {
    user_id = "12345",
    attempted_at = os.time()
  }
})

print(err:kind())       -- Output: not_found
print(err:retryable())  -- Output: false
local details = err:details()
print(details.user_id)  -- Output: 12345

-- Error wrapping for context
local function read_config()
  local file_err = errors.new({
    message = "config.lua not found",
    kind = errors.NOT_FOUND,
    retryable = false
  })
  return nil, file_err
end

local function initialize()
  local config, err = read_config()
  if err then
    -- Wrap with additional context
    return nil, errors.wrap(err, "failed to initialize application")
  }
  return config
end

-- Check error properties for handling
local result, err = initialize()
if err then
  if err:kind() == errors.NOT_FOUND then
    print("Resource missing:", err:message())
  elseif err:retryable() then
    print("Retrying operation...")
  else
    print("Fatal error:", err:message())
  end

  -- Get stack trace for debugging
  local stack = errors.call_stack(err)
  print("Stack trace:", stack)
end

-- Rate limiting example
local function api_call()
  return nil, errors.new({
    message = "API rate limit exceeded",
    kind = errors.RATE_LIMITED,
    retryable = true,
    details = {
      retry_after = 60,
      limit = 100,
      window = "1m"
    }
  })
end

local result, err = api_call()
if err and err:kind() == errors.RATE_LIMITED then
  local details = err:details()
  print(string.format("Rate limited. Retry after %d seconds", details.retry_after))
end
```

## Use Cases

### Structured Error Handling for AI Agents

The metadata system enables AI agents to handle errors programmatically:

```lua
local function handle_error(err)
  local kind = err:kind()
  local retryable = err:retryable()

  if kind == errors.NOT_FOUND then
    -- Resource doesn't exist, create it
    create_resource()
  elseif kind == errors.PERMISSION_DENIED then
    -- No access, request permission
    request_permission()
  elseif retryable then
    -- Transient failure, retry with backoff
    retry_with_backoff()
  else
    -- Fatal error, escalate
    escalate_to_human()
  end
end
```

### Error Chain Inspection

```lua
local err1 = errors.new("database connection failed")
local err2 = errors.wrap(err1, "failed to load user")
local err3 = errors.wrap(err2, "authentication failed")

-- err3 contains full error chain with context at each level
print(err3:message())  -- Output: authentication failed: failed to load user: database connection failed
```

## Notes

- Errors are automatically registered as a global during Lua state initialization
- Error objects preserve both Lua and Go stack traces
- Stack traces are captured at error creation and each wrap point
- Metadata (kind, retryable, details) is inherited through error chains
- Error objects can be converted to strings with `tostring(err)`
- The retryable field uses a three-state logic: true, false, or nil (unknown)
